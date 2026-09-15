# Architecture of Custom Agent System

This document defines the technical architecture for implementing a custom Agentic AI system, based on the "bare metal" design principles described in `docs/CUSTOM_AGENT.md` and following the engineering guidelines from `docs/DEFAULT_LLM_SKILL.md`.

## 1. Overview and Philosophy

The objective is to build a manually orchestrated agent system, avoiding the complexity and abstraction of "black box" frameworks (such as LangChain or AutoGen). The architecture prioritizes:

*   **Deterministic Control:** Use of Finite State Machines (FSM) to govern execution flow.
*   **Dependency Injection:** All external components (LLM, Database, Tools) are defined via interfaces.
*   **Memory Isolation:** Session data is isolated by `session_id` (single-tenant, multi-session). Global knowledge (`session_id IS NULL` in the `knowledge` table) is readable by all sessions — see [MEMORY.md, section 2.3](history/MEMORY_SQLITE.md).
*   **Observability:** Complete traceability of each reasoning step and tool execution.

## 2. Core System Components

The architecture is divided into single-responsibility layers, decoupled via Go interfaces.

### 2.1. The Core Engine

The heart of the system is the `AgentNode`, which acts as the main orchestrator. It contains no business-specific logic, only the control logic for the reasoning loop.

*   **Responsibility:** Manage the cycle *Perceive -> Reason -> Act -> Observe*.
*   **Implementation:** A control loop based on FSM (Finite State Machine).

### 2.2. Memory Layer

Following the unified architecture defined in `docs/history/MEMORY_SQLITE.md`, the memory system is designed to be isomorphic (Backend/WASM) using **SQLite** as the core engine.

> **Superseded for the WASM target.** SQLite remains the backend engine and describes the code as it stands today, but it does not compile under TinyGo for `GOOS=js GOARCH=wasm`. The browser path is IndexedDB through `storage.Conn`; the rewrite is Phase 4 of [docs/PLAN.md](PLAN.md).

*   **Structure:** Relational tables + FTS5 (lexical search, v1) + `sqlite-vec` vector search with RRF fusion (v2). Isomorphic: same schema runs on Backend (Go) and Frontend (WASM).
*   **Components:**
    1.  **Short-Term Memory:** `messages` table (Conversation history).
    2.  **Episodic Memory:** `episodes` table (Summarized past contexts).
    3.  **Semantic Memory:** `knowledge` table with Vector embeddings and FTS5 hybrid search.
    4.  **Action Memory:** `tool_logs` table (Audit and self-correction).

[See Memory Architecture Diagram](diagrams/MEMORY_ARCHITECTURE.md)

### 2.3. MCP Client
    
A component that manages agent capabilities by consuming tools from external MCP servers.

*   **Role:** The agent acts as an **MCP Client** consuming tools from external MCP servers via JSON-RPC 2.0 over HTTP.
*   **Abstraction:** Defines a common interface for discovering and calling tools.
*   **Security:** Validates arguments against JSON schemas before execution.

### 2.4. LLM Client (Model Gateway)

An interface that abstracts the model provider (OpenAI, Anthropic, Llama), allowing you to swap the "brain" without altering the agent logic.

## 3. Architecture Diagrams

### 3.1. System Context Diagram

[See Context Diagram](diagrams/SYSTEM_CONTEXT.md)

### 3.2. Execution Flow (ReAct Pattern)

This sequence diagram illustrates how the Orchestrator manages the lifecycle of a request using the ReAct pattern.

[See ReAct Flow Diagram](diagrams/REACT_FLOW.md)

### 3.3. Finite State Machine (FSM)

Agent behavior is not free-form; it is constrained by states to ensure reliability.

[See FSM State Diagram](diagrams/FSM_STATE.md)

### 3.4. MCP Client Flow

Tool discovery and execution via JSON-RPC 2.0 over HTTP.

[See MCP Client Flow Diagram](diagrams/MCP_CLIENT_FLOW.md)

