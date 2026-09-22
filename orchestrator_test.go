package agent

import (
	"testing"

	"webtyp.com/context"
	"webtyp.com/fmt"
)

func TestReAct_ToolCallThenAnswer(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	// Mock LLM
	mockLLM := &MockLLMClient{
		GenerateFunc: func(ctx *context.Context, req LLMRequest) (LLMResponse, error) {
			// Check if reflection
			if fmt.Contains(req.SystemPrompt, "critic that evaluates") {
				return LLMResponse{
					Text:       "SUFFICIENT",
					TokensUsed: 10,
					StopReason: "end_turn",
				}, nil
			}

			// Reasoning steps
			hasToolResult := false
			for _, m := range req.Messages {
				if m.Role == "tool" {
					hasToolResult = true
					break
				}
			}

			if !hasToolResult {
				return LLMResponse{
					Text:       "I will calculate 1+2.",
					StopReason: "tool_use",
					ToolCalls: []ToolCall{
						{
							ID:    "call_1",
							Name:  "calculator",
							Input: `{"a": 1, "b": 2}`,
						},
					},
					TokensUsed: 20,
				}, nil
			} else {
				return LLMResponse{
					Text:       "The answer is 3.",
					StopReason: "end_turn",
					TokensUsed: 15,
				}, nil
			}
		},
	}

	cfg := Config{
		Identity: IdentityConfig{Name: "Bot"},
		LLMs: LLMConfig{
			Primary: mockLLM,
		},
		Memory:     testMemory,
		IDGen:      testIDGen,
		MCPServers: []string{testServer.URL},
	}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	answer, err := agent.Run(ctx, sessionID, "What is 1+2?")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if answer != "The answer is 3." {
		t.Errorf("expected answer 'The answer is 3.', got '%s'", answer)
	}
}

func TestReAct_ReflectionApproved(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	mockLLM := &MockLLMClient{
		GenerateFunc: func(ctx *context.Context, req LLMRequest) (LLMResponse, error) {
			if fmt.Contains(req.SystemPrompt, "critic that evaluates") {
				return LLMResponse{Text: "SUFFICIENT", StopReason: "end_turn"}, nil
			}
			return LLMResponse{Text: "Answer", StopReason: "end_turn"}, nil
		},
	}

	cfg := Config{
		Identity: IdentityConfig{Name: "Bot"},
		LLMs:     LLMConfig{Primary: mockLLM},
		Memory:   testMemory,
		IDGen:    testIDGen,
	}

	agent, _ := New(cfg)
	ans, err := agent.Run(ctx, sessionID, "Hi")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if ans != "Answer" {
		t.Errorf("expected Answer, got %s", ans)
	}
}

func TestReAct_ReflectionRetry(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	attempts := 0
	mockLLM := &MockLLMClient{
		GenerateFunc: func(ctx *context.Context, req LLMRequest) (LLMResponse, error) {
			if fmt.Contains(req.SystemPrompt, "critic that evaluates") {
				attempts++
				if attempts == 1 {
					return LLMResponse{Text: "INSUFFICIENT. Missing detail.", StopReason: "end_turn"}, nil
				}
				return LLMResponse{Text: "SUFFICIENT", StopReason: "end_turn"}, nil
			}
			// If we see "Reflection feedback" in history, generate improved answer
			hasFeedback := false
			for _, m := range req.Messages {
				if fmt.Contains(m.Content, "Reflection feedback") {
					hasFeedback = true
					break
				}
			}
			if hasFeedback {
				return LLMResponse{Text: "Improved Answer", StopReason: "end_turn"}, nil
			}
			return LLMResponse{Text: "Initial Answer", StopReason: "end_turn"}, nil
		},
	}

	cfg := Config{
		Identity: IdentityConfig{Name: "Bot"},
		LLMs:     LLMConfig{Primary: mockLLM},
		Memory:   testMemory,
		IDGen:    testIDGen,
	}

	agent, _ := New(cfg)
	ans, err := agent.Run(ctx, sessionID, "Hi")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if ans != "Improved Answer" {
		t.Errorf("expected Improved Answer, got %s", ans)
	}
}

