> **HISTORICAL — superseded.** This document records the SQLite + `sqlite-vec` study
> that guided memory design up to 2026-09. It is kept for the reasoning it contains
> (memory categorisation, RRF hybrid retrieval, schema shape), **not** as the current
> plan. Two premises of this study did not survive contact with the WASM target:
>
> 1. `modernc.org/sqlite` does not compile under TinyGo for `GOOS=js GOARCH=wasm`.
> 2. `sqlite-vec` is a C extension; loading it in the browser means shipping a second
>    WASM runtime plus a JS VFS layer — exactly the JS dependency this ecosystem avoids.
>
> The current direction is IndexedDB via `webtyp.com/indexdb`, with the whole memory
> layer written once against `storage.Conn` so it runs on any backend.
> See [docs/PLAN.md](../PLAN.md).
>
> Sections 2 (memory categorisation), 2.3 (session scoping) and 3.3 (RRF) remain
> normative — they are reused verbatim by the new plan.

---

# Advanced Agent Memory Architecture: SQLite & Hybrid Retrieval in tinywasm/agent

This document defines the unified memory architecture for the `tinywasm/agent` system. It moves beyond traditional RAG (Retrieval-Augmented Generation) by leveraging **SQLite** as the single, consistent storage engine for both Backend (Go) and Frontend (WASM) environments. This approach ensures isomorphic data handling, structured persistence, and high-performance hybrid retrieval.

## 1. Architectural Philosophy: Why SQLite?

Traditional RAG stacks often fragment data (Vectors in Pinecone/Milvus, Metadata in Postgres/SQL). For autonomous agents requiring low latency, offline capabilities (Local-First AI), and strict consistency, this fragmentation is a bottleneck.

We adopt a **Unified SQLite Architecture**:
*   **Isomorphic:** The same SQL logic and schema run on the Server and in the Browser (via WASM).
*   **Structured & Semantic:** Combines relational data (conversation state, tool logs) with semantic vectors in a single engine.
*   **Low Latency:** Eliminates network round-trips for memory retrieval in Local-First scenarios.

## 2. Memory Categorization Strategy

Agent memory is not a monolithic vector store. It is structured into functional layers, implemented as relational tables with specific indexing strategies.

### 2.1. Short-Term Memory (Context Window)
*   **Purpose:** Stores the immediate conversation history and current execution state.
*   **Storage:** `messages` table with Foreign Keys to a `session_id`.
*   **Retrieval:** Standard SQL queries with limit/offset (Sliding Window).
*   **Retention:** High fidelity, strictly ordered.

### 2.2. Episodic Memory (Summarization)
*   **Purpose:** Compresses past interactions to preserve long-term context without saturating the LLM context window.
*   **Storage:** `episodes` table.
*   **Mechanism:** When a conversation session exceeds token limits, it is summarized and stored here.
*   **Retrieval:** Semantic search (Vectors) or Time-based.

### 2.3. Semantic Memory (Knowledge Base)
*   **Purpose:** Stores facts, user preferences, and domain rules.
*   **Storage:** `knowledge` table (relational) + `knowledge_fts` (FTS5 search).
*   **Mechanism:** Consolidates semantic facts in a single table, searchable via full-text (FTS5) and eventually vector search.
*   **Example:** "User prefers concise answers" (extracted and stored as a rule).
*   **Session Scoping:** `session_id = NULL` means **global knowledge** (business rules, permanent facts shared across all sessions). `session_id = <id>` means session-private knowledge. `SearchKnowledge()` always returns both:
    ```sql
    WHERE (session_id = ? OR session_id IS NULL)
    ORDER BY relevance DESC LIMIT ?
    ```

### 2.4. Action Memory (Tool Logs)
*   **Purpose:** Audit trail of tool executions for debugging and self-correction.
*   **Storage:** `tool_logs` table.
*   **Content:** Input arguments, Output results, Execution duration, Error states.
*   **Use Case:** Preventing cyclic errors by checking if a tool failed previously with the same parameters.

## 3. Technology Stack

### 3.1. Core Engine: SQLite with Extensions
*   **v1 (current):** `modernc.org/sqlite` (pure Go, no CGo). FTS5 (built into SQLite) provides keyword search for the `knowledge_fts` table.
*   **v2 (future):** `github.com/asg017/sqlite-vec` extension for vector search. Same Go client compiled to WASM via TinyGo — `sqlite-vec` provides native WASM compilation, no JavaScript VFS layer required.

