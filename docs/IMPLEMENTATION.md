# Implementation Plan: `tinywasm/agent`

## Development Rules

- **Single Responsibility Principle (SRP):** Every file (CSS, Go, JS) must have a single, well-defined purpose. This must be reflected in both the file's content and its naming convention.

- **Mandatory Dependency Injection (DI):**
    - **No Global State:** Avoid direct system calls (OS, Network) in logic.
    - **Interfaces:** Define interfaces for external dependencies (`LLMClient`, `MemoryStore`, `MCPClient`).
    - **Composition:** Main structs must hold these interfaces.
    - **Injection:** `agent.go` constructor is the ONLY place where "Real" implementations are injected.

- **Isomorphic Package Policy:** This library runs on both backend and WASM. Always prefer `tinywasm/` packages over stdlib: `github.com/tinywasm/fmt` (replaces `fmt`, `errors`, `strings`, `strconv`), `github.com/tinywasm/context` (replaces `context`), `github.com/tinywasm/time` (replaces `time`). Use stdlib directly only for packages without a `tinywasm/` equivalent (`net/http`, `sync`, `encoding/json`). Allowed external packages: `modernc.org/sqlite`. (`github.com/asg017/sqlite-vec` is **v2 scope** — see section 4 and [MEMORY.md, section 7](history/MEMORY_SQLITE.md)).

  > **Note on `tinywasm/json`:** Not yet implemented. `encoding/json` is used directly in backend-only files (`memory.go`, `mcp_client.go`, `mcp_registry.go`) until `tinywasm/json` is created.
  >
  > **Note on `tinywasm/context` and `tinywasm/time`:** Both packages require a refactor to expose backend-compatible APIs (see [tinywasm/context BACKEND_COMPAT plan](https://github.com/tinywasm/context) and [tinywasm/time DURATION_SUPPORT plan](https://github.com/tinywasm/time)). The agent refactor is pending those library updates.

- **Testing:**
    - **Diagram-Driven Testing (DDT):** Every logic flow in `docs/diagrams/*.md` must have a corresponding test.
    - **Mocks:** Use Mocks for external interfaces to ensure fast, deterministic tests.

---

## 1. Project Structure

```
agent/
├── go.mod                      # module github.com/tinywasm/agent
├── agent.go                    # Config struct + New() constructor (DI root)
├── interfaces.go               # Contracts: LLMClient, MemoryStore, MCPServer, Tool
├── types.go                    # Value types + IdentityConfig, LLMConfig
├── fsm.go                      # State machine + transition table
├── orchestrator.go             # ReAct + Reflection loop
├── memory.go                   # SQLiteMemoryStore implementation
├── schema.go                   # DDL strings + migrations
├── context_window.go           # Token budget + summarization
├── mcp_client.go               # HTTPMCPClient — JSON-RPC 2.0
├── mcp_registry.go             # MCPRegistry — multi-server management
│
├── mock_llm_test.go            # MockLLMClient (production-tested interface)
├── mock_memory_test.go         # MockMemoryStore (production-tested interface)
├── mock_mcp_test.go            # MockMCPClient (for deterministic unit tests)
├── setup_test.go               # Shared infrastructure (in-memory DB, *mcpserve.Handler)
├── fsm_test.go
├── orchestrator_test.go        # DDT: ReAct loop + FSM transitions (uses *mcpserve.Handler)
├── memory_test.go
├── mcp_client_test.go          # HTTP JSON-RPC tests (uses httptest.Server)
├── context_window_test.go
└── integration_test.go         # //go:build integration — clinic real LLM tests (Ollama)
```

> **Provider agnosticism:** The `agent` package ships with **zero LLM provider adapters**. Users implement `LLMClient` for their preferred provider (Anthropic, OpenAI, Ollama, etc.). See [Appendix A](#appendix-a-reference-llm-adapter-implementations) for reference implementations to copy as a starting point.

**Production Dependencies (v1):**
- `github.com/tinywasm/fmt`, `github.com/tinywasm/context`, `github.com/tinywasm/time` (isomorphic — pending library updates)
- `modernc.org/sqlite` (pure Go SQLite driver)
- `github.com/google/uuid` (ID generation)

> **v2 (future):** `github.com/asg017/sqlite-vec` — vector search extension. Not required in v1; FTS5 (built into SQLite) covers semantic search. See [MEMORY.md, section 7](history/MEMORY_SQLITE.md).

**Test-Only Dependencies:**
- `github.com/tinywasm/mcpserve` (v0.0.20+) — for real MCP protocol testing

**External Test Tools (not Go imports):**
- **Ollama** (v0.4+) with `qwen2.5:7b` — required only for `//go:build integration` tests. Skipped automatically if not running.

---

## 2. Canonical Types (`types.go`)

See [types.go](../types.go). All value types are defined there.

---

## 3. Canonical Interfaces (`interfaces.go`)

See [interfaces.go](../interfaces.go). All interfaces are defined there.

---

## 4. Canonical SQLite Schema (`schema.go`)

See [schema.go](../schema.go).

---

## 5. ReAct + Reflection Algorithm

See [orchestrator.go](../orchestrator.go).

---

## 6. Context Window Management

See [context_window.go](../context_window.go).

---

## 7. Testing Strategy (DDT)

### Integration Tests — ReAct Loop & FSM

Mandatory integration tests covering the ReAct loop and FSM transitions:

| Test | FSM Transition Scenarios |
|------|-------------------------|
| `TestReAct_ToolCallThenAnswer` | Reasoning → Acting → Reasoning → Reflecting → Responding |
| `TestReAct_ReflectionApproved` | Reasoning → Reflecting(SUFFICIENT) → Responding |
| `TestReAct_ReflectionRetry` | Reflecting(INSUFFICIENT) → Reasoning → Reflecting(SUFFICIENT) |
| `TestReAct_ToolErrorSelfCorrect` | Acting(error) → Reasoning → Correction |
| `TestReAct_MaxIterationsGuard` | Loop stops after MaxIterations even if LLM wants to continue |
| `TestContextWindow_Summarize` | Trigger summarization when threshold reached |

### MCP Protocol Tests

| Test | Method | Description |
|------|--------|-------------|
| `TestMCPClient_Discovery` | `httptest.Server` | Verify Tool Definition discovery via manual JSON-RPC |
| `TestMCPClient_CallTool` | `httptest.Server` | Verify JSON-RPC execution with error handling |
| `TestOrchestrator_RealMCP` | `*mcpserve.Handler` | End-to-end ReAct flow with real MCP server |

### SQLite Test Strategy

All tests use **SQLite `:memory:`** — no disk writes, no teardown required:

```go
// setup_test.go — initialized ONCE for the entire test package
var testMemory agent.MemoryStore

func TestMain(m *testing.M) {
    testMemory = agent.NewSQLiteMemory(":memory:")
    // ...
}
```

**Session isolation:** Each test function passes `t.Name()` as `sessionID`. The shared `:memory:` database isolates all reads/writes by `session_id` column — no data leaks between tests.

```go
func TestReAct_ToolCallThenAnswer(t *testing.T) {
    sessionID := t.Name() // unique per test
    // No teardown — :memory: is discarded at process exit
}
```

> `modernc.org/sqlite` supports `:memory:` with the same API as on-disk SQLite.
> A single shared instance is sufficient because session isolation is guaranteed by schema design.

### Test Dependency Strategy

**Test-only imports** (appear in `go.mod` but NOT in production binary):
- `github.com/tinywasm/mcpserve` — used in `orchestrator_test.go` and `setup_test.go` for real MCP protocol testing

**Distribution:**
```
mcp_client_test.go       → httptest.Server (JSON-RPC client isolation)
orchestrator_test.go     → *mcpserve.Handler (end-to-end real MCP flows)
setup_test.go            → *mcpserve.Handler (shared infrastructure + tool providers)
mock_mcp_test.go         → MockMCPClient (unit tests with controlled outputs)
```

**Example: `setup_test.go`**
```go
import "github.com/tinywasm/mcpserve" // test-only

var testHandler *mcpserve.Handler

func TestMain(m *testing.M) {
    testHandler = mcpserve.NewHandler(
        mcpserve.Config{Port: "0"},
        []mcpserve.ToolProvider{testProvider1, testProvider2},
        nil,
        nil,
    )
    go testHandler.Serve()
    // testHandler satisfies agent.MCPServer interface (has URL() string)
    code := m.Run()
    testHandler.Stop()
    os.Exit(code)
}
```

`MockMCPClient` is still required for unit tests where you need deterministic error injection or precise output control — scenarios that don't require the full MCP protocol.

### Real LLM Integration Tests (`//go:build integration`)

Require **Ollama** running locally with `qwen2.5:7b`. Skipped automatically if Ollama is not available. Run with:

```bash
go test -tags integration -run TestIntegration -v -timeout 300s ./...
```

**Model:** `qwen2.5:7b` (Q4_K_M, ~4.7 GB RAM)
- Spanish: excellent (100+ languages, production-realistic)
- Tool calling: ✓ via OpenAI-compatible API
- Context: 128K tokens
- Fallback: `llama3.1:8b`

**Guard pattern** — all integration tests begin with:
```go
//go:build integration

func ollamaAvailable() bool {
    resp, err := http.Get("http://localhost:11434/api/tags")
    return err == nil && resp.StatusCode == 200
}
```

**Clinic Scenarios (Spanish):**

| Test | Input | Assert |
|------|-------|--------|
| `TestIntegration_ClinicHours` | `"¿A qué hora abren los lunes?"` | Response mentions hours, in Spanish |
| `TestIntegration_SessionIsolation` | 2 goroutines, different sessionIDs, concurrent | Session A messages absent from Session B memory |

System prompt for clinic tests:
```
Eres la recepcionista de Clínica San Miguel.
Horario: Lunes a Viernes 8h-20h, Sábado 9h-14h.
Responde siempre en español, de forma concisa.
```

**Ollama installation (developer machine — Debian 12):**
```bash
curl -fsSL https://ollama.com/install.sh | sh
ollama pull qwen2.5:7b
# Verify:
curl http://localhost:11434/api/tags
```

---

## 8. FSM Implementation (`fsm.go`)

See [fsm.go](../fsm.go).

---

## 9. MCP Client Wire Types (`mcp_client.go`)

See [mcp_client.go](../mcp_client.go).
See also [mcp_registry.go](../mcp_registry.go) for MCPRegistry implementation.

---

## Appendix A: Reference LLM Adapter Implementations

> These files are **NOT part of the `agent` package**. They are reference implementations that
> users can copy into their own project as a starting point for their `LLMClient`.
> The `agent` library is provider-agnostic — bring your own adapter.

### A.1. Anthropic HTTP Adapter

Headers required: `x-api-key`, `anthropic-version: 2023-06-01`, `content-type: application/json`.

**Key adapter rule:** Consecutive internal `role="tool"` messages must be merged into a single `role="user"` Anthropic message containing `tool_result` content blocks before sending to the API.

```go
type anthropicMessage struct {
    Role    string `json:"role"`    // "user" or "assistant"
    Content any    `json:"content"` // string OR []contentBlock
}
type contentBlock struct {
    Type      string          `json:"type"`                  // "text"|"tool_use"|"tool_result"
    Text      string          `json:"text,omitempty"`
    ID        string          `json:"id,omitempty"`          // tool_use: call ID
    Name      string          `json:"name,omitempty"`        // tool_use: tool name
    Input     json.RawMessage `json:"input,omitempty"`       // tool_use: arguments
    Content   string          `json:"content,omitempty"`     // tool_result: observation
    ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result: correlates to tool_use ID
}
type anthropicToolDef struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"input_schema"` // note: snake_case for Anthropic
}
```

---

### A.2. Ollama HTTP Adapter (OpenAI-compatible)

Uses Ollama's **OpenAI-compatible endpoint** (`/v1/chat/completions`). Simpler than Anthropic — single message format, no merge needed for tool results.

```go
type OllamaClient struct {
    baseURL string        // default: "http://localhost:11434"
    model   string        // e.g. "qwen2.5:7b"
    timeout time.Duration // default: 120s (local CPU inference is slow)
}

func NewOllamaClient(model string) *OllamaClient {
    return &OllamaClient{
        baseURL: "http://localhost:11434",
        model:   model,
        timeout: 120 * time.Second,
    }
}
```

**Wire types** (OpenAI-compatible format):
```go
type openAIMessage struct {
    Role       string          `json:"role"`
    Content    string          `json:"content,omitempty"`
    ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
    ToolCallID string          `json:"tool_call_id,omitempty"`
}
type openAIToolCall struct {
    ID       string `json:"id"`
    Type     string `json:"type"` // always "function"
    Function struct {
        Name      string `json:"name"`
        Arguments string `json:"arguments"` // JSON string
    } `json:"function"`
}
type openAIRequest struct {
    Model    string          `json:"model"`
    Messages []openAIMessage `json:"messages"`
    Tools    []openAIToolDef `json:"tools,omitempty"`
    Stream   bool            `json:"stream"` // always false
}
type openAIResponse struct {
    Choices []struct {
        Message      openAIMessage `json:"message"`
        FinishReason string        `json:"finish_reason"` // "stop"|"tool_calls"
    } `json:"choices"`
    Usage struct {
        PromptTokens     int `json:"prompt_tokens"`
        CompletionTokens int `json:"completion_tokens"`
    } `json:"usage"`
}
```

**Key difference from Anthropic:** `role="tool"` messages pass directly as `role="tool"` with `tool_call_id`. No merging required — OpenAI format allows consecutive tool messages natively.

---

## 11. Constructor Pattern (`agent.go`)

See [agent.go](../agent.go).

---

## 12. Implementation Sequence

Build in this order — each step depends on the previous:

| Step | File(s) | Depends On |
|------|---------|------------|
| 1 | `types.go` + `interfaces.go` | nothing |
| 2 | `schema.go` + `memory.go` | types.go |
| 3 | `fsm.go` | nothing |
| 4 | `context_window.go` | MemoryStore, LLMClient interfaces |
| 5 | `mcp_client.go` + `mcp_registry.go` | types.go |
| 6 | `orchestrator.go` + `agent.go` | all previous |
| 7 | Tests for each step | corresponding step |
| 8 | `integration_test.go` (+ inline Ollama adapter) | all previous + Ollama running |

> **LLM adapter:** `integration_test.go` includes a minimal inline `OllamaClient` struct (test-only, not exported). No provider adapter is added to the production package.

---

## 13. Architecture Diagrams Reference

| Diagram | Description |
|---------|-------------|
| [System Context](diagrams/SYSTEM_CONTEXT.md) | Components and infrastructure overview |
| [ReAct + Reflection Flow](diagrams/REACT_FLOW.md) | Full reasoning loop with Reflection phase |
| [FSM State Machine](diagrams/FSM_STATE.md) | Agent state transitions and guardrails |
| [Memory Architecture](diagrams/MEMORY_ARCHITECTURE.md) | SQLite layers and search extensions |
| [MCP Client Flow](diagrams/MCP_CLIENT_FLOW.md) | JSON-RPC 2.0 tool discovery and execution |
| [Context Window Logic](diagrams/CONTEXT_WINDOW.md) | Token budget and summarization trigger |
| [Integration Test Scenario](diagrams/INTEGRATION_SCENARIO.md) | Clinic DDT scenario (Ollama integration tests) |
