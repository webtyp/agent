---
PLAN: "feat: browser-native semantic search — master index"
TAG: v0.2.0
EXECUTOR: unassigned
REVIEWER: none
---

> This is the **index plan**. It owns no code. Every repository listed in §5 carries its
> own `docs/PLAN.md` with the concrete diff; this file owns the architecture, the
> shared contracts, the build order and the acceptance gates that span repositories.
>
> Plans for repositories that **do not exist yet** live in [`docs/plans/`](plans/) until
> the repository is created; the file is then moved to that repository's `docs/PLAN.md`
> unchanged.

# Plan — Semantic search in the browser, on IndexedDB

## 1. Objective

Run semantic search **entirely in the browser**: embedding generation, vector storage
and k-nearest-neighbour retrieval, with no server round-trip at query time and no
JavaScript library dependency. Persistence is `webtyp.com/indexdb`, extended where its
current API cannot express the problem.

The agent's `MemoryStore` is rewritten once against `storage.Conn` so the same memory
code runs on a SQL backend (server) and on IndexedDB (browser).

## 2. Why this replaces the SQLite study

The previous design ([`docs/history/MEMORY_SQLITE.md`](history/MEMORY_SQLITE.md))
assumed one storage engine everywhere: SQLite on the server, the same SQLite compiled
to WASM in the browser, with `sqlite-vec` for vector search. Two premises failed:

- `modernc.org/sqlite` is pure Go but does **not** compile under TinyGo for
  `GOOS=js GOARCH=wasm`. It is a backend-only dependency.
- `sqlite-vec` is a C extension. Using it in the browser means shipping a second WASM
  runtime plus a JavaScript VFS shim — the exact JS dependency the ecosystem exists to
  avoid, and a second copy of every byte of storage.

What survives from that study and is **reused verbatim**: the memory categorisation
(short-term / episodic / semantic / action), the `session_id IS NULL` global-knowledge
scoping rule, and Reciprocal Rank Fusion as the eventual hybrid-retrieval strategy.

What replaces it: the browser already ships a persistent, transactional, structured
store — IndexedDB — and this ecosystem already owns a driver for it. The isomorphism
moves up one level: instead of "the same engine everywhere", it becomes "the same
`storage.Conn` contract everywhere", which is what `webtyp.com/storage` was built for.

## 3. Architecture

```
                          ┌──────────────────────────────┐
                          │  agent  (MemoryStore)        │
                          │  rewritten on storage.Conn   │
                          └──────────────┬───────────────┘
                                         │
                          ┌──────────────▼───────────────┐
                          │  vectordb                    │  documents, kNN,
                          │  the store                   │  filters, LRU, quota
                          └──┬──────────┬──────────┬─────┘
                             │          │          │
         ┌───────────────────▼──┐  ┌────▼─────┐  ┌─▼─────────────────┐
         │  vector              │  │  embed   │  │  storage          │
         │  math, arena, top-k, │  │  port +  │  │  Conn contract    │
         │  LE codec            │  │  adapter │  └──┬─────────────┬──┘
         └──────────────────────┘  └─┬──┬──┬──┘     │             │
                                     │  │  │        │             │
              ┌──────────────────────┘  │  └────────┼─────┐       │
              │              ┌──────────┘           │     │       │
    ┌─────────▼────────┐  ┌──▼──────────────┐  ┌────▼─────▼───┐ ┌─▼─────────────┐
    │  tokenizer       │  │  weights        │  │  indexdb     │ │ sqlt/postgres │
    │  text → ids      │  │  artifact + IDB │  │  browser     │ │ server        │
    └──────────────────┘  │  cache          │  └──────────────┘ └───────────────┘
                          └──┬──────────────┘
                             │
        ╔════════════════════▼══════════════════════════════════╗
        ║  PHASE 5 — only if Phase 3 recall proves insufficient  ║
        ║    webgpu  (navigator.gpu)  →  nn  (WGSL encoder)      ║
        ║    → a second embed adapter, same Embedder interface   ║
        ╚═══════════════════════════════════════════════════════╝
```

Every arrow is a compile-time dependency. There are no cycles and no repository
depends on `agent`.

## 4. Global design decisions

These are decided **here**, once. Repository plans reference this section rather than
re-arguing it.

### D1 — A vector is a `[]byte` on disk and a slice of a shared arena in memory