### 3.2. Vector Search: `sqlite-vec` (v2)
`sqlite-vec` is a lightweight, dependency-free C extension for vector search.
*   **Performance:** SIMD-accelerated (AVX/NEON) distance calculations.
*   **WASM Compatibility:** Compiles cleanly to WASM, enabling semantic search directly in the browser.
*   **Operation:** Standard SQL virtual tables for vector storage and querying.
*   **Status:** v2 scope. Not included in the v1 implementation.

### 3.3. Hybrid Search (RRF) (v2)
To maximize retrieval quality, we combine:
1.  **Lexical Search:** SQLite FTS5 (Full-Text Search) for exact keyword matching (e.g., specific IDs, unique names).
2.  **Semantic Search:** Vector distance (Cosine/Euclidean) via `sqlite-vec`.
3.  **Fusion:** Reciprocal Rank Fusion (RRF) to normalize and combine scores.

```sql
-- Conceptual RRF Implementation
WITH fts_results AS (
  SELECT rowid, rank FROM knowledge_fts WHERE content MATCH ? LIMIT 20
),
vec_results AS (
  SELECT rowid, distance FROM knowledge_vec WHERE embedding MATCH ? LIMIT 20
)
SELECT rowid, SUM(1.0 / (60 + rank_val)) as score
FROM (
  SELECT rowid, ROW_NUMBER() OVER (ORDER BY rank) as rank_val FROM fts_results
  UNION ALL
  SELECT rowid, ROW_NUMBER() OVER (ORDER BY distance) as rank_val FROM vec_results
)
GROUP BY rowid
ORDER BY score DESC;
```

## 4. Frontend Specifics (v2 WASM Target)

In the `tinywasm` environment, the agent compiles to WASM via TinyGo and runs in the browser. The isomorphic property holds because the same Go `MemoryStore` interface and SQL schema are used — only the compilation target changes.

*   **Driver:** Same Go SQLite client compiled to WASM via TinyGo.
*   **Extension:** `sqlite-vec` provides a WASM compilation of the extension. The same `vec0` virtual table API works identically in both environments.
*   **Optimization:**
    *   **Binary Quantization:** Use quantized vectors (e.g., int8 or binary) to reduce memory footprint in the browser runtime.
    *   **Embedding size:** Consider reducing vector dimensions (e.g., float[384] instead of float[768]) for WASM targets to lower memory usage.

## 5. Schema Design (Reference)

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY, created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK(role IN ('user','assistant','system','tool')),
    content TEXT NOT NULL, tool_name TEXT, tool_call_id TEXT,
    token_count INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at);

CREATE TABLE IF NOT EXISTS episodes (
    id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    summary TEXT NOT NULL, token_count INTEGER NOT NULL DEFAULT 0,
    from_msg_id TEXT NOT NULL, to_msg_id TEXT NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS knowledge (
    id TEXT PRIMARY KEY, session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
    content TEXT NOT NULL, source TEXT NOT NULL DEFAULT 'agent',
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts USING fts5(
    content, content='knowledge', content_rowid='rowid'
);

-- v2 (future): vector search via sqlite-vec extension.
-- CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_vec USING vec0(embedding float[768]);
-- Each rowid matches the corresponding rowid in the knowledge table.

CREATE TABLE IF NOT EXISTS tool_logs (
    id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL, input_json TEXT NOT NULL,
    output_text TEXT, error_text TEXT,
    duration_ms INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX IF NOT EXISTS idx_tool_logs_session ON tool_logs(session_id, created_at);
```

## 6. Diagram

[See Memory Architecture Diagram](../diagrams/MEMORY_ARCHITECTURE.md)

---

## 7. Implementation Phases

### v1 (current)
| Layer | Table | Search | Status |
|-------|-------|--------|--------|
| Short-Term | `messages` | SQL (session_id + created_at) | ✓ |
| Episodic | `episodes` | SQL (session_id + created_at) | ✓ |
| Semantic | `knowledge` + `knowledge_fts` | FTS5 keyword search | ✓ |
| Action | `tool_logs` | SQL (session_id + tool_name) | ✓ |

`SearchKnowledge()` v1 query:
```sql
SELECT k.* FROM knowledge k
JOIN knowledge_fts f ON k.rowid = f.rowid
WHERE f.content MATCH ?
  AND (k.session_id = ? OR k.session_id IS NULL)
ORDER BY rank LIMIT ?
```

### v2 (future)
- Add `knowledge_vec` virtual table (`sqlite-vec`, `float[768]`)
- Add RRF fusion (FTS5 rank + vector distance)
- Embedding generation: `Orchestrator` computes embedding before calling `SaveKnowledge()`
- `LLMConfig` may gain an optional `EmbeddingClient` interface
