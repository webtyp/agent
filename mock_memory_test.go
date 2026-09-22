package agent

import "webtyp.com/context"

// MockMemoryStore implements MemoryStore with injectable function fields.
// Each method delegates to its Func field if set, otherwise returns zero values.
// Pattern: same as MockLLMClient.GenerateFunc.
var _ MemoryStore = (*MockMemoryStore)(nil)

type MockMemoryStore struct {
	EnsureSessionFunc   func(ctx *context.Context, sessionID string) error
	AppendMessageFunc   func(ctx *context.Context, sessionID string, msg Message) error
	GetMessagesFunc     func(ctx *context.Context, sessionID string, limit int) ([]Message, error)
	DeleteMessagesFunc  func(ctx *context.Context, sessionID string, ids []string) error
	SaveEpisodeFunc     func(ctx *context.Context, sessionID, summary string, tokenCount int, fromID, toID string) error
	GetEpisodesFunc     func(ctx *context.Context, sessionID string, limit int) ([]Episode, error)
	SaveKnowledgeFunc   func(ctx *context.Context, sessionID, content, source string) error
	SearchKnowledgeFunc func(ctx *context.Context, query, sessionID string, limit int) ([]Knowledge, error)
	LogToolCallFunc     func(ctx *context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error
	GetToolLogsFunc     func(ctx *context.Context, sessionID, toolName string, limit int) ([]ToolLog, error)
}

func (m *MockMemoryStore) EnsureSession(ctx *context.Context, sessionID string) error {
	if m.EnsureSessionFunc != nil {
		return m.EnsureSessionFunc(ctx, sessionID)
	}
	return nil
}

func (m *MockMemoryStore) AppendMessage(ctx *context.Context, sessionID string, msg Message) error {
	if m.AppendMessageFunc != nil {
		return m.AppendMessageFunc(ctx, sessionID, msg)
	}
	return nil
}

func (m *MockMemoryStore) GetMessages(ctx *context.Context, sessionID string, limit int) ([]Message, error) {
	if m.GetMessagesFunc != nil {
		return m.GetMessagesFunc(ctx, sessionID, limit)
	}
	return nil, nil
}

func (m *MockMemoryStore) DeleteMessages(ctx *context.Context, sessionID string, ids []string) error {
	if m.DeleteMessagesFunc != nil {
		return m.DeleteMessagesFunc(ctx, sessionID, ids)
	}
	return nil
}

func (m *MockMemoryStore) SaveEpisode(ctx *context.Context, sessionID, summary string, tokenCount int, fromID, toID string) error {
	if m.SaveEpisodeFunc != nil {
		return m.SaveEpisodeFunc(ctx, sessionID, summary, tokenCount, fromID, toID)
	}
	return nil
}

func (m *MockMemoryStore) GetEpisodes(ctx *context.Context, sessionID string, limit int) ([]Episode, error) {
	if m.GetEpisodesFunc != nil {
		return m.GetEpisodesFunc(ctx, sessionID, limit)
	}
	return nil, nil
}

func (m *MockMemoryStore) SaveKnowledge(ctx *context.Context, sessionID, content, source string) error {
	if m.SaveKnowledgeFunc != nil {
		return m.SaveKnowledgeFunc(ctx, sessionID, content, source)
	}
	return nil
}

func (m *MockMemoryStore) SearchKnowledge(ctx *context.Context, query, sessionID string, limit int) ([]Knowledge, error) {
	if m.SearchKnowledgeFunc != nil {
		return m.SearchKnowledgeFunc(ctx, query, sessionID, limit)
	}
	return nil, nil
}

func (m *MockMemoryStore) LogToolCall(ctx *context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error {
	if m.LogToolCallFunc != nil {
		return m.LogToolCallFunc(ctx, sessionID, toolName, inputJSON, outputText, errText, durationMS)
	}
	return nil
}

func (m *MockMemoryStore) GetToolLogs(ctx *context.Context, sessionID, toolName string, limit int) ([]ToolLog, error) {
	if m.GetToolLogsFunc != nil {
		return m.GetToolLogsFunc(ctx, sessionID, toolName, limit)
	}
	return nil, nil
}