On disk the canonical type is `model.Blob()` → `[]byte` → a JS `Uint8Array` under
structured clone. In Go, vectors are **never** a `[]float32` per document: they live in
one contiguous `[]float32` arena of `N × Dim`, where document *i* occupies
`arena[i*Dim : (i+1)*Dim]`.

Rationale, in the order that matters:

1. **`js.ValueOf` cannot carry a vector, and failing costs the whole app.**
   `execute.go:create` builds a `map[string]any` and hands it to `store.Call("add", …)`,
   which routes through `js.ValueOf`. `js.ValueOf` accepts `[]any` but neither `[]byte`
   nor `[]float32`, and panics on anything else. Under TinyGo `GOOS=js GOARCH=wasm`
   there is **no `recover()`** (`getStore` already documents this), so that panic is an
   unrecoverable crash, not an error value. Encoding a 384-dim vector as `[]any` to get
   around it costs 384 boxed allocations and ~3 KB of JS heap per document against
   1536 bytes of real data. Not viable.

2. **`js.CopyBytesToJS` / `js.CopyBytesToGo` are the only bulk-copy primitives, and
   TinyGo implements both.** They are a `memcpy` between WASM linear memory and a
   `Uint8Array`. The path `[]float32` → (O(1) reinterpretation) → `[]byte` → one memcpy
   → `Uint8Array` has no intermediate representation: no JSON, no base64, no boxing.

3. **`FieldBlob` already exists** (`model/field.go:13`) and is already wired through
   `IsZeroPtr`, `ValuesFrom` and the codecs. Introducing a new `FieldType` would force
   an edit to every exhaustive `switch` in `model`, `storage/mem`, `sqlt`, `postgres`
   and `indexdb` — a breaking change across six repositories to buy nothing.

4. **`[]byte` is the only isomorphic representation.** It maps to `BLOB` (SQLite),
   `BYTEA` (Postgres) and `Uint8Array` (IndexedDB). A `Float32Array` would be marginally
   faster in the browser and has no SQL counterpart.

5. **The arena is what actually makes queries fast.** Scoring reads only WASM linear
   memory, so a query crosses the JS boundary **zero times**. It allocates nothing per
   candidate. TinyGo's conservative GC sees one large object instead of N small slices —
   this matters far more under TinyGo than under standard Go. And the dot product walks
   contiguous memory, so cache locality is optimal.

6. **Vectors are stored L2-normalised**, making cosine similarity a plain dot product.
   This removes one `sqrt` and one division per document per query. (The JS original
   precomputes the magnitude but still divides N times per search.)

**Cost, stated honestly:** resident memory is `N × Dim × 4` bytes. 10 000 documents at
384 dims is 15 MB — acceptable. 100 000 documents is 150 MB, which is where int8
quantisation (÷4) becomes mandatory. Quantisation is **Phase 5**, not v1.

### D2 — Vectors persist in shards, documents persist per row

One row of `vec_shards` holds `ShardSize` (default 1024) vectors as a single blob.
Cold start becomes `N/1024` structured-clone deserialisations instead of `N`, each one
a single `CopyBytesToGo` straight into its offset in the arena.

Text and metadata live in a **separate** object store, read only for the final top-k —
never during scoring. In RAM, alongside the arena, sits a compact header array
(`id`, `tags`, `created`, `hits`, `deleted`) used for pre-filtering.

A consequence worth noting: because the hot read path is "a handful of large rows from
one table", `vectordb` needs **no streaming-scan API** from `storage`. That shrinks the
`storage` change to blob conformance plus batch insert.

### D3 — IndexedDB cannot index a vector. Every kNN query is a full scan.

No B-tree over a 384-dimensional space helps. This is not a limitation to engineer
around in v1; it is the shape of the problem. The design therefore minimises **cost per
candidate** (D1, D2) rather than trying to avoid candidates. Approximate indexes
(HNSW/IVF) are explicitly out of scope until a corpus exists that needs them.

### D4 — Embeddings are produced in the browser, in two phases behind one port

`embed.Embedder` is the single contract. Two implementations ship behind it:

- **Phase 3 — static embeddings.** A distilled token-embedding table (model2vec /
  "potion" family): tokenise, look up, mean-pool, normalise. No attention, no forward
  pass, no GPU. Pure Go, TinyGo-compatible today, ~30 MB quantised. Retrieval quality is
  below a full encoder but sound, and it delivers a working end-to-end pipeline before
  any GPU code exists.
