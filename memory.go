package agent

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"
	"webtyp.com/fmt"
	_ "modernc.org/sqlite"
)

// SQLiteMemoryStore implements MemoryStore using SQLite.
type SQLiteMemoryStore struct {
	db *sql.DB
}

// NewSQLiteMemory creates a new SQLiteMemoryStore.
func NewSQLiteMemory(dsn string) (MemoryStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errf("failed to open sqlite db: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errf("failed to apply schema: %w", err)
	}

	return &SQLiteMemoryStore{db: db}, nil
}

func (s *SQLiteMemoryStore) EnsureSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO sessions (id) VALUES (?)", sessionID)
	if err != nil {
		return fmt.Errf("failed to ensure session: %w", err)
	}
	return nil
}

func (s *SQLiteMemoryStore) AppendMessage(ctx context.Context, sessionID string, msg Message) error {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	// We allow caller to provide ID, but we must ensure it's unique if we use it as PK.
	// The table has ID as PK.

	var toolCallsJSON string
	if len(msg.ToolCalls) > 0 {
		b, err := json.Marshal(msg.ToolCalls)
		if err != nil {
			return fmt.Errf("failed to marshal tool calls: %w", err)
		}
		toolCallsJSON = string(b)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, tool_name, tool_call_id, tool_calls, token_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, msg.ID, sessionID, msg.Role, msg.Content, msg.ToolName, msg.ToolCallID, toolCallsJSON, msg.TokenCount, msg.CreatedAt)
	if err != nil {
		return fmt.Errf("failed to append message: %w", err)
	}
	return nil
}

func (s *SQLiteMemoryStore) GetMessages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, role, content, tool_name, tool_call_id, tool_calls, token_count, created_at
		FROM messages
		WHERE session_id = ?
		ORDER BY created_at ASC
		LIMIT ?
	`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errf("failed to get messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		var toolName, toolCallID, toolCallsJSON sql.NullString
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &toolName, &toolCallID, &toolCallsJSON, &m.TokenCount, &m.CreatedAt); err != nil {
			return nil, fmt.Errf("failed to scan message: %w", err)
		}
		if toolName.Valid {
			m.ToolName = toolName.String
		}
		if toolCallID.Valid {
			m.ToolCallID = toolCallID.String
		}
		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			if err := json.Unmarshal([]byte(toolCallsJSON.String), &m.ToolCalls); err != nil {
				return nil, fmt.Errf("failed to unmarshal tool calls: %w", err)
			}
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errf("rows error: %w", err)
	}
	return msgs, nil
}

func (s *SQLiteMemoryStore) DeleteMessages(ctx context.Context, sessionID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	query := "DELETE FROM messages WHERE session_id = ? AND id IN (?" + fmt.Convert(",?").Repeat(len(ids)-1).String() + ")"
	args := make([]any, len(ids)+1)
	args[0] = sessionID
	for i, id := range ids {
		args[i+1] = id
	}
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errf("failed to delete messages: %w", err)
	}
	return nil
}

func (s *SQLiteMemoryStore) SaveEpisode(ctx context.Context, sessionID, summary string, tokenCount int, fromID, toID string) error {
	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO episodes (id, session_id, summary, token_count, from_msg_id, to_msg_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, sessionID, summary, tokenCount, fromID, toID)
	if err != nil {
		return fmt.Errf("failed to save episode: %w", err)
	}
	return nil
}

func (s *SQLiteMemoryStore) GetEpisodes(ctx context.Context, sessionID string, limit int) ([]Episode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, summary, token_count, from_msg_id, to_msg_id, created_at
		FROM episodes
		WHERE session_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errf("failed to get episodes: %w", err)
	}
	defer rows.Close()

	var eps []Episode
	for rows.Next() {
		var e Episode
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Summary, &e.TokenCount, &e.FromMsgID, &e.ToMsgID, &e.CreatedAt); err != nil {
			return nil, fmt.Errf("failed to scan episode: %w", err)
		}
		eps = append(eps, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errf("rows error: %w", err)
	}
	return eps, nil
}

func (s *SQLiteMemoryStore) SaveKnowledge(ctx context.Context, sessionID, content, source string) error {
	id := uuid.New().String()
	var sessionIDParam any
	if sessionID != "" {
		sessionIDParam = sessionID
	} else {
		sessionIDParam = nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO knowledge (id, session_id, content, source)
		VALUES (?, ?, ?, ?)
	`, id, sessionIDParam, content, source)
	if err != nil {
		return fmt.Errf("failed to save knowledge: %w", err)
	}
	return nil
}

func (s *SQLiteMemoryStore) SearchKnowledge(ctx context.Context, query, sessionID string, limit int) ([]Knowledge, error) {
	// Using FTS5 match
	// We need to handle session scoping: SessionID == "" (NULL) means global.
	// If sessionID is provided, we search both global and private knowledge.
	// Wait, FTS5 table `knowledge_fts` is a virtual table. It doesn't store session_id directly.
	// But we can join with `knowledge` table.
	// "content='knowledge', content_rowid='rowid'" in FTS5 definition means it uses the `knowledge` table as content source.

	// The query should be:
	// SELECT k.* FROM knowledge_fts f
	// JOIN knowledge k ON f.rowid = k.rowid
	// WHERE k.session_id IS NULL OR k.session_id = ?
	// AND knowledge_fts MATCH ?
	// ORDER BY rank
	// LIMIT ?

	rows, err := s.db.QueryContext(ctx, `
		SELECT k.id, k.session_id, k.content, k.source, k.created_at
		FROM knowledge_fts f
		JOIN knowledge k ON f.rowid = k.rowid
		WHERE (k.session_id IS NULL OR k.session_id = ?)
		AND knowledge_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, sessionID, query, limit)
	if err != nil {
		return nil, fmt.Errf("failed to search knowledge: %w", err)
	}
	defer rows.Close()

	var kns []Knowledge
	for rows.Next() {
		var k Knowledge
		var sid sql.NullString
		if err := rows.Scan(&k.ID, &sid, &k.Content, &k.Source, &k.CreatedAt); err != nil {
			return nil, fmt.Errf("failed to scan knowledge: %w", err)
		}
		if sid.Valid {
			k.SessionID = sid.String
		}
		kns = append(kns, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errf("rows error: %w", err)
	}
	return kns, nil
}

func (s *SQLiteMemoryStore) LogToolCall(ctx context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error {
	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tool_logs (id, session_id, tool_name, input_json, output_text, error_text, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, sessionID, toolName, inputJSON, outputText, errText, durationMS)
	if err != nil {
		return fmt.Errf("failed to log tool call: %w", err)
	}
	return nil
}

func (s *SQLiteMemoryStore) GetToolLogs(ctx context.Context, sessionID, toolName string, limit int) ([]ToolLog, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, tool_name, input_json, output_text, error_text, duration_ms, created_at
		FROM tool_logs
		WHERE session_id = ? AND tool_name = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, sessionID, toolName, limit)
	if err != nil {
		return nil, fmt.Errf("failed to get tool logs: %w", err)
	}
	defer rows.Close()

	var logs []ToolLog
	for rows.Next() {
		var l ToolLog
		var outputText, errText sql.NullString
		if err := rows.Scan(&l.ID, &l.SessionID, &l.ToolName, &l.InputJSON, &outputText, &errText, &l.DurationMS, &l.CreatedAt); err != nil {
			return nil, fmt.Errf("failed to scan tool log: %w", err)
		}
		if outputText.Valid {
			l.OutputText = outputText.String
		}
		if errText.Valid {
			l.ErrText = errText.String
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errf("rows error: %w", err)
	}
	return logs, nil
}
