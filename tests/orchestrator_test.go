package tests

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/llm"
	"webtyp.com/model"
)

type mockLLMClient struct {
	generateFunc func(ctx *context.Context, req llm.Request) (llm.Response, error)
}

func (m *mockLLMClient) Generate(ctx *context.Context, req llm.Request) (llm.Response, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, req)
	}
	return llm.Response{StopReason: llm.StopEndTurn, Text: "Default response"}, nil
}

type dummyTool struct {
	name string
	desc string
}

func (d dummyTool) Name() string        { return d.name }
func (d dummyTool) Description() string { return d.desc }
func (d dummyTool) InputSchema() string { return `{"type":"object"}` }
func (d dummyTool) Action() model.Action { return model.Read }
func (d dummyTool) Execute(ctx *context.Context, args string) (string, error) {
	return "tool result for " + d.name, nil
}

func TestNew_RequiresToolIndex(t *testing.T) {
	mockLLM := &mockLLMClient{}
	cfg := agent.Config{
		LLMs:   agent.LLMConfig{Primary: mockLLM},
		Tokens: quarterCounter{},
		Budget: agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory: testMemory,
		IDGen:  testIDGen,
	}

	_, err := agent.New(cfg)
	if err == nil || !fmt.Contains(err.Error(), "ToolIndex is required") {
		t.Errorf("expected ToolIndex error, got %v", err)
	}
}

func TestNew_ReservesSearchToolsName(t *testing.T) {
	mockLLM := &mockLLMClient{}
	cfg := agent.Config{
		LLMs:       agent.LLMConfig{Primary: mockLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     testMemory,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		LocalTools: []agent.Tool{dummyTool{name: "search_tools", desc: "reserved"}},
	}

	_, err := agent.New(cfg)
	if err == nil || !fmt.Contains(err.Error(), "reserved for tool search") {
		t.Errorf("expected search_tools error, got %v", err)
	}
}

func TestRun_OffersOnlySearchToolsFirst(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	var firstReq llm.Request
	mockLLM := &mockLLMClient{
		generateFunc: func(ctx *context.Context, req llm.Request) (llm.Response, error) {
			if len(firstReq.Tools) == 0 {
				firstReq = req
			}
			return llm.Response{Text: "Hello", StopReason: llm.StopEndTurn}, nil
		},
	}

	cfg := agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: mockLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     testMemory,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		LocalTools: []agent.Tool{
			dummyTool{name: "clinic_hours", desc: "opening hours"},
			dummyTool{name: "patient_records", desc: "patient records"},
			dummyTool{name: "lab_results", desc: "lab test results"},
			dummyTool{name: "pharmacy_stock", desc: "medicine stock"},
			dummyTool{name: "billing_invoices", desc: "billing invoices"},
		},
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "Hi")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(firstReq.Tools) != 1 || firstReq.Tools[0].Name != "search_tools" {
		t.Errorf("expected first request tools to be only [search_tools], got %v", firstReq.Tools)
	}
}

func TestRun_DiscoveredToolBecomesCallable(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	step := 0
	var req2Tools []llm.ToolDef

	tool := dummyTool{
		name: "clinic_hours",
		desc: "opening hours of the clinic",
	}

	mockLLM := &mockLLMClient{
		generateFunc: func(ctx *context.Context, req llm.Request) (llm.Response, error) {
			step++
			if step == 1 {
				return llm.Response{
					StopReason: llm.StopToolUse,
					ToolCalls: []llm.ToolCall{
						{ID: "c1", Name: "search_tools", Input: `{"query":"clinic hours"}`},
					},
				}, nil
			}
			if step == 2 {
				req2Tools = req.Tools
				return llm.Response{
					StopReason: llm.StopToolUse,
					ToolCalls: []llm.ToolCall{
						{ID: "c2", Name: "clinic_hours", Input: `{}`},
					},
				}, nil
			}

			return llm.Response{Text: "Hours are 8 to 20", StopReason: llm.StopEndTurn}, nil
		},
	}

	cfg := agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: mockLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     testMemory,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		LocalTools: []agent.Tool{tool},
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	reply, err := a.Run(ctx, sessionID, "When do you open?")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	found := false
	for _, tDef := range req2Tools {
		if tDef.Name == "clinic_hours" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected clinic_hours in step 2 tools, got %v", req2Tools)
	}

	if reply.Text != "Hours are 8 to 20" {
		t.Errorf("unexpected answer: %s", reply.Text)
	}
}

