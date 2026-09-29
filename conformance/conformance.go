package conformance

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/llm"
)

// Factory builds a fresh, empty agent.MemoryStore for ONE test. Called once per subtest —
// no state bleeds between them.
type Factory struct {
	Name string
	New  func(t *testing.T) agent.MemoryStore
}

func Run(t *testing.T, f Factory) {
	if f.New == nil {
		t.Fatal("conformance: Factory.New is required")
	}
	t.Run("conversation_ensure_session_idempotent", func(t *testing.T) { conversationEnsureSessionIdempotent(t, f) })
	t.Run("conversation_append_and_get_turns", func(t *testing.T) { conversationAppendAndGetTurns(t, f) })
	t.Run("conversation_get_turns_respects_limit", func(t *testing.T) { conversationGetTurnsRespectsLimit(t, f) })
	t.Run("conversation_delete_removes_only_specified", func(t *testing.T) { conversationDeleteRemovesOnlySpecified(t, f) })
	t.Run("conversation_session_isolation", func(t *testing.T) { conversationSessionIsolation(t, f) })
	t.Run("summary_save_and_get", func(t *testing.T) { summarySaveAndGet(t, f) })
	t.Run("summary_get_respects_limit", func(t *testing.T) { summaryGetRespectsLimit(t, f) })
	t.Run("summary_session_isolation", func(t *testing.T) { summarySessionIsolation(t, f) })
	t.Run("summary_round_trips_all_fields", func(t *testing.T) { summaryRoundTripsAllFields(t, f) })
	t.Run("knowledge_search_finds_exact_content", func(t *testing.T) { knowledgeSearchFindsExactContent(t, f) })
	t.Run("knowledge_global_visible_from_any_session", func(t *testing.T) { knowledgeGlobalVisibleFromAnySession(t, f) })
	t.Run("knowledge_session_scoped_not_visible_from_other_session", func(t *testing.T) { knowledgeSessionScopedNotVisibleFromOtherSession(t, f) })
	t.Run("knowledge_search_respects_limit", func(t *testing.T) { knowledgeSearchRespectsLimit(t, f) })
	t.Run("toollog_log_and_get", func(t *testing.T) { toolLogLogAndGet(t, f) })
	t.Run("toollog_filter_by_tool_name", func(t *testing.T) { toolLogFilterByToolName(t, f) })
	t.Run("toollog_session_isolation", func(t *testing.T) { toolLogSessionIsolation(t, f) })
}

func conversationEnsureSessionIdempotent(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	if err := mem.EnsureSession(ctx, "session-1"); err != nil {
		t.Fatalf("EnsureSession first call: %v", err)
	}
	if err := mem.EnsureSession(ctx, "session-1"); err != nil {
		t.Fatalf("EnsureSession second call: %v", err)
	}
}

func conversationAppendAndGetTurns(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	turn := agentcontext.Turn{
		ID:        "m1",
		Message:   llm.Message{Role: llm.RoleUser, Content: "hello"},
		CreatedAt: 1,
	}
	if err := mem.AppendTurn(ctx, "s1", turn); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}

	got, err := mem.GetTurns(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("GetTurns: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d turns, want 1", len(got))
	}
	if got[0].ID != "m1" || got[0].Message.Content != "hello" {
		t.Fatalf("unexpected turn content: %+v", got[0])
	}
}

func conversationGetTurnsRespectsLimit(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	for i := 1; i <= 5; i++ {
		turn := agentcontext.Turn{
			ID:        "m",
			Message:   llm.Message{Role: llm.RoleUser, Content: "msg"},
			CreatedAt: int64(i),
		}
		if err := mem.AppendTurn(ctx, "s1", turn); err != nil {
			t.Fatalf("AppendTurn %d: %v", i, err)
		}
	}

	got, err := mem.GetTurns(ctx, "s1", 3)
	if err != nil {
		t.Fatalf("GetTurns: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d turns, want 3", len(got))
	}
}