- **Phase 4 — transformer encoder on WebGPU.** Full sentence-transformer inference via
  `navigator.gpu`, owned end to end in Go + WGSL.

Both run entirely in the browser. Phase 4 is a **new adapter**, not a rewrite:
`vectordb`, `indexdb`, `storage` and `model` are untouched by it.

### D5 — Spanish is a selection constraint, and it dominates model size

`docs/EFFICIENT_SLM.md` makes Spanish a first-class requirement, which rules out
English-only encoders (`all-MiniLM-L6-v2` and friends). Multilingual models carry a
~250 k-token vocabulary, and the embedding table alone is
`250 000 × 384 × 4 ≈ 384 MB` in fp32 — far larger than the transformer body.

Therefore: **the embedding table ships int8-quantised with per-row scales** (~96 MB, and
~30 MB for a 256-dim static model), and the model artifact is cached in IndexedDB after
first download so it is fetched once per browser, not once per session. Candidate
models and the final choice are recorded in [`docs/plans/embed.md`](plans/embed.md).

### D6 — Licensing

`webtyp/vector-storage` is a fork of `nitaiaharoni1/vector-storage` (MIT). Any Go
repository that ports its logic — `vectordb` above all — must carry a `NOTICE` file
crediting the original author and reproducing the MIT terms. This is not optional.

## 5. Repository map