func TestRun_UndiscoveredToolIsRefused(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	step := 0
	var toolMsgContent string

	mockLLM := &mockLLMClient{
		generateFunc: func(ctx *context.Context, req llm.Request) (llm.Response, error) {
			step++
			if step == 1 {
				return llm.Response{
					StopReason: llm.StopToolUse,
					ToolCalls: []llm.ToolCall{
						{ID: "c1", Name: "clinic_hours", Input: `{}`},
					},
				}, nil
			}

			for _, m := range req.Messages {
				if m.Role == llm.RoleTool {
					toolMsgContent = m.Content
				}
			}

			return llm.Response{Text: "Handling refusal", StopReason: llm.StopEndTurn}, nil
		},
	}

	cfg := agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: mockLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     testMemory,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		LocalTools: []agent.Tool{
			dummyTool{name: "clinic_hours", desc: "opening hours"},
			dummyTool{name: "patient_records", desc: "patient records"},
			dummyTool{name: "lab_results", desc: "lab test results"},
			dummyTool{name: "pharmacy_stock", desc: "medicine stock"},
			dummyTool{name: "billing_invoices", desc: "billing invoices"},
		},
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "Hi")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	expectedSubstring := "tool clinic_hours is not available; call search_tools first"
	if !fmt.Contains(toolMsgContent, expectedSubstring) {
		t.Errorf("expected tool message to contain %q, got %q", expectedSubstring, toolMsgContent)
	}
}

func TestReAct_ToolCallThenAnswer(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	mockLLM := &mockLLMClient{
		generateFunc: func(ctx *context.Context, req llm.Request) (llm.Response, error) {
			hasToolResult := false
			for _, m := range req.Messages {
				if m.Role == llm.RoleTool {
					hasToolResult = true
					break
				}
			}

			if !hasToolResult {
				return llm.Response{
					Text:       "I will search for calculator.",
					StopReason: llm.StopToolUse,
					ToolCalls: []llm.ToolCall{
						{
							ID:    "call_search",
							Name:  "search_tools",
							Input: `{"query": "calculator"}`,
						},
					},
				}, nil
			} else {
				hasCalcResult := false
				for _, m := range req.Messages {
					if m.Role == llm.RoleTool && m.ToolName == "calculator" {
						hasCalcResult = true
						break
					}
				}

				if !hasCalcResult {
					return llm.Response{
						Text:       "Calling calculator",
						StopReason: llm.StopToolUse,
						ToolCalls: []llm.ToolCall{
							{
								ID:    "call_1",
								Name:  "calculator",
								Input: `{"a": 1, "b": 2}`,
							},
						},
					}, nil
				}

				return llm.Response{
					Text:       "The answer is 3.",
					StopReason: llm.StopEndTurn,
				}, nil
			}
		},
	}

	cfg := agent.Config{
		Identity: agentcontext.Identity{Name: "Bot"},
		LLMs: agent.LLMConfig{
			Primary: mockLLM,
		},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     testMemory,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		MCPServers: []string{testServer.URL},
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	reply, err := a.Run(ctx, sessionID, "What is 1+2?")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if reply.Text != "The answer is 3." {
		t.Errorf("expected answer 'The answer is 3.', got '%s'", reply.Text)
	}
}

func TestReAct_ToolErrorSelfCorrect(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	mockLLM := &mockLLMClient{
		generateFunc: func(ctx *context.Context, req llm.Request) (llm.Response, error) {
			hasError := false
			for _, m := range req.Messages {
				if m.Role == llm.RoleTool && fmt.Contains(m.Content, "Error") {
					hasError = true
					break
				}
			}

			if !hasError {
				return llm.Response{
					Text:       "Trying tool",
					StopReason: llm.StopToolUse,
					ToolCalls:  []llm.ToolCall{{Name: "unknown_tool", Input: "{}"}},
				}, nil
			}
			return llm.Response{Text: "Corrected Answer", StopReason: llm.StopEndTurn}, nil
		},
	}

	cfg := agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: mockLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     testMemory,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		LocalTools: []agent.Tool{
			dummyTool{name: "unknown_tool", desc: "unknown"},
			dummyTool{name: "t1", desc: "one"},
			dummyTool{name: "t2", desc: "two"},
			dummyTool{name: "t3", desc: "three"},
			dummyTool{name: "t4", desc: "four"},
		},
	}

	a, _ := agent.New(cfg)
	reply, err := a.Run(ctx, sessionID, "Hi")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if reply.Text != "Corrected Answer" {
		t.Errorf("expected Corrected Answer, got %s", reply.Text)
	}
}

