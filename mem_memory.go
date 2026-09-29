package agent

import (
	"sync"

	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/fmt"
)

type sessionTurns struct {
	sessionID string
	turns     []agentcontext.Turn
}

type sessionSummaries struct {
	sessionID string
	summaries []agentcontext.Summary
}

type memMemory struct {
	mu        sync.RWMutex
	turns     []sessionTurns
	summaries []sessionSummaries
	knowledge []Knowledge
	toolLogs  []ToolLog
}

// NewMemMemory returns an in-memory MemoryStore reference implementation.
func NewMemMemory() MemoryStore {
	return &memMemory{}
}

func (m *memMemory) EnsureSession(ctx *context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.turns {
		if s.sessionID == sessionID {
			return nil
		}
	}
	m.turns = append(m.turns, sessionTurns{sessionID: sessionID})
	m.summaries = append(m.summaries, sessionSummaries{sessionID: sessionID})
	return nil
}

func (m *memMemory) AppendTurn(ctx *context.Context, sessionID string, t agentcontext.Turn) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, s := range m.turns {
		if s.sessionID == sessionID {
			m.turns[i].turns = append(m.turns[i].turns, t)
			return nil
		}
	}
	m.turns = append(m.turns, sessionTurns{sessionID: sessionID, turns: []agentcontext.Turn{t}})
	return nil
}

func (m *memMemory) GetTurns(ctx *context.Context, sessionID string, limit int) ([]agentcontext.Turn, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, s := range m.turns {
		if s.sessionID == sessionID {
			list := s.turns
			if limit > 0 && len(list) > limit {
				list = list[len(list)-limit:]
			}
			out := make([]agentcontext.Turn, len(list))
			copy(out, list)
			return out, nil
		}
	}
	return nil, nil
}

func (m *memMemory) DeleteTurns(ctx *context.Context, sessionID string, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, s := range m.turns {
		if s.sessionID == sessionID {
			var filtered []agentcontext.Turn
			for _, t := range s.turns {
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
			m.turns[i].turns = filtered
			return nil
		}
	}
	return nil
}

func (m *memMemory) SaveSummary(ctx *context.Context, sessionID string, s agentcontext.Summary) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, sum := range m.summaries {
		if sum.sessionID == sessionID {
			m.summaries[i].summaries = append(m.summaries[i].summaries, s)
			return nil
		}
	}
	m.summaries = append(m.summaries, sessionSummaries{sessionID: sessionID, summaries: []agentcontext.Summary{s}})
	return nil
}

func (m *memMemory) GetSummaries(ctx *context.Context, sessionID string, limit int) ([]agentcontext.Summary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, sum := range m.summaries {
		if sum.sessionID == sessionID {
			list := sum.summaries
			if limit > 0 && len(list) > limit {
				list = list[len(list)-limit:]
			}
			out := make([]agentcontext.Summary, len(list))
			copy(out, list)
			return out, nil
		}
	}
	return nil, nil
}

func (m *memMemory) SaveKnowledge(ctx *context.Context, sessionID, content, source string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := Knowledge{
		ID:        fmt.Sprintf("k-%d", len(m.knowledge)+1),
		SessionID: sessionID,
		Content:   content,
		Source:    source,
	}
	m.knowledge = append(m.knowledge, k)
	return nil
}

func (m *memMemory) SearchKnowledge(ctx *context.Context, query, sessionID string, limit int) ([]Knowledge, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []Knowledge
	for _, k := range m.knowledge {
		if k.SessionID == "" || k.SessionID == sessionID {
			if query == "" || fmt.Contains(k.Content, query) {
				result = append(result, k)
				if limit > 0 && len(result) >= limit {
					break
				}
			}
		}
	}
	return result, nil
}

func (m *memMemory) LogToolCall(ctx *context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	log := ToolLog{
		ID:         fmt.Sprintf("tl-%d", len(m.toolLogs)+1),
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

func (m *memMemory) GetToolLogs(ctx *context.Context, sessionID, toolName string, limit int) ([]ToolLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []ToolLog
	for _, log := range m.toolLogs {
		if log.SessionID == sessionID && (toolName == "" || log.ToolName == toolName) {
			result = append(result, log)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}
