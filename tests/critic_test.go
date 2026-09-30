package tests

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/llm"
)

type mockDecider struct {
	decideFunc func(ctx *context.Context, q llm.Question) (llm.Decision, error)
	questions  []llm.Question
}

func (m *mockDecider) Decide(ctx *context.Context, q llm.Question) (llm.Decision, error) {
	m.questions = append(m.questions, q)
	if m.decideFunc != nil {
		return m.decideFunc(ctx, q)
	}
	return llm.Decision{Choice: 0, Confidence: 1.0}, nil
}

func TestCritic_NilCritic(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{Text: "Draft answer", StopReason: llm.StopEndTurn},
		},
	}
	mem := agent.NewMemMemory()
	mem.EnsureSession(ctx, sessionID)

	a, err := agent.New(agent.Config{
		Identity:  agentcontext.Identity{Name: "Bot"},
		LLMs:      agent.LLMConfig{Primary: scriptLLM},
		Tokens:    quarterCounter{},
		Budget:    agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:    mem,
		IDGen:     testIDGen,
		ToolIndex: agent.NewMemToolIndex(),
		Critic:    nil,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reply, err := a.Run(ctx, sessionID, "Hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if reply.Text != "Draft answer" {
		t.Errorf("expected 'Draft answer', got %q", reply.Text)
	}
	if len(scriptLLM.requests) != 1 {
		t.Errorf("expected 1 generation request, got %d", len(scriptLLM.requests))
	}
}

func TestCritic_AcceptingCritic(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{Text: "Accurate answer", StopReason: llm.StopEndTurn},
		},
	}
	decider := &mockDecider{
		decideFunc: func(ctx *context.Context, q llm.Question) (llm.Decision, error) {
			return llm.Decision{Choice: 0, Confidence: 0.95}, nil
		},
	}

	mem := agent.NewMemMemory()
	mem.EnsureSession(ctx, sessionID)

	a, err := agent.New(agent.Config{
		Identity:  agentcontext.Identity{Name: "Bot"},
		LLMs:      agent.LLMConfig{Primary: scriptLLM},
		Tokens:    quarterCounter{},
		Budget:    agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:    mem,
		IDGen:     testIDGen,
		ToolIndex: agent.NewMemToolIndex(),
		Critic:    decider,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reply, err := a.Run(ctx, sessionID, "What hours?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if reply.Text != "Accurate answer" {
		t.Errorf("expected 'Accurate answer', got %q", reply.Text)
	}
	if len(decider.questions) != 1 {
		t.Fatalf("expected 1 question to critic, got %d", len(decider.questions))
	}

	q := decider.questions[0]
	expectedContext := "User asked: What hours?\nAssistant answered: Accurate answer"
	if q.Context != expectedContext {
		t.Errorf("expected context %q, got %q", expectedContext, q.Context)
	}
}

func TestCritic_RejectingCriticConfidenceHigh(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{Text: "Hallucinated draft", StopReason: llm.StopEndTurn},
			{Text: "Fixed answer", StopReason: llm.StopEndTurn},
		},
	}
	deciderCalls := 0
	decider := &mockDecider{
		decideFunc: func(ctx *context.Context, q llm.Question) (llm.Decision, error) {
			deciderCalls++
			return llm.Decision{Choice: 1, Confidence: 0.90}, nil
		},
	}

	mem := agent.NewMemMemory()
	mem.EnsureSession(ctx, sessionID)

	a, err := agent.New(agent.Config{
		Identity:  agentcontext.Identity{Name: "Bot"},
		LLMs:      agent.LLMConfig{Primary: scriptLLM},
		Tokens:    quarterCounter{},
		Budget:    agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:    mem,
		IDGen:     testIDGen,
		ToolIndex: agent.NewMemToolIndex(),
		Critic:    decider,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reply, err := a.Run(ctx, sessionID, "Tell me facts")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if reply.Text != "Fixed answer" {
		t.Errorf("expected 'Fixed answer', got %q", reply.Text)
	}

	if deciderCalls != 1 {
		t.Errorf("expected critic to be called exactly once (no second check after retry), got %d", deciderCalls)
	}

	if len(scriptLLM.requests) != 2 {
		t.Fatalf("expected 2 requests to LLM, got %d", len(scriptLLM.requests))
	}

	req2 := scriptLLM.requests[1]
	lastMsg := req2.Messages[len(req2.Messages)-1]
	if lastMsg.Role != llm.RoleSystem || !fmt.Contains(lastMsg.Content, "Your previous draft stated things the tool results do not support") {
		t.Errorf("expected second request to end with criticRetryNote system message, got %v", lastMsg)
	}

	turns, err := mem.GetTurns(ctx, sessionID, 100)
	if err != nil {
		t.Fatalf("GetTurns: %v", err)
	}
	for _, tr := range turns {
		if fmt.Contains(tr.Message.Content, "Hallucinated draft") {
			t.Errorf("found rejected draft in memory: %v", tr)
		}
		if fmt.Contains(tr.Message.Content, "Your previous draft stated things") {
			t.Errorf("found criticRetryNote in memory: %v", tr)
		}
	}
}

func TestCritic_RejectingCriticConfidenceLow(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{Text: "Plausible draft", StopReason: llm.StopEndTurn},
		},
	}

	decider := &mockDecider{
		decideFunc: func(ctx *context.Context, q llm.Question) (llm.Decision, error) {
			return llm.Decision{Choice: 1, Confidence: 0.79}, nil
		},
	}

	mem := agent.NewMemMemory()
	mem.EnsureSession(ctx, sessionID)

	a, err := agent.New(agent.Config{
		Identity:  agentcontext.Identity{Name: "Bot"},
		LLMs:      agent.LLMConfig{Primary: scriptLLM},
		Tokens:    quarterCounter{},
		Budget:    agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:    mem,
		IDGen:     testIDGen,
		ToolIndex: agent.NewMemToolIndex(),
		Critic:    decider,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reply, err := a.Run(ctx, sessionID, "Query")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if reply.Text != "Plausible draft" {
		t.Errorf("expected first answer returned when critic confidence < 0.8, got %q", reply.Text)
	}
}

func TestCritic_ErrorReturnsWrappedError(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{Text: "Some draft", StopReason: llm.StopEndTurn},
		},
	}

	decider := &mockDecider{
		decideFunc: func(ctx *context.Context, q llm.Question) (llm.Decision, error) {
			return llm.Decision{}, fmt.Errf("network failure")
		},
	}

	mem := agent.NewMemMemory()
	mem.EnsureSession(ctx, sessionID)

	a, err := agent.New(agent.Config{
		Identity:  agentcontext.Identity{Name: "Bot"},
		LLMs:      agent.LLMConfig{Primary: scriptLLM},
		Tokens:    quarterCounter{},
		Budget:    agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:    mem,
		IDGen:     testIDGen,
		ToolIndex: agent.NewMemToolIndex(),
		Critic:    decider,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "Query")
	if err == nil || !fmt.Contains(err.Error(), "agent: critic:") {
		t.Errorf("expected error containing 'agent: critic:', got %v", err)
	}
}