### 3.5. Context Window Management

Token budget strategy and summarization trigger logic.

[See Context Window Diagram](diagrams/CONTEXT_WINDOW.md)

### 4.0. Canonical Types

All value types referenced by the interfaces below (`Message`, `Episode`, `Knowledge`, `ToolLog`,
`LLMRequest`, `LLMResponse`, `ToolDef`, `ToolCall`, `ContextWindowConfig`) are defined in
[docs/TYPES.md](TYPES.md).

### 4.1. Canonical Interfaces

```go
package agent

import "context"

// LLMClient abstracts the model provider (Anthropic, OpenAI, etc).
type LLMClient interface {
    Generate(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

// MemoryStore defines the unified SQLite-based storage.
type MemoryStore interface {
    EnsureSession(ctx context.Context, sessionID string) error
    AppendMessage(ctx context.Context, sessionID string, msg Message) error
    GetMessages(ctx context.Context, sessionID string, limit int) ([]Message, error)
    DeleteMessages(ctx context.Context, sessionID string, ids []string) error
    SaveEpisode(ctx context.Context, sessionID, summary string, tokenCount int, fromID, toID string) error
    GetEpisodes(ctx context.Context, sessionID string, limit int) ([]Episode, error)
    SaveKnowledge(ctx context.Context, sessionID, content, source string) error
    SearchKnowledge(ctx context.Context, query, sessionID string, limit int) ([]Knowledge, error)
    LogToolCall(ctx context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error
    GetToolLogs(ctx context.Context, sessionID, toolName string, limit int) ([]ToolLog, error)
}

// MCPServer is satisfied by any running MCP server.
// The agent connects to it via JSON-RPC 2.0 over HTTP.
type MCPServer interface {
    URL() string
}

// Tool represents a direct in-process capability.
type Tool interface {
    Name() string
    Description() string
    InputSchema() string
    Execute(ctx context.Context, argsJSON string) (string, error)
}
```

### 4.2. Configuration and Identity

The agent is initialized via a `Config` struct that defines its persona, routing, and capabilities.

```go
type IdentityConfig struct {
    Name         string   // Identity name
    Role         string   // Functional description
    Instructions string   // Behavioral constraints
    Goals        []string // High-level objectives
}

type LLMConfig struct {
    Primary    LLMClient // Main reasoning/acting model
    Reflector  LLMClient // For self-correction (defaults to Primary)
    Summarizer LLMClient // For context window compression (defaults to Primary)
}

type Config struct {
    Identity      IdentityConfig
    LLMs          LLMConfig
    Memory        MemoryStore
    
    // Three-Tier Tool Registry
    LocalTools    []Tool      // Direct Go tools
    MCPHandlers   []MCPServer // Running MCP servers (programmatic)
    MCPServers    []string    // Remote MCP URLs
    
    ContextWindow ContextWindowConfig
    MaxIterations int
    MCPTimeout    time.Duration
}
```

### 4.3. Three-Tier Tool Registry

The agent supports three independent tool sources, merged at startup:

| Source | Type | Execution | Use Case |
|--------|------|-----------|----------|
| **Local** | `[]Tool` | Direct in-process | Crypto, math, internal state |
| **Handler** | `[]MCPServer` | JSON-RPC via URL() | Local `*mcpserve.Handler` refs |
| **Remote** | `[]string` | JSON-RPC via URL | External/Cloud MCP servers |

## 5. Implementation Strategy

1.  **No Frameworks:** Go's standard library (`net/http`, `encoding/json`, `context`) will be used for core logic.
2.  **Testing:**
    *   Mocks for `LLMClient` and `MemoryStore` for deterministic unit testing.
    *   Integration tests to verify the complete FSM flow.
3.  **Error Handling:** Tool failures should not stop the agent; they should be reported as error observations to the LLM so it can attempt corrections (Auto-correction).

---
*This document serves as an architectural reference and should be updated if the fundamental system patterns change.*
