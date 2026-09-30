package tests

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/llm"
)

type errorToolIndex struct{}

func (e errorToolIndex) IndexTools(ctx *context.Context, tools []llm.ToolDef) error {
	return nil
}

func (e errorToolIndex) SearchTools(ctx *context.Context, query string, limit int) ([]string, error) {
	return nil, fmt.Errf("index search failed")
}

func TestPreselect_FewTools(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	t1 := dummyTool{name: "tool_a", desc: "alpha tool"}
	t2 := dummyTool{name: "tool_b", desc: "beta tool"}

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{StopReason: llm.StopEndTurn, Text: "Done"},
		},
	}

	mem := agent.NewMemMemory()
	mem.EnsureSession(ctx, sessionID)

	a, err := agent.New(agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: scriptLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     mem,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		LocalTools: []agent.Tool{t1, t2},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "Hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(scriptLLM.requests) == 0 {
		t.Fatalf("expected at least 1 request recorded")
	}

	req := scriptLLM.requests[0]
	if len(req.Tools) != 3 {
		t.Fatalf("expected 3 tools offered (search_tools + 2 local), got %d: %v", len(req.Tools), req.Tools)
	}

	names := make(map[string]bool)
	for _, tool := range req.Tools {
		names[tool.Name] = true
	}

	if !names["search_tools"] || !names["tool_a"] || !names["tool_b"] {
		t.Errorf("expected search_tools, tool_a, tool_b, got %v", names)
	}
}

func TestPreselect_ManyToolsKeywordMatch(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	matchedTool := dummyTool{name: "business_hours", desc: "Opening hours of the clinic for each day of the week"}
	u1 := dummyTool{name: "patient_records", desc: "Patient medical history records"}
	u2 := dummyTool{name: "lab_results", desc: "Laboratory test results"}
	u3 := dummyTool{name: "pharmacy_stock", desc: "Medicine and pharmacy stock level"}
	u4 := dummyTool{name: "billing_invoices", desc: "Billing invoices and payments"}
	u5 := dummyTool{name: "staff_schedule", desc: "Staff schedule and shifts"}

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{StopReason: llm.StopEndTurn, Text: "Done"},
		},
	}

	mem := agent.NewMemMemory()
	mem.EnsureSession(ctx, sessionID)

	a, err := agent.New(agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: scriptLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     mem,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		LocalTools: []agent.Tool{matchedTool, u1, u2, u3, u4, u5},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "What are the opening hours today?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(scriptLLM.requests) == 0 {
		t.Fatalf("expected at least 1 request recorded")
	}

	req := scriptLLM.requests[0]
	// Should offer search_tools plus at most 3 tools (PreselectTools default = 3)
	if len(req.Tools) > 4 {
		t.Errorf("expected at most 4 tools offered (search_tools + 3 preselected), got %d: %v", len(req.Tools), req.Tools)
	}

	foundMatched := false
	for _, tool := range req.Tools {
		if tool.Name == "business_hours" {
			foundMatched = true
		}
		if tool.Name == "billing_invoices" || tool.Name == "pharmacy_stock" || tool.Name == "lab_results" || tool.Name == "staff_schedule" || tool.Name == "patient_records" {
			t.Errorf("unrelated tool %s should not be offered", tool.Name)
		}
	}

	if !foundMatched {
		t.Errorf("expected business_hours to be preselected, offered tools: %v", req.Tools)
	}
}

func TestPreselect_DirectCallWorks(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	matchedTool := dummyTool{name: "business_hours", desc: "Opening hours of the clinic for each day of the week"}
	u1 := dummyTool{name: "patient_records", desc: "Patient medical history records"}
	u2 := dummyTool{name: "lab_results", desc: "Laboratory test results"}
	u3 := dummyTool{name: "pharmacy_stock", desc: "Medicine and pharmacy stock level"}
	u4 := dummyTool{name: "billing_invoices", desc: "Billing invoices and payments"}
	u5 := dummyTool{name: "staff_schedule", desc: "Staff schedule and shifts"}

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{
				StopReason: llm.StopToolUse,
				ToolCalls:  []llm.ToolCall{{ID: "c1", Name: "business_hours", Input: "{}"}},
			},
			{
				StopReason: llm.StopEndTurn,
				Text:       "Open 8 to 20",
			},
		},
	}

	mem := agent.NewMemMemory()
	mem.EnsureSession(ctx, sessionID)

	a, err := agent.New(agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: scriptLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     mem,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		LocalTools: []agent.Tool{matchedTool, u1, u2, u3, u4, u5},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reply, err := a.Run(ctx, sessionID, "What are the opening hours today?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if reply.Text != "Open 8 to 20" {
		t.Errorf("expected 'Open 8 to 20', got %q", reply.Text)
	}

	// Verify tool result was passed without refusal error
	if len(scriptLLM.requests) < 2 {
		t.Fatalf("expected 2 requests")
	}

	req2 := scriptLLM.requests[1]
	for _, msg := range req2.Messages {
		if msg.Role == llm.RoleTool {
			if fmt.Contains(msg.Content, "not available") {
				t.Errorf("tool call was refused: %s", msg.Content)
			}
		}
	}
}

func TestPreselect_NegativePreselectToolsError(t *testing.T) {
	mockLLM := &recordingScriptedLLM{}
	cfg := agent.Config{
		LLMs:           agent.LLMConfig{Primary: mockLLM},
		Tokens:         quarterCounter{},
		Budget:         agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:         testMemory,
		IDGen:          testIDGen,
		ToolIndex:      agent.NewMemToolIndex(),
		PreselectTools: -1,
	}

	_, err := agent.New(cfg)
	if err == nil || !fmt.Contains(err.Error(), "agent: PreselectTools must not be negative") {
		t.Errorf("expected errPreselectNegative, got %v", err)
	}
}

func TestPreselect_IndexError(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	t1 := dummyTool{name: "tool_1", desc: "one"}
	t2 := dummyTool{name: "tool_2", desc: "two"}
	t3 := dummyTool{name: "tool_3", desc: "three"}
	t4 := dummyTool{name: "tool_4", desc: "four"}
	t5 := dummyTool{name: "tool_5", desc: "five"}

	scriptLLM := &recordingScriptedLLM{}
	mem := agent.NewMemMemory()
	mem.EnsureSession(ctx, sessionID)

	a, err := agent.New(agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: scriptLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     mem,
		IDGen:      testIDGen,
		ToolIndex:  errorToolIndex{},
		LocalTools: []agent.Tool{t1, t2, t3, t4, t5},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "query")
	if err == nil || !fmt.Contains(err.Error(), "agent: preselect tools:") {
		t.Errorf("expected error containing 'agent: preselect tools:', got %v", err)
	}
}