func conversationDeleteRemovesOnlySpecified(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	mem.AppendTurn(ctx, "s1", agentcontext.Turn{ID: "m1", Message: llm.Message{Content: "one"}})
	mem.AppendTurn(ctx, "s1", agentcontext.Turn{ID: "m2", Message: llm.Message{Content: "two"}})
	mem.AppendTurn(ctx, "s1", agentcontext.Turn{ID: "m3", Message: llm.Message{Content: "three"}})

	if err := mem.DeleteTurns(ctx, "s1", []string{"m2"}); err != nil {
		t.Fatalf("DeleteTurns: %v", err)
	}

	got, err := mem.GetTurns(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("GetTurns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d turns after delete, want 2", len(got))
	}
	for _, m := range got {
		if m.ID == "m2" {
			t.Fatalf("deleted turn m2 still present")
		}
	}
}

func conversationSessionIsolation(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	if err := mem.EnsureSession(ctx, "session-a"); err != nil {
		t.Fatalf("EnsureSession(a): %v", err)
	}
	if err := mem.EnsureSession(ctx, "session-b"); err != nil {
		t.Fatalf("EnsureSession(b): %v", err)
	}
	if err := mem.AppendTurn(ctx, "session-a", agentcontext.Turn{ID: "m1", Message: llm.Message{Role: llm.RoleUser, Content: "hello from a"}}); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}

	gotB, err := mem.GetTurns(ctx, "session-b", 10)
	if err != nil {
		t.Fatalf("GetTurns(b): %v", err)
	}
	if len(gotB) != 0 {
		t.Fatalf("session-b sees %d turns from session-a, want 0", len(gotB))
	}
}

func summarySaveAndGet(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	sum := agentcontext.Summary{
		ID:         "e1",
		Text:       "summary text",
		Tokens:     100,
		FromTurnID: "m1",
		ToTurnID:   "m5",
		CreatedAt:  1,
	}
	if err := mem.SaveSummary(ctx, "s1", sum); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}

	got, err := mem.GetSummaries(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d summaries, want 1", len(got))
	}
	if got[0].Text != "summary text" || got[0].Tokens != 100 {
		t.Fatalf("unexpected summary content: %+v", got[0])
	}
}

func summaryGetRespectsLimit(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	for i := 1; i <= 5; i++ {
		sum := agentcontext.Summary{
			ID:         "e",
			Text:       "sum",
			Tokens:     i * 10,
			FromTurnID: "m1",
			ToTurnID:   "m2",
			CreatedAt:  int64(i),
		}
		if err := mem.SaveSummary(ctx, "s1", sum); err != nil {
			t.Fatalf("SaveSummary %d: %v", i, err)
		}
	}

	got, err := mem.GetSummaries(ctx, "s1", 2)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d summaries, want 2", len(got))
	}
}

func summarySessionIsolation(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	mem.SaveSummary(ctx, "session-a", agentcontext.Summary{
		ID:         "e1",
		Text:       "summary a",
		Tokens:     50,
		FromTurnID: "m1",
		ToTurnID:   "m2",
		CreatedAt:  1,
	})

	gotB, err := mem.GetSummaries(ctx, "session-b", 10)
	if err != nil {
		t.Fatalf("GetSummaries(b): %v", err)
	}
	if len(gotB) != 0 {
		t.Fatalf("session-b sees %d summaries from session-a, want 0", len(gotB))
	}
}

func summaryRoundTripsAllFields(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	sum := agentcontext.Summary{
		ID:         "s-123",
		Text:       "detailed summary text",
		Tokens:     42,
		FromTurnID: "turn-1",
		ToTurnID:   "turn-10",
		CreatedAt:  1234567890,
	}

	if err := mem.SaveSummary(ctx, "s1", sum); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}

	got, err := mem.GetSummaries(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("GetSummaries: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d summaries, want 1", len(got))
	}

	s := got[0]
	if s.ID != sum.ID || s.Text != sum.Text || s.Tokens != sum.Tokens ||
		s.FromTurnID != sum.FromTurnID || s.ToTurnID != sum.ToTurnID || s.CreatedAt != sum.CreatedAt {
		t.Fatalf("summary roundtrip mismatch: got %+v, want %+v", s, sum)
	}
}

