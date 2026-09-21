package agent

import (
	"context"
	"sort"
	"sync"

	"webtyp.com/fmt"
	"webtyp.com/model"
)

// NewMemMemory returns an in-memory MemoryStore with no external dependency — the reference
// implementation any other MemoryStore is checked against via conformance.Run. Not for
// production use: SearchKnowledge does a case-insensitive substring match, not semantic
// search (that requires an embedder, which this package deliberately does not depend on —
// see MASTER_PLAN.md D7/§5). idGen mints record ids — never construct a concrete generator
// inside this package (model.IDGenerator's own doc comment says so); the caller injects one
// (e.g. webtyp.com/unixid). Safe for concurrent use.
func NewMemMemory(idGen model.IDGenerator) MemoryStore {
	return &memMemory{idGen: idGen}
}

type memMemory struct {
	mu        sync.Mutex
	idGen     model.IDGenerator
	messages  []Message
	episodes  []Episode
	knowledge []Knowledge
	toolLogs  []ToolLog
}

func (m *memMemory) EnsureSession(ctx context.Context, sessionID string) error {
	return nil
}

func (m *memMemory) AppendMessage(ctx context.Context, sessionID string, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg.SessionID == "" {
		msg.SessionID = sessionID
	}
	m.messages = append(m.messages, msg)
	return nil
}

func (m *memMemory) GetMessages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var matched []Message
	for _, msg := range m.messages {
		if msg.SessionID == sessionID {
			matched = append(matched, msg)
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].CreatedAt < matched[j].CreatedAt
	})

	if limit > 0 && len(matched) > limit {
		matched = matched[len(matched)-limit:]
	}

	res := make([]Message, len(matched))
	copy(res, matched)
	return res, nil
}

func (m *memMemory) DeleteMessages(ctx context.Context, sessionID string, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var updated []Message
	for _, msg := range m.messages {
		if msg.SessionID == sessionID && containsString(ids, msg.ID) {
			continue
		}
		updated = append(updated, msg)
	}
	m.messages = updated
	return nil
}

func (m *memMemory) SaveEpisode(ctx context.Context, sessionID, summary string, tokenCount int, fromID, toID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.episodes = append(m.episodes, Episode{
		ID:         m.idGen.NewID(),
		SessionID:  sessionID,
		Summary:    summary,
		TokenCount: tokenCount,
		FromMsgID:  fromID,
		ToMsgID:    toID,
	})
	return nil
}

func (m *memMemory) GetEpisodes(ctx context.Context, sessionID string, limit int) ([]Episode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var matched []Episode
	for _, ep := range m.episodes {
		if ep.SessionID == sessionID {
			matched = append(matched, ep)
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].CreatedAt < matched[j].CreatedAt
	})

	if limit > 0 && len(matched) > limit {
		matched = matched[len(matched)-limit:]
	}

	res := make([]Episode, len(matched))
	copy(res, matched)
	return res, nil
}

func (m *memMemory) SaveKnowledge(ctx context.Context, sessionID, content, source string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.knowledge = append(m.knowledge, Knowledge{
		ID:        m.idGen.NewID(),
		SessionID: sessionID,
		Content:   content,
		Source:    source,
	})
	return nil
}

func (m *memMemory) SearchKnowledge(ctx context.Context, query, sessionID string, limit int) ([]Knowledge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	q := fmt.ToLower(query)
	var matched []Knowledge
	for _, k := range m.knowledge {
		if k.SessionID == "" || k.SessionID == sessionID {
			if fmt.Contains(fmt.ToLower(k.Content), q) {
				matched = append(matched, k)
			}
		}
	}

	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}

	res := make([]Knowledge, len(matched))
	copy(res, matched)
	return res, nil
}

func (m *memMemory) LogToolCall(ctx context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolLogs = append(m.toolLogs, ToolLog{
		ID:         m.idGen.NewID(),
		SessionID:  sessionID,
		ToolName:   toolName,
		InputJSON:  inputJSON,
		OutputText: outputText,
		ErrText:    errText,
		DurationMS: durationMS,
	})
	return nil
}

func (m *memMemory) GetToolLogs(ctx context.Context, sessionID, toolName string, limit int) ([]ToolLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var matched []ToolLog
	for _, tl := range m.toolLogs {
		if tl.SessionID == sessionID {
			if toolName == "" || tl.ToolName == toolName {
				matched = append(matched, tl)
			}
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].CreatedAt < matched[j].CreatedAt
	})

	if limit > 0 && len(matched) > limit {
		matched = matched[len(matched)-limit:]
	}

	res := make([]ToolLog, len(matched))
	copy(res, matched)
	return res, nil
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
