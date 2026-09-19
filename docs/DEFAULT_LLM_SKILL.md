# DEFAULT_LLM_SKILL — `webtyp/agent`

This file defines the **mandatory engineering rules** for any LLM working on this project.
It is the authoritative source for the "Development Rules" section in `IMPLEMENTATION.md`.

---

## 1. Architecture & Design

### Single Responsibility Principle (SRP)
Every Go file must have a single, well-defined purpose reflected in its name:
- `interfaces.go` — public contracts only
- `types.go` — value types only
- `fsm.go` — FSM logic only
- `orchestrator.go` — ReAct loop only
- `memory.go` — SQLite MemoryStore implementation only

Files exceeding **500 lines MUST be split** and renamed by domain.

### Mandatory Dependency Injection (DI)
- **No global state.** Zero direct system calls (OS, Network, DB) in business logic.
- **Interfaces** define all external dependencies: `LLMClient`, `MemoryStore`, `MCPServer`, `Tool`.
- **Composition:** The `Agent` struct holds interfaces, never concrete implementations.
- **`agent.go` constructor** (`New(cfg Config)`) is the **ONLY** place where concrete types are wired together.

### Isomorphic Package Policy (Backend + WASM)
This library is designed to run on both backend (Go) and frontend (WASM via TinyGo). To maintain isomorphic compatibility, **always prefer `webtyp.com/` packages** over their stdlib counterparts:

| Instead of (stdlib)      | Use (webtyp)                       |
|--------------------------|--------------------------------------|
| `fmt`, `errors`, `strings`, `strconv` | `webtyp.com/fmt` |
| `encoding/json`          | `webtyp.com/json`           |
| `context`                | `webtyp.com/context`        |
| `time`                   | `webtyp.com/time`           |

Packages **without** a `webtyp.com/` equivalent use stdlib directly: `net/http`, `net/url`, `sync`.

### Allowed External Dependencies

**Production Code (v1):**
- `webtyp.com/fmt` — isomorphic fmt/errors/strings/strconv
- `webtyp.com/json` — isomorphic encoding/json
- `webtyp.com/context` — isomorphic context
- `webtyp.com/time` — isomorphic time
- `modernc.org/sqlite` — pure Go SQLite driver (supports `:memory:` out of the box)

**Production Code (v2 — future):**
- `github.com/asg017/sqlite-vec` — vector search extension (WASM-compatible)

**Test-Only Code** (`*_test.go`):
- `webtyp.com/mcp` — MCP server & client implementation for tests

**External Test Tools** (not Go imports — external processes):
- **Ollama** (v0.4+) with `qwen2.5:7b` — required for `//go:build integration` tests only
  - Install: `curl -fsSL https://ollama.com/install.sh | sh && ollama pull qwen2.5:7b`
  - Health check: `GET http://localhost:11434/api/tags`
  - Fallback model: `llama3.1:8b`

Do NOT add any other external dependency without explicit approval.

---

## 2. Testing

### Test Runner
Always use **`gotest`** (`github.com/tinywasm/devflow/cmd/gotest`). It runs `vet`, standard tests with `-race` and `-cover`, detects WASM tests, uses intelligent git-state caching, and updates README badges automatically.

### Diagram-Driven Testing (DDT)
Every logic flow documented in `docs/diagrams/*.md` **MUST** have a corresponding integration test covering:
- Every branch (decision diamond)
- Every failure mode (timeouts, errors, invalid transitions)

The mandatory DDT test matrix for this project:

| Test | Diagram Covered |
|------|----------------|
| `TestReAct_ToolCallThenAnswer` | `REACT_FLOW.md` |
| `TestReAct_ReflectionApproved` | `REACT_FLOW.md` |
| `TestReAct_ReflectionRetry` | `REACT_FLOW.md` |
| `TestReAct_ToolErrorSelfCorrect` | `REACT_FLOW.md` |
| `TestReAct_MaxIterationsGuard` | `FSM_STATE.md` |
| `TestFSM_InvalidTransition` | `FSM_STATE.md` |
| `TestContextWindow_Summarize` | `CONTEXT_WINDOW.md` |
| `TestMCPClient_Discovery` | `MCP_CLIENT_FLOW.md` |
| `TestMCPClient_CallTool` | `MCP_CLIENT_FLOW.md` |

### Standard Library Only for Assertions
**NEVER** use external assertion libraries (`testify`, `gomega`).
Use only: `testing`, `net/http/httptest`, `reflect`, `errors`.

### Mandatory Mocking
Every external interface **MUST** have a mock in `*_test.go` files:
- `mock_llm_test.go` — `MockLLMClient`
- `mock_memory_test.go` — `MockMemoryStore`
- `mock_mcp_test.go` — `MockMCPClient`

Tests must be fast (no real I/O), deterministic, and side-effect free.

### Shared Setup
`setup_test.go` initializes shared test infrastructure **once** for the entire package:

