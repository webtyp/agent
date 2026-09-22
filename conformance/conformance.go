package conformance

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/context"
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
	t.Run("conversation_append_and_get_messages", func(t *testing.T) { conversationAppendAndGetMessages(t, f) })
	t.Run("conversation_get_messages_respects_limit", func(t *testing.T) { conversationGetMessagesRespectsLimit(t, f) })
	t.Run("conversation_delete_removes_only_specified", func(t *testing.T) { conversationDeleteRemovesOnlySpecified(t, f) })
	t.Run("conversation_session_isolation", func(t *testing.T) { conversationSessionIsolation(t, f) })
	t.Run("episode_save_and_get", func(t *testing.T) { episodeSaveAndGet(t, f) })
	t.Run("episode_get_respects_limit", func(t *testing.T) { episodeGetRespectsLimit(t, f) })
	t.Run("episode_session_isolation", func(t *testing.T) { episodeSessionIsolation(t, f) })
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

func conversationAppendAndGetMessages(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	msg := agent.Message{ID: "m1", SessionID: "s1", Role: "user", Content: "hello"}
	if err := mem.AppendMessage(ctx, "s1", msg); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	got, err := mem.GetMessages(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
	if got[0].ID != "m1" || got[0].Content != "hello" {
		t.Fatalf("unexpected message content: %+v", got[0])
	}
}

func conversationGetMessagesRespectsLimit(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	for i := 1; i <= 5; i++ {
		msg := agent.Message{ID: "m", SessionID: "s1", Role: "user", Content: "msg", CreatedAt: int64(i)}
		if err := mem.AppendMessage(ctx, "s1", msg); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}

	got, err := mem.GetMessages(ctx, "s1", 3)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d messages, want 3", len(got))
	}
}

func conversationDeleteRemovesOnlySpecified(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	mem.AppendMessage(ctx, "s1", agent.Message{ID: "m1", SessionID: "s1", Content: "one"})
	mem.AppendMessage(ctx, "s1", agent.Message{ID: "m2", SessionID: "s1", Content: "two"})
	mem.AppendMessage(ctx, "s1", agent.Message{ID: "m3", SessionID: "s1", Content: "three"})

	if err := mem.DeleteMessages(ctx, "s1", []string{"m2"}); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	got, err := mem.GetMessages(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d messages after delete, want 2", len(got))
	}
	for _, m := range got {
		if m.ID == "m2" {
			t.Fatalf("deleted message m2 still present")
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
	if err := mem.AppendMessage(ctx, "session-a", agent.Message{ID: "m1", SessionID: "session-a", Role: "user", Content: "hello from a"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	gotB, err := mem.GetMessages(ctx, "session-b", 10)
	if err != nil {
		t.Fatalf("GetMessages(b): %v", err)
	}
	if len(gotB) != 0 {
		t.Fatalf("session-b sees %d messages from session-a, want 0", len(gotB))
	}
}

func episodeSaveAndGet(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	if err := mem.SaveEpisode(ctx, "s1", "summary text", 100, "m1", "m5"); err != nil {
		t.Fatalf("SaveEpisode: %v", err)
	}

	got, err := mem.GetEpisodes(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("GetEpisodes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d episodes, want 1", len(got))
	}
	if got[0].Summary != "summary text" || got[0].TokenCount != 100 {
		t.Fatalf("unexpected episode content: %+v", got[0])
	}
}

func episodeGetRespectsLimit(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	for i := 1; i <= 5; i++ {
		if err := mem.SaveEpisode(ctx, "s1", "sum", i*10, "m1", "m2"); err != nil {
			t.Fatalf("SaveEpisode %d: %v", i, err)
		}
	}

	got, err := mem.GetEpisodes(ctx, "s1", 2)
	if err != nil {
		t.Fatalf("GetEpisodes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d episodes, want 2", len(got))
	}
}

func episodeSessionIsolation(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	mem.SaveEpisode(ctx, "session-a", "summary a", 50, "m1", "m2")

	gotB, err := mem.GetEpisodes(ctx, "session-b", 10)
	if err != nil {
		t.Fatalf("GetEpisodes(b): %v", err)
	}
	if len(gotB) != 0 {
		t.Fatalf("session-b sees %d episodes from session-a, want 0", len(gotB))
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
