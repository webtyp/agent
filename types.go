package agent

import (
	"webtyp.com/llm"
	"webtyp.com/model"
)

// Reply is what the agent has for the person after Run, Confirm or Decline.
type Reply struct {
	// Text is the answer to show. While Pending is non-empty it is Texts.Confirm.
	Text string
	// Pending are the tool calls that change data, waiting for the person: show them and call Confirm or Decline. They have not run.
	Pending []llm.ToolCall
}

// Agent is the entry point for all agent operations.
// Constructed via New(cfg Config) — the only wiring point for concrete implementations.
type Agent struct {
	cfg      Config
	mem      MemoryStore
	registry *mcpRegistry
	idGen    model.IDGenerator
}

// Knowledge is a semantic fact or rule stored in the knowledge table.
type Knowledge struct {
	ID        string
	SessionID string // NULL = global knowledge, shared across all sessions
	Content   string
	Source    string // default: "agent"
	CreatedAt int64  // unixepoch
}

// ToolLog is an audit record of a single tool execution. Stored in the tool_logs table.
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

// Config is the configuration struct for New(). See docs/HYBRID_DESIGN.md for the turn it drives.
type Config struct {
	Decider   llm.Decider // required: picks the tool, enum arguments, yes/no answers; checks injection and the writer
	Writer    llm.Client  // optional: phrases a tool's data when no template answers; nil = the data is shown as is
	Texts     Texts       // required: the application's words (texts.go)
	Templates []Template  // optional: answers from one tool's result, in the application's words
	Guard     Guard       // the code check every message passes first (guard.go)

	Tokens llm.TokenCounter  // required: counts the tokens of each stored turn
	Memory MemoryStore       // required
	IDGen  model.IDGenerator // required
	Clock  Clock             // the users' time and timezone (default: this machine's, MachineClock)

	ToolIndex  ToolIndex // required: finds the candidate tools for a message (NewMemToolIndex for keywords)
	Candidates int       // tools the decision model chooses among, 1..9 (default 5)

	LocalTools  []Tool
	MCPHandlers []MCPServer
	MCPServers  []string

	WriterMaxTokens int // the longest answer the writer may write (default 128)
	MCPTimeoutMS    int // default 30000
}
