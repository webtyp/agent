package agent

import (
	"webtyp.com/agentcontext"
	"webtyp.com/llm"
	"webtyp.com/model"
)

// Reply is what the agent has for the person after Run, Confirm or Decline.
type Reply struct {
	// Text is the answer to show. While Pending is non-empty it is whatever the model wrote
	// before its tool calls, possibly empty.
	Text string
	// Pending are the tool calls the model wants to make that change data. They have not run.
	// Show them to the person and call Confirm or Decline. Empty when the answer is final.
	Pending []llm.ToolCall
}

// Agent is the entry point for all agent operations.
// Constructed via New(cfg Config) — the only wiring point for concrete implementations.
type Agent struct {
	cfg      Config
	mem      MemoryStore
	llms     LLMConfig
	registry *mcpRegistry
	fsm      *fsm
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

// Config is the configuration struct for New().
type Config struct {
	Identity agentcontext.Identity
	LLMs     LLMConfig           // required: LLMs.Primary != nil
	Critic   llm.Decider         // optional: checks each answer before it reaches the person; nil = no check
	Tokens   llm.TokenCounter    // required: the tokenizer of LLMs.Primary
	Budget   agentcontext.Budget // required: the token limits of LLMs.Primary
	Memory   MemoryStore         // required
	IDGen    model.IDGenerator   // required
	Clock    Clock               // the users' time and timezone (default: this machine's, MachineClock)

	ToolIndex       ToolIndex // required: finds tools for search_tools (NewMemToolIndex for keywords)
	ToolSearchLimit int       // tools returned per search (default 5)

	RecentTurns     int // turns loaded per reasoning step (default 20)
	RecentSummaries int // summaries loaded per reasoning step (default 5)

	LocalTools  []Tool
	MCPHandlers []MCPServer
	MCPServers  []string

	MaxIterations int // default 10
	MaxRetries    int // default 3
	MCPTimeoutMS  int // default 30000
}

// LLMConfig holds the LLM clients for different tasks.
type LLMConfig struct {
	Primary    llm.Client // required
	Summarizer llm.Client // optional, defaults to Primary
}
