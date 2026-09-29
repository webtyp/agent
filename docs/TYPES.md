# Types — `webtyp/agent`

This page says **which library owns each type** the agent uses, and documents the ones declared
here. The code (`types.go`) is the source of truth for fields, and this page explains how the
types fit together.

## Who owns what

Each type lives in the library that reads it (decision D1 of the
[ecosystem master plan](AGENT_ECOSYSTEM_MASTER_PLAN.md)). Nothing is re-exported under a second
name.

| Type | Library | What it is |
|---|---|---|
| `llm.Message`, `llm.Role`, `llm.ToolCall` | `webtyp/llm` | one entry of the conversation as the model sees it |
| `llm.Request`, `llm.Response`, `llm.StopReason`, `llm.Usage`, `llm.ToolDef` | `webtyp/llm` | one exchange with the model |
| `llm.Client`, `llm.TokenCounter` | `webtyp/llm` | the model and its tokenizer |
| `agentcontext.Turn` | `webtyp/agentcontext` | an `llm.Message` as stored: ID, token count, timestamp |
| `agentcontext.Summary` | `webtyp/agentcontext` | model-written text that replaced a range of turns |
| `agentcontext.Identity` | `webtyp/agentcontext` | who the agent is; becomes the request's `System` |
| `agentcontext.Budget` | `webtyp/agentcontext` | the model's context and output token limits |
| `Agent`, `Config`, `LLMConfig`, `Knowledge`, `ToolLog` | `webtyp/agent` (this repo) | below |

## Declared here

### `Agent`

The value `New(cfg)` returns. Its fields are unexported, and its one method is
`Run(ctx, sessionID, userQuery) (string, error)`.

### `Config`

```go
type Config struct {
	Identity agentcontext.Identity
	LLMs     LLMConfig           // required: LLMs.Primary != nil
	Tokens   llm.TokenCounter    // required: the tokenizer of LLMs.Primary
	Budget   agentcontext.Budget // required: the token limits of LLMs.Primary
	Memory   MemoryStore         // required
	IDGen    model.IDGenerator   // required, e.g. webtyp.com/unixid

	RecentTurns     int // turns loaded per reasoning step (default 20)
	RecentSummaries int // summaries loaded per reasoning step (default 5)

	LocalTools  []Tool      // in-process Go tools
	MCPHandlers []MCPServer // MCP servers running in the same process
	MCPServers  []string    // remote MCP base URLs ("/mcp" is appended by webtyp/mcp)

	MaxIterations int // Reasoning→Acting cycles before giving up (default 10)
	MaxRetries    int // consecutive tool failures before responding (default 3)
	MCPTimeoutMS  int // default 30000
}
```

`Tokens` and `Budget` have no default. The context size and tokenizer are facts of the model,
and a guessed value would silently mis-budget every request.

### `LLMConfig`

```go
type LLMConfig struct {
	Primary    llm.Client // reasoning and acting (required)
	Reflector  llm.Client // judges the candidate answer (defaults to Primary)
	Summarizer llm.Client // writes summaries when the conversation is compacted (defaults to Primary)
}
```

### `Knowledge`

A fact or rule the agent can search. `SessionID == ""` means global: visible from every session.
A non-empty `SessionID` is visible only from that session.

```go
type Knowledge struct {
	ID        string
	SessionID string
	Content   string
	Source    string // who added it, e.g. "agent" or a document name
	CreatedAt int64  // unix seconds
}
```

### `ToolLog`

The audit record of one tool execution.

```go
type ToolLog struct {
	ID         string
	SessionID  string
	ToolName   string
	InputJSON  string
	OutputText string // empty on error
	ErrText    string // empty on success
	DurationMS int64
	CreatedAt  int64 // unix seconds
}
```
