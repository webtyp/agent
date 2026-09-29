package agent

import (
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/llm"
)

type ConversationStore interface {
	EnsureSession(ctx *context.Context, sessionID string) error
	AppendTurn(ctx *context.Context, sessionID string, t agentcontext.Turn) error
	GetTurns(ctx *context.Context, sessionID string, limit int) ([]agentcontext.Turn, error) // the `limit` most recent
	DeleteTurns(ctx *context.Context, sessionID string, ids []string) error
}

type SummaryStore interface {
	SaveSummary(ctx *context.Context, sessionID string, s agentcontext.Summary) error
	GetSummaries(ctx *context.Context, sessionID string, limit int) ([]agentcontext.Summary, error) // the `limit` most recent
}

// KnowledgeStore's SearchKnowledge takes TEXT, never a vector — the caller (agentmemory)
// owns embedding it. sessionID == "" scopes to global knowledge; a non-empty sessionID must
// see its own session's knowledge PLUS global — never another session's. See conformance
// tests TestKnowledge_GlobalVisibleFromAnySession / TestKnowledge_SessionScopedNotVisibleFromOtherSession.
type KnowledgeStore interface {
	SaveKnowledge(ctx *context.Context, sessionID, content, source string) error
	SearchKnowledge(ctx *context.Context, query, sessionID string, limit int) ([]Knowledge, error)
}

type ToolLogStore interface {
	LogToolCall(ctx *context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error
	GetToolLogs(ctx *context.Context, sessionID, toolName string, limit int) ([]ToolLog, error)
}

// MemoryStore is the composed contract. Config.Memory keeps this type.
type MemoryStore interface {
	ConversationStore
	SummaryStore
	KnowledgeStore
	ToolLogStore
}

// ToolIndex finds, among every tool the agent can run, the ones that match what the model
// is looking for. webtyp/agentmemory implements it by meaning; NewMemToolIndex by keywords.
type ToolIndex interface {
	// IndexTools replaces the indexed set with tools.
	IndexTools(ctx *context.Context, tools []llm.ToolDef) error
	// SearchTools returns the names of at most limit tools, most relevant first; empty when none match.
	SearchTools(ctx *context.Context, query string, limit int) ([]string, error)
}

type MCPServer interface {
	// URL is the server's base URL. webtyp.com/mcp.NewClient appends "/mcp" to it — pass
	// the host root (e.g. "https://host"), not a path already ending in "/mcp".
	URL() string
}

type Tool interface {
	Name() string
	Description() string
	InputSchema() string
	Execute(ctx *context.Context, argsJSON string) (string, error)
}