func TestReAct_MaxIterationsGuard(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	mockLLM := &mockLLMClient{
		generateFunc: func(ctx *context.Context, req llm.Request) (llm.Response, error) {
			return llm.Response{
				Text:       "Looping",
				StopReason: llm.StopToolUse,
				ToolCalls:  []llm.ToolCall{{Name: "search_tools", Input: `{"query": "calculator"}`}},
			}, nil
		},
	}

	cfg := agent.Config{
		Identity:      agentcontext.Identity{Name: "Bot"},
		LLMs:          agent.LLMConfig{Primary: mockLLM},
		Tokens:        quarterCounter{},
		Budget:        agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:        testMemory,
		IDGen:         testIDGen,
		ToolIndex:     agent.NewMemToolIndex(),
		MCPServers:    []string{testServer.URL},
		MaxIterations: 3,
	}

	a, _ := agent.New(cfg)
	_, err := a.Run(ctx, sessionID, "Hi")
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

	step := 0
	mockLLM := &mockLLMClient{
		generateFunc: func(ctx *context.Context, req llm.Request) (llm.Response, error) {
			if len(req.Tools) == 0 {
				t.Error("expected tools to be offered, got none")
				return llm.Response{Text: "no tools", StopReason: llm.StopEndTurn}, nil
			}
			step++
			if step == 1 {
				return llm.Response{
					StopReason: llm.StopToolUse,
					ToolCalls:  []llm.ToolCall{{ID: "s1", Name: "search_tools", Input: `{"query":"calculator"}`}},
				}, nil
			}
			if step == 2 {
				return llm.Response{
					StopReason: llm.StopToolUse,
					ToolCalls:  []llm.ToolCall{{ID: "mcp1", Name: "calculator", Input: `{"a":3,"b":4}`}},
				}, nil
			}
			return llm.Response{Text: "Result via MCP", StopReason: llm.StopEndTurn}, nil
		},
	}

	cfg := agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: mockLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     testMemory,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		MCPServers: []string{testServer.URL},
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	reply, err := a.Run(ctx, sessionID, "compute")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if reply.Text != "Result via MCP" {
		t.Errorf("unexpected answer: %s", reply.Text)
	}
}

func TestRun_SendsOutputLimitNotContextSize(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	var recReq llm.Request
	mockLLM := &mockLLMClient{
		generateFunc: func(ctx *context.Context, req llm.Request) (llm.Response, error) {
			recReq = req
			return llm.Response{Text: "OK", StopReason: llm.StopEndTurn}, nil
		},
	}

	cfg := agent.Config{
		Identity:  agentcontext.Identity{Name: "Bot"},
		LLMs:      agent.LLMConfig{Primary: mockLLM},
		Tokens:    quarterCounter{},
		Budget:    agentcontext.Budget{ContextTokens: 8192, OutputTokens: 256},
		Memory:    testMemory,
		IDGen:     testIDGen,
		ToolIndex: agent.NewMemToolIndex(),
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "Hi")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if recReq.MaxOutputTokens != 256 {
		t.Errorf("expected MaxOutputTokens == 256, got %d", recReq.MaxOutputTokens)
	}
}

func TestRun_CompactsAndSummarizesOldestTurns(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	mem := agent.NewMemMemory()
	mem.EnsureSession(ctx, sessionID)

	for i := 1; i <= 30; i++ {
		turn := agentcontext.Turn{
			ID:        fmt.Sprintf("t-%d", i),
			Message:   llm.Message{Role: llm.RoleUser, Content: "1234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890"}, // 100 chars -> 25 tokens
			Tokens:    100,
			CreatedAt: int64(i),
		}
		mem.AppendTurn(ctx, sessionID, turn)
	}

	summarizerCalled := false
	var savedSum agentcontext.Summary

	mockLLM := &mockLLMClient{
		generateFunc: func(ctx *context.Context, req llm.Request) (llm.Response, error) {
			if fmt.Contains(req.System, "summar") || fmt.Contains(req.System, "Summar") {
				summarizerCalled = true
				return llm.Response{Text: "Summary of old turns", StopReason: llm.StopEndTurn}, nil
			}
			return llm.Response{Text: "Done", StopReason: llm.StopEndTurn}, nil
		},
	}

	cfg := agent.Config{
		Identity:        agentcontext.Identity{Name: "Bot"},
		LLMs:            agent.LLMConfig{Primary: mockLLM, Summarizer: mockLLM},
		Tokens:          quarterCounter{},
		Budget:          agentcontext.Budget{ContextTokens: 2000, OutputTokens: 256},
		RecentTurns:     50,
		RecentSummaries: 5,
		Memory:          mem,
		IDGen:           testIDGen,
		ToolIndex:       agent.NewMemToolIndex(),
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "New query")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !summarizerCalled {
		t.Errorf("expected summarizer to be called during compaction")
	}

	sums, err := mem.GetSummaries(ctx, sessionID, 10)
	if err != nil || len(sums) == 0 {
		t.Fatalf("expected summary saved, got %v, err=%v", sums, err)
	}
	savedSum = sums[0]

	if savedSum.FromTurnID != "t-1" {
		t.Errorf("expected FromTurnID == t-1, got %s", savedSum.FromTurnID)
	}

	turns, err := mem.GetTurns(ctx, sessionID, 100)
	if err != nil {
		t.Fatalf("GetTurns failed: %v", err)
	}
	for _, tr := range turns {
		if tr.ID == "t-1" {
			t.Errorf("expected t-1 deleted from memory after compaction, but it was found")
		}
	}
}

