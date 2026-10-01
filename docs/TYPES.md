# Types — `webtyp/agent`

This page says **which library owns each type** the agent uses, and documents the ones declared
here. The code (`types.go`, `texts.go`, `guard.go`) is the source of truth for fields, and this page explains how the
types fit together.

## Who owns what

Each type lives in the library that reads it (decision D1 of the
[ecosystem master plan](AGENT_ECOSYSTEM_MASTER_PLAN.md)). Nothing is re-exported under a second
name.

| Type | Library | What it is |
|---|---|---|
| `llm.Message`, `llm.Role`, `llm.ToolCall` | `webtyp/llm` | one entry of the conversation as the model sees it |
| `llm.Request`, `llm.Response`, `llm.StopReason`, `llm.Usage`, `llm.ToolDef`, `llm.Decider` | `webtyp/llm` | model exchanges and decider contract |
| `llm.Client`, `llm.TokenCounter` | `webtyp/llm` | the model and its tokenizer |
| `agentcontext.Turn` | `webtyp/agentcontext` | an `llm.Message` as stored: ID, token count, timestamp |
| `agentcontext.Summary` | `webtyp/agentcontext` | model-written text that replaced a range of turns |
| `Agent`, `Config`, `Reply`, `Texts`, `Template`, `Data`, `Guard`, `Tool`, `Knowledge`, `ToolLog` | `webtyp/agent` (this repo) | below |

## Declared here

### `Agent`

The value `New(cfg)` returns. Its fields are unexported, and its public methods are:
- `Run(ctx, sessionID, userQuery) (Reply, error)`
- `Confirm(ctx, sessionID) (Reply, error)`
- `Decline(ctx, sessionID) (Reply, error)`

### `Reply`

```go
type Reply struct {
	Text    string         // answer to show the person. While Pending is non-empty it is Texts.Confirm.
	Pending []llm.ToolCall // tool calls that change data, waiting for the person: show them and call Confirm or Decline.
}
```

### `Config`

```go
type Config struct {
	Decider   llm.Decider // required: picks the tool, enum arguments, yes/no answers; checks injection and the writer
	Writer    llm.Client  // optional: phrases a tool's data when no template answers; nil = the data is shown as is
	Texts     Texts       // required: the application's words (texts.go)
	Templates []Template  // optional: answers from one tool's result, in the application's words
	Guard     Guard       // the code check every message passes first (guard.go)

	Tokens llm.TokenCounter  // required: counts the tokens of each stored turn
	Memory MemoryStore       // required
	IDGen  model.IDGenerator // required
	Clock  Clock             // the users' time and timezone (default MachineClock)

	ToolIndex  ToolIndex // required: finds the candidate tools for a message (NewMemToolIndex for keywords)
	Candidates int       // tools the decision model chooses among, 1..9 (default 5)

	LocalTools  []Tool      // in-process Go tools
	MCPHandlers []MCPServer // MCP servers running in the same process
	MCPServers  []string    // remote MCP base URLs ("/mcp" is appended by webtyp/mcp)

	WriterMaxTokens int // default 128
	MCPTimeoutMS    int // default 30000
}
```

### `Texts`

```go
type Texts struct {
	Speaker      string
	Assistant    string
	NoToolOption string

	NoTool   string
	Refused  string
	TooLong  string
	Clarify  string
	Confirm  string
	Declined string
	Failed   string
	Yes      string
	No       string
	Found    string

	WriterSystem  string
	DataLabel     string
	QuestionLabel string
}
```

### `Template` & `Data`

```go
type Template struct {
	Tool   string
	Answer func(d Data) (string, bool)
}

type Data struct {
	Message          string
	Result           string
	Now              int64
	UTCOffsetMinutes int
}
```

### `Guard`

```go
type Guard struct {
	MaxChars int
	Phrases  []string
	Roles    []string
}
```

### `Clock`

```go
type Clock interface {
	Now() int64            // unix nanoseconds, UTC
	UTCOffsetMinutes() int // e.g. -180 for UTC-3
}
```

### `Tool`

```go
type Tool interface {
	Name() string
	Description() string
	InputSchema() string
	Action() model.Action
	Execute(ctx *context.Context, argsJSON string) (string, error)
}
```

### `Knowledge`

```go
type Knowledge struct {
	ID        string
	SessionID string
	Content   string
	Source    string // default "agent"
	CreatedAt int64  // unixepoch
}
```

### `ToolLog`

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
