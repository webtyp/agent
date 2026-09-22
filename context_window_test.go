package agent

import (
	"testing"

	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/time"
)

func TestContextWindow_Summarize(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	mockLLM := &MockLLMClient{
		GenerateFunc: func(ctx *context.Context, req LLMRequest) (LLMResponse, error) {
			if fmt.Contains(req.SystemPrompt, "summarizes conversations") {
				return LLMResponse{
					Text:       "Summary of conversation",
					TokensUsed: 10,
					StopReason: "end_turn",
				}, nil
			}
			return LLMResponse{}, nil
		},
	}

	agent := &Agent{
		cfg: Config{
			Identity: IdentityConfig{Name: "TestBot"},
			LLMs: LLMConfig{
				Primary:    mockLLM,
				Summarizer: mockLLM,
			},
			Memory: testMemory,
			IDGen:  testIDGen,
			ContextWindow: ContextWindowConfig{
				MaxTokens:     100,
				SummarizeAt:   0.5,
				MaxRecentMsgs: 10,
				MaxEpisodes:   5,
			},
		},
		mem:   testMemory,
		llms:  LLMConfig{Primary: mockLLM, Summarizer: mockLLM},
		idGen: testIDGen,
	}

	for i := 0; i < 6; i++ {
		msg := Message{
			ID:         testIDGen.NewID(),
			SessionID:  sessionID,
			Role:       "user",
			Content:    fmt.Sprintf("Message %d", i),
			TokenCount: 10,
			CreatedAt:  time.Now()/1e9 + int64(i),
		}
		if err := testMemory.AppendMessage(ctx, sessionID, msg); err != nil {
			t.Fatalf("AppendMessage failed: %v", err)
		}
	}

	req, err := agent.prepareContext(ctx, sessionID)
	if err != nil {
		t.Fatalf("prepareContext failed: %v", err)
	}

	msgs, err := testMemory.GetMessages(ctx, sessionID, 100)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages remaining in DB, got %d", len(msgs))
	}

	eps, err := testMemory.GetEpisodes(ctx, sessionID, 10)
	if err != nil {
		t.Fatalf("GetEpisodes failed: %v", err)
	}
	if len(eps) != 1 {
		t.Errorf("expected 1 episode, got %d", len(eps))
	}
	if eps[0].Summary != "Summary of conversation" {
		t.Errorf("expected summary 'Summary of conversation', got '%s'", eps[0].Summary)
	}

	if len(req.Messages) != 4 {
		t.Errorf("expected 4 messages in request (1 episode summary + 3 messages), got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("expected first message to be system (episode summary), got %s", req.Messages[0].Role)
	}

	if !fmt.Contains(req.Messages[0].Content, "Summary of conversation") {
		t.Errorf("expected summary content in system message, got '%s'", req.Messages[0].Content)
	}
}
