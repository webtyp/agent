package agent

import (
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/llm"
	"webtyp.com/model"
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

type KnowledgeStore interface {
	SaveKnowledge(ctx *context.Context, sessionID, content, source string) error
	SearchKnowledge(ctx *context.Context, query, sessionID string, limit int) ([]Knowledge, error)
}

type ToolLogStore interface {
	LogToolCall(ctx *context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error
	GetToolLogs(ctx *context.Context, sessionID, toolName string, limit int) ([]ToolLog, error)
}

type MemoryStore interface {
	ConversationStore
	SummaryStore
	KnowledgeStore
	ToolLogStore
}

type Clock interface {
	Now() int64
	UTCOffsetMinutes() int
}

type ToolIndex interface {
	IndexTools(ctx *context.Context, tools []llm.ToolDef) error
	SearchTools(ctx *context.Context, query string, limit int) ([]string, error)
}

type MCPServer interface {
	URL() string
}

type Tool interface {
	Name() string
	Description() string
	InputSchema() string
	Action() model.Action
	Execute(ctx *context.Context, argsJSON string) (string, error)
}