func TestRun_StopMaxTokensReturnsError(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	testMemory.EnsureSession(ctx, sessionID)

	mockLLM := &mockLLMClient{
		generateFunc: func(ctx *context.Context, req llm.Request) (llm.Response, error) {
			return llm.Response{
				StopReason: llm.StopMaxTokens,
				Text:       "Incomplete response...",
			}, nil
		},
	}

	cfg := agent.Config{
		Identity:  agentcontext.Identity{Name: "Bot"},
		LLMs:      agent.LLMConfig{Primary: mockLLM},
		Tokens:    quarterCounter{},
		Budget:    agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:    testMemory,
		IDGen:     testIDGen,
		ToolIndex: agent.NewMemToolIndex(),
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "Hi")
	if err == nil {
		t.Fatalf("expected error due to output truncation, got nil")
	}

	if !fmt.Contains(err.Error(), "Budget.OutputTokens") {
		t.Errorf("expected output truncated error, got %v", err)
	}
}

func TestRun_TurnsCarryTokenCounts(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()
	mem := agent.NewMemMemory()
	mem.EnsureSession(ctx, sessionID)

	mockLLM := &mockLLMClient{
		generateFunc: func(ctx *context.Context, req llm.Request) (llm.Response, error) {
			return llm.Response{Text: "12345678", StopReason: llm.StopEndTurn}, nil // 8 chars -> 2 tokens
		},
	}

	cfg := agent.Config{
		Identity:  agentcontext.Identity{Name: "Bot"},
		LLMs:      agent.LLMConfig{Primary: mockLLM},
		Tokens:    quarterCounter{},
		Budget:    agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:    mem,
		IDGen:     testIDGen,
		ToolIndex: agent.NewMemToolIndex(),
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "1234") // 4 chars -> 1 token
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	turns, err := mem.GetTurns(ctx, sessionID, 10)
	if err != nil {
		t.Fatalf("GetTurns failed: %v", err)
	}

	if len(turns) < 2 {
		t.Fatalf("expected at least 2 turns stored, got %d", len(turns))
	}

	for _, tr := range turns {
		expectedTokens := len(tr.Message.Content) / 4
		if tr.Tokens != expectedTokens {
			t.Errorf("turn %s content %q tokens = %d, want %d", tr.ID, tr.Message.Content, tr.Tokens, expectedTokens)
		}
	}
}

func TestNew_RequiresTokensAndBudget(t *testing.T) {
	mockLLM := &mockLLMClient{}

	cfgNoTokens := agent.Config{
		LLMs:      agent.LLMConfig{Primary: mockLLM},
		Memory:    testMemory,
		IDGen:     testIDGen,
		ToolIndex: agent.NewMemToolIndex(),
		Budget:    agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
	}

	_, err := agent.New(cfgNoTokens)
	if err == nil || !fmt.Contains(err.Error(), "Tokens is required") {
		t.Errorf("expected Tokens is required, got %v", err)
	}

	cfgBadBudget := agent.Config{
		LLMs:      agent.LLMConfig{Primary: mockLLM},
		Tokens:    quarterCounter{},
		Memory:    testMemory,
		IDGen:     testIDGen,
		ToolIndex: agent.NewMemToolIndex(),
		Budget:    agentcontext.Budget{ContextTokens: 0, OutputTokens: 0},
	}

	_, err = agent.New(cfgBadBudget)
	if err == nil || !fmt.Contains(err.Error(), "agent: Budget:") {
		t.Errorf("expected error containing 'agent: Budget:', got %v", err)
	}
}
