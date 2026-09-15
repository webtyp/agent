# Canonical Type Definitions: `tinywasm/agent`

This document defines all value types used by the interfaces and constructors in this library.
These types live in `types.go` and are the source of truth for all Go struct definitions.

Linked from: [ARCHITECTURE.md](ARCHITECTURE.md) · [IMPLEMENTATION.md](IMPLEMENTATION.md)

---

## Agent (Opaque Root)

`Agent` is the DI root returned by `New()`. Its internals are unexported.

```go
// Agent is the entry point for all agent operations.
// Constructed via New(cfg Config) — the only wiring point for concrete implementations.
type Agent struct {
    // unexported fields: cfg Config, mem MemoryStore, llms LLMConfig,
    // registry *mcpRegistry, fsm *fsm
}

// Run executes the full ReAct + Reflection loop for a single user query.
// sessionID scopes all memory reads/writes to an isolated conversation.
func (a *Agent) Run(ctx context.Context, sessionID, userQuery string) (string, error)
```

---

## Message

Represents a single turn in the conversation. Stored in the `messages` table.

```go
type Message struct {
    ID         string // UUID
    SessionID  string
    Role       string // "user" | "assistant" | "system" | "tool"
    Content    string // text content or tool result JSON
    ToolName   string // non-empty only when Role == "tool"
    ToolCallID string // correlates to the LLM tool_use ID
    TokenCount int
    CreatedAt  int64  // unixepoch
}
```

---

## Episode

A compressed summary of past messages. Stored in the `episodes` table.

```go
type Episode struct {
    ID         string
    SessionID  string
    Summary    string // LLM-generated compression of FromMsgID..ToMsgID
    TokenCount int    // estimated tokens of the summary
    FromMsgID  string // first message ID included in the summary
    ToMsgID    string // last message ID included in the summary
    CreatedAt  int64  // unixepoch
}
```

---

## Knowledge

A semantic fact or rule stored in the `knowledge` table.

```go
type Knowledge struct {
    ID        string
    SessionID string // NULL = global knowledge, shared across all sessions
    Content   string
    Source    string // default: "agent"
    CreatedAt int64  // unixepoch
}
```

> **Session scoping:** `SessionID == ""` (NULL in SQL) means the knowledge is global and
> returned by `SearchKnowledge` for any session. Non-empty `SessionID` means it is private
> to that session. See [MEMORY.md, section 2.3](history/MEMORY_SQLITE.md) for the WHERE clause used at retrieval time.

---

## LLMRequest

The canonical input to `LLMClient.Generate()`.

```go
type LLMRequest struct {
    SystemPrompt string    // built from IdentityConfig at startup
    Messages     []Message // assembled by context_window.go
    Tools        []ToolDef // available tools for this reasoning turn
    MaxTokens    int       // provider-specific token budget
}
```

---

## LLMResponse

The canonical output of `LLMClient.Generate()`.

```go
type LLMResponse struct {
    Text       string     // final answer text (non-empty when StopReason == "end_turn")
    StopReason string     // "tool_use" | "end_turn"
    ToolCalls  []ToolCall // populated when StopReason == "tool_use"
    TokensUsed int        // total tokens consumed (prompt + completion)
}
```

---

## ToolDef

A canonical tool descriptor. Used by LLM adapters to build provider-specific wire formats
(e.g., `anthropicToolDef`, `openAIToolDef`) and by `mcp_registry.go` to adapt MCP
`tools/list` responses into the internal registry.

```go
type ToolDef struct {
    Name        string // unique tool identifier
    Description string // human-readable purpose for the LLM
    InputSchema string // JSON Schema string (validated before execution)
}
```

---

## ToolCall

A single tool invocation requested by the LLM in a `LLMResponse`.

```go
type ToolCall struct {
    ID    string // provider-specific correlation ID (e.g., Anthropic tool_use id)
    Name  string // tool name, matches ToolDef.Name
    Input string // raw JSON arguments (validated against ToolDef.InputSchema)
}
```

---

## ToolLog

An audit record of a single tool execution. Stored in the `tool_logs` table.

```go
type ToolLog struct {
    ID         string
    SessionID  string
    ToolName   string
    InputJSON  string
    OutputText string // empty on error
    ErrText    string // empty on success
    DurationMS int64
    CreatedAt  int64 // unixepoch
}
```

---

## ContextWindowConfig

Controls the token budget and summarization behavior of `context_window.go`.

```go
type ContextWindowConfig struct {
    MaxTokens      int // total provider context limit (e.g., 8192)
    BufferTokens   int // reserved for system prompt + tool defs (e.g., 1024)
    MaxRecentMsgs  int // max messages loaded per turn (e.g., 20)
    MaxEpisodes    int // max episodes loaded per turn (e.g., 5)
    SummarizeAt    float64 // fraction of budget that triggers summarization (default: 0.8)
}
```

> **Summarization — "oldest 50%" algorithm:** When token usage exceeds `SummarizeAt × MaxTokens`,
> the orchestrator summarizes the first `⌊len(messages)/2⌋` messages in ascending `created_at` order
> (i.e., the earliest half of the current window slice). The summary is saved as an `Episode`
> and those messages are deleted from the `messages` table.
