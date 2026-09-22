package agent

import "webtyp.com/context"

type LLMClient interface {
	Generate(ctx *context.Context, req LLMRequest) (LLMResponse, error)
}

// Each contract is what one collaborator of the orchestrator actually needs. See
// docs/MASTER_PLAN.md D7/D8 and docs/plans/agent.md §2 for the argument.
type ConversationStore interface {
	EnsureSession(ctx *context.Context, sessionID string) error
	AppendMessage(ctx *context.Context, sessionID string, msg Message) error
	GetMessages(ctx *context.Context, sessionID string, limit int) ([]Message, error)
	DeleteMessages(ctx *context.Context, sessionID string, ids []string) error
}

type EpisodeStore interface {
	SaveEpisode(ctx *context.Context, sessionID, summary string, tokenCount int, fromID, toID string) error
	GetEpisodes(ctx *context.Context, sessionID string, limit int) ([]Episode, error)
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

// MemoryStore is the composed contract. Config.Memory keeps this type — structurally
// identical to the old 11-method interface, so no call site changes.
type MemoryStore interface {
	ConversationStore
	EpisodeStore
	KnowledgeStore
	ToolLogStore
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