func TestReAct_ToolErrorSelfCorrect(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	mockLLM := &MockLLMClient{
		GenerateFunc: func(ctx *context.Context, req LLMRequest) (LLMResponse, error) {
			if fmt.Contains(req.SystemPrompt, "critic") {
				return LLMResponse{Text: "SUFFICIENT", StopReason: "end_turn"}, nil
			}

			// Check for tool error in history
			hasError := false
			for _, m := range req.Messages {
				if m.Role == "tool" && fmt.Contains(m.Content, "Error") {
					hasError = true
					break
				}
			}

			if !hasError {
				// Call non-existent tool
				return LLMResponse{
					Text:       "Trying tool",
					StopReason: "tool_use",
					ToolCalls:  []ToolCall{{Name: "unknown_tool", Input: "{}"}},
				}, nil
			}
			// Correct
			return LLMResponse{Text: "Corrected Answer", StopReason: "end_turn"}, nil
		},
	}

	cfg := Config{
		Identity: IdentityConfig{Name: "Bot"},
		LLMs:     LLMConfig{Primary: mockLLM},
		Memory:   testMemory,
		IDGen:    testIDGen,
	}

	agent, _ := New(cfg)
	ans, err := agent.Run(ctx, sessionID, "Hi")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if ans != "Corrected Answer" {
		t.Errorf("expected Corrected Answer, got %s", ans)
	}
}

func TestReAct_MaxIterationsGuard(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	mockLLM := &MockLLMClient{
		GenerateFunc: func(ctx *context.Context, req LLMRequest) (LLMResponse, error) {
			// Always call tool
			return LLMResponse{
				Text:       "Looping",
				StopReason: "tool_use",
				ToolCalls:  []ToolCall{{Name: "calculator", Input: `{"a": 1, "b": 1}`}},
			}, nil
		},
	}

	cfg := Config{
		Identity:      IdentityConfig{Name: "Bot"},
		LLMs:          LLMConfig{Primary: mockLLM},
		Memory:        testMemory,
		IDGen:         testIDGen,
		MCPServers:    []string{testServer.URL},
		MaxIterations: 3,
	}

	agent, _ := New(cfg)
	_, err := agent.Run(ctx, sessionID, "Hi")
	if err == nil {
		t.Fatalf("expected error due to max iterations, got nil")
	}
	if !fmt.Contains(err.Error(), "max iterations reached") {
		t.Errorf("expected 'max iterations reached', got %v", err)
	}
}

func TestOrchestrator_RealMCP(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	toolCalled := false
	mockLLM := &MockLLMClient{
		GenerateFunc: func(ctx *context.Context, req LLMRequest) (LLMResponse, error) {
			if fmt.Contains(req.SystemPrompt, "critic that evaluates") {
				return LLMResponse{Text: "SUFFICIENT", StopReason: "end_turn"}, nil
			}
			// Verify tools were discovered from the real MCP server.
			if len(req.Tools) == 0 {
				t.Error("expected tools to be discovered from MCP server, got none")
				return LLMResponse{Text: "no tools", StopReason: "end_turn"}, nil
			}
			if !toolCalled {
				toolCalled = true
				return LLMResponse{
					StopReason: "tool_use",
					ToolCalls:  []ToolCall{{ID: "mcp1", Name: req.Tools[0].Name, Input: `{"a":3,"b":4}`}},
					TokensUsed: 10,
				}, nil
			}
			return LLMResponse{Text: "Result via MCP", StopReason: "end_turn", TokensUsed: 10}, nil
		},
	}

	cfg := Config{
		Identity:   IdentityConfig{Name: "Bot"},
		LLMs:       LLMConfig{Primary: mockLLM},
		Memory:     testMemory,
		IDGen:      testIDGen,
		MCPServers: []string{testServer.URL},
	}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	ans, err := agent.Run(ctx, sessionID, "compute")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if ans != "Result via MCP" {
		t.Errorf("unexpected answer: %s", ans)
	}
	if !toolCalled {
		t.Error("expected MCP tool to be called, but it was not")
	}
}
