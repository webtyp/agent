package agent

import (
	"time"
)

// Agent is the entry point for all agent operations.
// Constructed via New(cfg Config) — the only wiring point for concrete implementations.
type Agent struct {
	cfg      Config
	mem      MemoryStore
	llms     LLMConfig
	registry *mcpRegistry
	fsm      *fsm
}

// Run executes the full ReAct + Reflection loop for a single user query.
// sessionID scopes all memory reads/writes to an isolated conversation.
// Note: This is a placeholder for the method signature. The implementation will be in orchestrator.go or agent.go.
// func (a *Agent) Run(ctx context.Context, sessionID, userQuery string) (string, error)

// Message represents a single turn in the conversation. Stored in the messages table.
type Message struct {
	ID         string // UUID
	SessionID  string
	Role       string // "user" | "assistant" | "system" | "tool"
	Content    string // text content or tool result JSON
	ToolName   string // non-empty only when Role == "tool"
	ToolCallID string // correlates to the LLM tool_use ID
	ToolCalls  []ToolCall // populated when Role == "assistant" and StopReason == "tool_use"
	TokenCount int // tokens of this specific message alone (0 if unknown)
	CreatedAt  int64 // unixepoch
}

// Episode is a compressed summary of past messages. Stored in the episodes table.
type Episode struct {
	ID         string
	SessionID  string
	Summary    string // LLM-generated compression of FromMsgID..ToMsgID
	TokenCount int    // estimated tokens of the summary
	FromMsgID  string // first message ID included in the summary
	ToMsgID    string // last message ID included in the summary
	CreatedAt  int64  // unixepoch
}

// Knowledge is a semantic fact or rule stored in the knowledge table.
type Knowledge struct {
	ID        string
	SessionID string // NULL = global knowledge, shared across all sessions
	Content   string
	Source    string // default: "agent"
	CreatedAt int64  // unixepoch
}

// LLMRequest is the canonical input to LLMClient.Generate().
type LLMRequest struct {
	SystemPrompt string    // built from IdentityConfig at startup
	Messages     []Message // assembled by context_window.go
	Tools        []ToolDef // available tools for this reasoning turn
	MaxTokens    int       // provider-specific token budget
}

// LLMResponse is the canonical output of LLMClient.Generate().
type LLMResponse struct {
	Text       string     // final answer text (non-empty when StopReason == "end_turn")
	StopReason string     // "tool_use" | "end_turn"
	ToolCalls  []ToolCall // populated when StopReason == "tool_use"
	TokensUsed int        // total tokens consumed (prompt + completion)
}

// ToolDef is a canonical tool descriptor.
type ToolDef struct {
	Name        string // unique tool identifier
	Description string // human-readable purpose for the LLM
	InputSchema string // JSON Schema string (validated before execution)
}

// ToolCall is a single tool invocation requested by the LLM in a LLMResponse.
type ToolCall struct {
	ID    string // provider-specific correlation ID (e.g., Anthropic tool_use id)
	Name  string // tool name, matches ToolDef.Name
	Input string // raw JSON arguments (validated against ToolDef.InputSchema)
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

// ContextWindowConfig controls the token budget and summarization behavior.
type ContextWindowConfig struct {
	MaxTokens     int     // total provider context limit (e.g., 8192)
	BufferTokens  int     // reserved for system prompt + tool defs (e.g., 1024)
	MaxRecentMsgs int     // max messages loaded per turn (e.g., 20)
	MaxEpisodes   int     // max episodes loaded per turn (e.g., 5)
	SummarizeAt   float64 // fraction of budget that triggers summarization (default: 0.8)
}

// Config is the configuration struct for New().
type Config struct {
	Identity IdentityConfig
	LLMs     LLMConfig   // required: LLMs.Primary != nil
	Memory   MemoryStore // required

	// Tool sources — merged at startup into internal tool registry
	LocalTools  []Tool      // direct in-process tools
	MCPHandlers []MCPServer // programmatic references to running servers
	MCPServers  []string    // remote MCP server URLs (JSON-RPC 2.0)

	// Runtime tunables
	ContextWindow ContextWindowConfig
	MaxIterations int           // max Reasoning→Acting cycles (default: 10)
	MaxRetries    int           // max consecutive Acting failures before Responding (default: 3)
	MCPTimeout    time.Duration // default: 30s
}

// IdentityConfig defines the agent's persona.
type IdentityConfig struct {
	Name         string
	Role         string
	Instructions string
	Goals        []string
}

// LLMConfig holds the LLM clients for different tasks.
type LLMConfig struct {
	Primary    LLMClient // required
	Reflector  LLMClient // optional, defaults to Primary
	Summarizer LLMClient // optional, defaults to Primary
}