```go
var (
    testServer *httptest.Server
    testMemory agent.MemoryStore // shared SQLite :memory: instance
)

func TestMain(m *testing.M) {
    // SQLite :memory: — no disk writes, fully isolated per-process, fast.
    // One shared instance for the whole test package; session isolation is
    // guaranteed by the session_id column in every table (see history/MEMORY_SQLITE.md, section 2.3).
    testMemory = agent.NewSQLiteMemory(":memory:")

    srv, _ := mcp.NewServer(mcp.Config{Name: "test", Version: "1.0.0", Authorize: mcp.AllowAll}, []mcp.ToolProvider{testToolProvider{}})
    testServer = httptest.NewServer(http.HandlerFunc(...))
    os.Exit(m.Run())
}
```

**Per-test session isolation:** Each test function uses a unique `sessionID` derived from `t.Name()` to avoid cross-test state contamination in the shared `:memory:` database:

```go
func TestReAct_ToolCallThenAnswer(t *testing.T) {
    sessionID := t.Name() // e.g. "TestReAct_ToolCallThenAnswer"
    // testMemory.EnsureSession(ctx, sessionID) creates an isolated namespace
    // No teardown needed — the :memory: DB is discarded at process exit.
}
```

> **Why `:memory:` (not a temp file)?**
> - No disk I/O → tests run significantly faster.
> - No leftover files on test failure.
> - `modernc.org/sqlite` supports `:memory:` natively with the same SQL API as on-disk SQLite.
> - Session isolation by `session_id` makes a single shared instance sufficient for all tests.


---

## 3. File Structure

> **Single source of truth:** See [IMPLEMENTATION.md, section 1](IMPLEMENTATION.md) for the authoritative file tree with annotations.

Key rules for file structure:
- **Max 500 lines per file** — split and rename by domain if exceeded.
- **No provider-specific adapters in the core package** — `agent` is provider-agnostic. Users bring their own `LLMClient` implementation.
- **Flat hierarchy** — no subdirectories (Go library rule).
- **If test files exceed 5 in root**, move **ALL** tests to `tests/`.

---

## 4. Agent-Specific Patterns

### IdentityConfig → System Prompt
The system prompt is **auto-generated** at startup from `IdentityConfig` fields and the consolidated tool list:
```
You are {Name}, {Role}.

## Instructions
{Instructions}

## Goals
- {Goal_1}
- ...

## Available Tools
{Tool_A}: {Description}
...
```

### LLMConfig → Role Routing
The agent uses operation-specific LLM routing:
- **Primary:** Used for the main Reasoning + Acting phases.
- **Reflector:** Used for the self-correction phase (defaults to Primary if nil).
- **Summarizer:** Used for context window compression (defaults to Primary if nil).

### Three-Tier Tool Registry
Tools are merged from three sources into a unified internal registry:
1. **LocalTools:** Direct in-process Go tools.
2. **MCPHandlers:** Programmatic references to running MCP servers (via `URL()`).
3. **MCPServers:** Remote external MCP server URLs.

### FSM Enforcement
Agent behavior is governed by a strict FSM. **Never** allow free-form loops without FSM transition validation.

Valid transitions:
```
Idle → Reasoning → Acting → Reasoning (loop allowed)
Reasoning → Reflecting → Responding
Acting → Reflecting → Responding
```

Any invalid transition must return an error — never silently skip.

### ReAct + Reflection Loop
The orchestrator loop has a **MaxIterations guard** (default: 10). Exceeding it must terminate with an explicit error, never silently hang.

### Tool Error Resilience
Tool failures must **not** stop the agent. They are appended to memory as error observations so the LLM can self-correct. Only repeated failures beyond MaxIterations should cause termination.

### MCP Protocol
- All MCP calls use **JSON-RPC 2.0 over a single HTTP endpoint** (method encoded in body, never in URL path).
- Handshake sequence: `initialize` → `notifications/initialized` (no ID, no response) → `tools/list`.
- Arguments must be validated against the tool's JSON Schema before execution.

### LLM Adapter Contract
The `agent` package is **provider-agnostic**. No LLM adapters ship with the core package.

Users implement `LLMClient` for their preferred provider. Each adapter must:
1. Map `LLMRequest` → provider-specific wire format.
2. Map provider response → `LLMResponse` (with `StopReason: "tool_use" | "end_turn"`, `ToolCalls []ToolCall`).
3. Handle any protocol quirks (e.g., Anthropic requires consecutive `role="tool"` messages to be merged into a single `role="user"` with `tool_result` blocks; OpenAI-compatible APIs do not).

Reference implementations are in [IMPLEMENTATION.md Appendix A](IMPLEMENTATION.md).

---

## 5. Documentation

- Update `docs/ARCHITECTURE.md` if contracts (interfaces) change.
- Update `docs/IMPLEMENTATION.md` if implementation steps or file structure changes.
- Update diagrams in `docs/diagrams/` if any flow changes.
- Link all new docs from `README.md`.
- **Never push without updating documentation first.**

### Publishing
Use **`gopush`** (`github.com/tinywasm/devflow/cmd/gopush`) to publish. It runs tests, commits, tags, pushes, and updates dependent modules. Only run after all tests pass via `gotest`.