| Repository | State | Single responsibility | Phase | Plan |
|---|---|---|---|---|
| `webtyp/model` | modify | `Vector(dim)` kind over `FieldBlob` | 1 | [`model/docs/PLAN.md`](https://github.com/webtyp/model/blob/main/docs/PLAN.md) |
| `webtyp/storage` | modify | blob conformance + batch insert contract | 1 | [`storage/docs/PLAN.md`](https://github.com/webtyp/storage/blob/main/docs/PLAN.md) |
| `webtyp/indexdb` | modify | blob I/O, one transaction per batch | 1 | [`indexdb/docs/PLAN.md`](https://github.com/webtyp/indexdb/blob/main/docs/PLAN.md) |
| `webtyp/vector` | **new** | vector math, arena, top-k, LE codec | 2 | [`docs/plans/vector.md`](plans/vector.md) |
| `webtyp/vectordb` | **new** | document store + kNN + filters + LRU | 2 | [`docs/plans/vectordb.md`](plans/vectordb.md) |
| `webtyp/tokenizer` | **new** | text → token ids | 3 | [`docs/plans/tokenizer.md`](plans/tokenizer.md) |
| `webtyp/weights` | **new** | model artifact format + browser cache | 3 | [`docs/plans/weights.md`](plans/weights.md) |
| `webtyp/embed` | **new** | `Embedder` port + static adapter | 3 | [`docs/plans/embed.md`](plans/embed.md) |
| `webtyp/agent` | modify | `MemoryStore` on `storage.Conn` | 4 | this repo, §7 |
| `webtyp/webgpu` | **new** | `navigator.gpu` bindings | 5 | [`docs/plans/webgpu.md`](plans/webgpu.md) |
| `webtyp/nn` | **new** | WGSL kernels + encoder graph | 5 | [`docs/plans/nn.md`](plans/nn.md) |
| `webtyp/vector-storage` | freeze | historical JS reference + port map | 1 | [`vector-storage/docs/PLAN.md`](https://github.com/webtyp/vector-storage/blob/main/docs/PLAN.md) |

## 6. Build order and phase gates

Phases are strictly ordered: a phase does not start until the previous one's gate passes.
Within a phase, repositories in the same row can proceed in parallel.

### Phase 1 — Unblock persistence (no ML)
`model` → `storage` → `indexdb`, in that order (each depends on the previous release).

**Gate:** a `[]byte` of 1536 bytes round-trips through IndexedDB in a real browser with
byte-for-byte equality, and inserting 1024 rows uses **one** transaction. Proven by
`indexdb/tests/` under `gotest -tinygo` and by the `storage` conformance suite passing
on `mem`, `sqlt` and `indexdb`.

### Phase 2 — Math and store (no ML)
`vector` → `vectordb`.

**Gate:** kNN over 10 000 synthetic vectors returns the correct top-k (verified against
a naive reference implementation) with **zero allocations per query**, measured by
`testing.AllocsPerRun`. The same test passes against `storage/mem` in standard Go and
against `indexdb` in the browser.

### Phase 3 — Embeddings in the browser
`tokenizer` and `weights` in parallel, then `embed`.

**Gate:** a real corpus in Spanish is indexed and searched end to end in the browser,
offline after first load, with a documented recall@10 against a reference.

### Phase 4 — Agent memory
`agent`: `MemoryStore` rewritten on `storage.Conn`, `SearchKnowledge` gains its semantic
path.

**Gate:** the full DDT matrix in `DEFAULT_LLM_SKILL.md` §2 passes against both a SQL
backend and `indexdb`, from one implementation.

### Phase 5 — WebGPU encoder and optimisation
`webgpu` → `nn` → a second `embed` adapter. Then int8 quantisation in `vector`, then
lexical BM25 + RRF fusion.

**Entry condition, not just a dependency:** Phase 5 starts only if `embed`'s evaluation
harness shows Phase 3's static embedder is not good enough on the Spanish test set. If
recall@10 is adequate, not building `nn` is the correct outcome. See
[`docs/plans/nn.md`](plans/nn.md).

**Gate:** the Phase 4 encoder produces vectors matching the reference implementation
within a documented tolerance, and `vectordb` is unchanged by its arrival.

## 7. Changes owned by this repository (`agent`)

Scope, to be detailed once Phase 2 releases:

1. **`memory.go` → `memory_storage.go`.** Drop `modernc.org/sqlite`. One `MemoryStore`
   implementation written against `storage.Conn`, so the backend becomes an injected
   dependency — which is what `DEFAULT_LLM_SKILL.md` §1 already demands and what the
   current direct-SQL implementation violates.
2. **Schema as `model.Definition` values**, not SQL DDL strings, so `indexdb` can create
   the object stores from the same declaration a SQL backend turns into DDL.
3. **`SearchKnowledge` gains a semantic path** through `vectordb`, with an injected
   `embed.Embedder`. Lexical FTS5 is not available outside SQLite: on `indexdb`, the
   lexical half of RRF waits for Phase 5's BM25 index. Until then `SearchKnowledge` is
   `LIKE`-based lexically and vector-based semantically.
4. **`LLMConfig` gains an optional `Embedder`**, as anticipated by the historical study.
5. **Module path migration** `github.com/tinywasm/agent` → `webtyp.com/agent`. Every
   other repository in this plan already migrated; `agent` cannot depend on
   `webtyp.com/storage` while advertising the old path without confusing `gopush`'s
   dependent-module updates. This is a prerequisite for Phase 4, not part of it.
6. Update `DEFAULT_LLM_SKILL.md`'s dependency allow-list: `modernc.org/sqlite` leaves
   production code; `webtyp.com/storage`, `webtyp.com/vectordb` and `webtyp.com/embed`
   enter it.

## 8. Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Phase 5 (WebGPU encoder) is a large, open-ended project | Delivery slips indefinitely | Phase 3's static embedder makes the product work without it; Phase 5 is an adapter swap behind `embed.Embedder` |
| Multilingual model artifact is 100 MB+ | Unusable first load | int8 embedding table, IndexedDB cache after first fetch, and a documented smaller fallback |
| IndexedDB quota eviction wipes the index | Silent data loss | `navigator.storage.persist()` on init, `estimate()` before writes, LRU eviction before the browser's |
| `jsvalue.ScanValue` may not handle `Uint8Array` | Phase 1 blocked | **Verify first** — it is the opening task of the `indexdb` plan |
| Arena outgrows browser memory past ~100 k docs | OOM | Documented ceiling, int8 quantisation in Phase 5 |
| Vectors and text drift out of sync on partial write | Corrupt results | Single transaction spanning both stores; a `dim`/`model_id` header row rejects a mismatched arena at load |

## 9. Open decisions

- **O1.** Exact embedding model and dimension for Phase 3 (see `plans/embed.md` §2).
  Constrained by D5. Default assumption until decided: 256 dims, multilingual static.
- **O2.** Does `webtyp/binary` already provide a little-endian float32 codec? If so,
  `vector` depends on it instead of defining its own. **Verify before writing `vector`.**
- **O3.** Metadata filtering shape. v1 assumption: opaque `model.RawJSON` for payload
  plus a delimited, `LIKE`-filterable tag column, because `model` has no string-slice
  field type. Revisit if filtering becomes a hot path.
- **O4.** Whether `vectordb` should expose an optional `VectorSearcher` capability so
  `postgres` can delegate to pgvector server-side. Deferred: it changes no browser code.
