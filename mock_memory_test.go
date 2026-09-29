package agent

import (
	"webtyp.com/agentcontext"
	"webtyp.com/context"
)

type mockMemory struct {
	turns     map[string][]agentcontext.Turn
	summaries map[string][]agentcontext.Summary
	knowledge []Knowledge
	toolLogs  []ToolLog
}

func newMockMemory() *mockMemory {
	return &mockMemory{
		turns:     make(map[string][]agentcontext.Turn),
		summaries: make(map[string][]agentcontext.Summary),
	}
}

func (m *mockMemory) EnsureSession(ctx *context.Context, sessionID string) error {
	if _, ok := m.turns[sessionID]; !ok {
		m.turns[sessionID] = []agentcontext.Turn{}
		m.summaries[sessionID] = []agentcontext.Summary{}
	}
	return nil
}

func (m *mockMemory) AppendTurn(ctx *context.Context, sessionID string, t agentcontext.Turn) error {
	m.turns[sessionID] = append(m.turns[sessionID], t)
	return nil
}

func (m *mockMemory) GetTurns(ctx *context.Context, sessionID string, limit int) ([]agentcontext.Turn, error) {
	list := m.turns[sessionID]
	if limit > 0 && len(list) > limit {
		list = list[len(list)-limit:]
	}
	out := make([]agentcontext.Turn, len(list))
	copy(out, list)
	return out, nil
}

func (m *mockMemory) DeleteTurns(ctx *context.Context, sessionID string, ids []string) error {
	var filtered []agentcontext.Turn
	for _, t := range m.turns[sessionID] {
		remove := false
		for _, id := range ids {
			if t.ID == id {
				remove = true
				break
			}
		}
		if !remove {
			filtered = append(filtered, t)
		}
	}
	m.turns[sessionID] = filtered
	return nil
}

func (m *mockMemory) SaveSummary(ctx *context.Context, sessionID string, s agentcontext.Summary) error {
	m.summaries[sessionID] = append(m.summaries[sessionID], s)
	return nil
}

func (m *mockMemory) GetSummaries(ctx *context.Context, sessionID string, limit int) ([]agentcontext.Summary, error) {
	list := m.summaries[sessionID]
	if limit > 0 && len(list) > limit {
		list = list[len(list)-limit:]
	}
	out := make([]agentcontext.Summary, len(list))
	copy(out, list)
	return out, nil
}

func (m *mockMemory) SaveKnowledge(ctx *context.Context, sessionID, content, source string) error {
	k := Knowledge{
		ID:        "k-1",
		SessionID: sessionID,
		Content:   content,
		Source:    source,
	}
	m.knowledge = append(m.knowledge, k)
	return nil
}

func (m *mockMemory) SearchKnowledge(ctx *context.Context, query, sessionID string, limit int) ([]Knowledge, error) {
	return m.knowledge, nil
}

func (m *mockMemory) LogToolCall(ctx *context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error {
	log := ToolLog{
		ID:         "tl-1",
		SessionID:  sessionID,
		ToolName:   toolName,
		InputJSON:  inputJSON,
		OutputText: outputText,
		ErrText:    errText,
		DurationMS: durationMS,
	}
	m.toolLogs = append(m.toolLogs, log)
	return nil
}

func (m *mockMemory) GetToolLogs(ctx *context.Context, sessionID, toolName string, limit int) ([]ToolLog, error) {
	return m.toolLogs, nil
}