func knowledgeSearchFindsExactContent(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	if err := mem.SaveKnowledge(ctx, "s1", "golang programming language", "doc"); err != nil {
		t.Fatalf("SaveKnowledge: %v", err)
	}

	results, err := mem.SearchKnowledge(ctx, "programming", "s1", 10)
	if err != nil {
		t.Fatalf("SearchKnowledge: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d knowledge results, want 1", len(results))
	}
	if results[0].Content != "golang programming language" {
		t.Fatalf("unexpected knowledge content: %+v", results[0])
	}
}

func knowledgeGlobalVisibleFromAnySession(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	if err := mem.SaveKnowledge(ctx, "", "global secret facts", "doc"); err != nil {
		t.Fatalf("SaveKnowledge: %v", err)
	}

	results, err := mem.SearchKnowledge(ctx, "facts", "session-x", 10)
	if err != nil {
		t.Fatalf("SearchKnowledge: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("session-x could not see global knowledge, got %d results", len(results))
	}
}

func knowledgeSessionScopedNotVisibleFromOtherSession(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	if err := mem.SaveKnowledge(ctx, "session-a", "the secret project codename is condor", "agent"); err != nil {
		t.Fatalf("SaveKnowledge: %v", err)
	}

	results, err := mem.SearchKnowledge(ctx, "condor", "session-b", 10)
	if err != nil {
		t.Fatalf("SearchKnowledge: %v", err)
	}
	for _, r := range results {
		if r.Content == "the secret project codename is condor" {
			t.Fatalf("session-b's search found session-a's private knowledge")
		}
	}
}

func knowledgeSearchRespectsLimit(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	mem.SaveKnowledge(ctx, "s1", "common item 1", "src")
	mem.SaveKnowledge(ctx, "s1", "common item 2", "src")
	mem.SaveKnowledge(ctx, "s1", "common item 3", "src")

	results, err := mem.SearchKnowledge(ctx, "common", "s1", 2)
	if err != nil {
		t.Fatalf("SearchKnowledge: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d knowledge results, want 2", len(results))
	}
}

func toolLogLogAndGet(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	if err := mem.LogToolCall(ctx, "s1", "calculator", `{"a":1}`, "2", "", 10); err != nil {
		t.Fatalf("LogToolCall: %v", err)
	}

	logs, err := mem.GetToolLogs(ctx, "s1", "", 10)
	if err != nil {
		t.Fatalf("GetToolLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("got %d tool logs, want 1", len(logs))
	}
	if logs[0].ToolName != "calculator" || logs[0].OutputText != "2" {
		t.Fatalf("unexpected tool log content: %+v", logs[0])
	}
}

func toolLogFilterByToolName(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	mem.LogToolCall(ctx, "s1", "calc", `{}`, "1", "", 5)
	mem.LogToolCall(ctx, "s1", "weather", `{}`, "sunny", "", 15)

	logs, err := mem.GetToolLogs(ctx, "s1", "calc", 10)
	if err != nil {
		t.Fatalf("GetToolLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("got %d tool logs, want 1", len(logs))
	}
	if logs[0].ToolName != "calc" {
		t.Fatalf("got tool log for %s, want calc", logs[0].ToolName)
	}
}

func toolLogSessionIsolation(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	mem.LogToolCall(ctx, "session-a", "calc", `{}`, "1", "", 5)

	logsB, err := mem.GetToolLogs(ctx, "session-b", "", 10)
	if err != nil {
		t.Fatalf("GetToolLogs(b): %v", err)
	}
	if len(logsB) != 0 {
		t.Fatalf("session-b sees %d tool logs from session-a, want 0", len(logsB))
	}
}
