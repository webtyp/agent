package tests

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"webtyp.com/agent"
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/fmt"
	webtypjson "webtyp.com/json"
	"webtyp.com/llm"
	"webtyp.com/mcp"
	"webtyp.com/model"
)

type recordingScriptedLLM struct {
	script   []llm.Response
	step     int
	requests []llm.Request
}

func (m *recordingScriptedLLM) Generate(ctx *context.Context, req llm.Request) (llm.Response, error) {
	m.requests = append(m.requests, req)
	if m.step < len(m.script) {
		resp := m.script[m.step]
		m.step++
		return resp, nil
	}
	return llm.Response{StopReason: llm.StopEndTurn, Text: "Default response"}, nil
}

type trackTool struct {
	name      string
	action    model.Action
	callCount int
}

func (t *trackTool) Name() string         { return t.name }
func (t *trackTool) Description() string  { return "track tool" }
func (t *trackTool) InputSchema() string  { return `{"type":"object"}` }
func (t *trackTool) Action() model.Action { return t.action }
func (t *trackTool) Execute(ctx *context.Context, args string) (string, error) {
	t.callCount++
	return "ok", nil
}

func TestConfirm_ModifyingToolPausesAndConfirms(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	createTool := &trackTool{name: "book_appt", action: model.Create}

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{
				StopReason: llm.StopToolUse,
				Text:       "I will book it.",
				ToolCalls:  []llm.ToolCall{{ID: "c1", Name: "book_appt", Input: "{}"}},
			},
			{
				StopReason: llm.StopEndTurn,
				Text:       "Appointment booked!",
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
		LocalTools: []agent.Tool{createTool},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reply, err := a.Run(ctx, sessionID, "Book appt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(reply.Pending) != 1 || reply.Pending[0].Name != "book_appt" {
		t.Fatalf("expected 1 pending call for book_appt, got %v", reply.Pending)
	}
	if reply.Text != "I will book it." {
		t.Errorf("expected text 'I will book it.', got %q", reply.Text)
	}
	if createTool.callCount != 0 {
		t.Errorf("expected 0 tool calls before confirm, got %d", createTool.callCount)
	}

	turns, err := mem.GetTurns(ctx, sessionID, 1)
	if err != nil || len(turns) == 0 {
		t.Fatalf("GetTurns failed: %v", err)
	}
	if turns[0].Message.Role != llm.RoleAssistant || len(turns[0].Message.ToolCalls) != 1 {
		t.Errorf("expected stored last turn to be assistant tool-call turn, got %v", turns[0])
	}

	// Confirm
	confirmReply, err := a.Confirm(ctx, sessionID)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	if createTool.callCount != 1 {
		t.Errorf("expected 1 tool call after confirm, got %d", createTool.callCount)
	}
	if confirmReply.Text != "Appointment booked!" {
		t.Errorf("expected 'Appointment booked!', got %q", confirmReply.Text)
	}
}

func TestDecline_ModifyingToolDeclines(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	createTool := &trackTool{name: "cancel_appt", action: model.Delete}

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{
				StopReason: llm.StopToolUse,
				Text:       "I will cancel it.",
				ToolCalls:  []llm.ToolCall{{ID: "c1", Name: "cancel_appt", Input: "{}"}},
			},
			{
				StopReason: llm.StopEndTurn,
				Text:       "Understood, cancellation was declined.",
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
		LocalTools: []agent.Tool{createTool},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reply, err := a.Run(ctx, sessionID, "Cancel appt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(reply.Pending) != 1 {
		t.Fatalf("expected pending call")
	}

	declineReply, err := a.Decline(ctx, sessionID)
	if err != nil {
		t.Fatalf("Decline: %v", err)
	}

	if createTool.callCount != 0 {
		t.Errorf("expected Execute never called on decline, got %d", createTool.callCount)
	}
	if declineReply.Text != "Understood, cancellation was declined." {
		t.Errorf("unexpected decline text: %q", declineReply.Text)
	}

	lastReq := scriptLLM.requests[len(scriptLLM.requests)-1]
	foundDeclined := false
	for _, m := range lastReq.Messages {
		if m.Role == llm.RoleTool && fmt.Contains(m.Content, "declined this action") {
			foundDeclined = true
			break
		}
	}
	if !foundDeclined {
		t.Errorf("expected last request to contain declined tool message, got %v", lastReq.Messages)
	}
}

func TestRunWhilePending_AutoDeclinesPendingAndAppendsUserQuery(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	createTool := &trackTool{name: "delete_account", action: model.Delete}

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{
				StopReason: llm.StopToolUse,
				Text:       "Deleting...",
				ToolCalls:  []llm.ToolCall{{ID: "c1", Name: "delete_account", Input: "{}"}},
			},
			{
				StopReason: llm.StopEndTurn,
				Text:       "Okay, ignoring that deletion.",
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
		LocalTools: []agent.Tool{createTool},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "Delete my account")
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}

	_, err = a.Run(ctx, sessionID, "Nevermind, what is the time?")
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}

	lastReq := scriptLLM.requests[len(scriptLLM.requests)-1]
	declinedIdx := -1
	userQueryIdx := -1
	for i, m := range lastReq.Messages {
		if m.Role == llm.RoleTool && fmt.Contains(m.Content, "declined this action") {
			declinedIdx = i
		}
		if m.Role == llm.RoleUser && fmt.Contains(m.Content, "Nevermind") {
			userQueryIdx = i
		}
	}

	if declinedIdx == -1 || userQueryIdx == -1 || declinedIdx >= userQueryIdx {
		t.Errorf("expected declined result to precede new user turn in request, got declinedIdx=%d, userQueryIdx=%d", declinedIdx, userQueryIdx)
	}
}

func TestConfirmAndDecline_NothingPendingErrors(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	scriptLLM := &recordingScriptedLLM{}
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
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = a.Confirm(ctx, sessionID)
	if err == nil || !fmt.Contains(err.Error(), "agent: nothing to confirm in this session") {
		t.Errorf("expected 'agent: nothing to confirm in this session', got %v", err)
	}

	_, err = a.Decline(ctx, sessionID)
	if err == nil || !fmt.Contains(err.Error(), "agent: nothing to decline in this session") {
		t.Errorf("expected 'agent: nothing to decline in this session', got %v", err)
	}
}

func TestRun_ReadToolRunsWithoutPausing(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	readTool := &trackTool{name: "get_info", action: model.Read}

	scriptLLM := &recordingScriptedLLM{
		script: []llm.Response{
			{
				StopReason: llm.StopToolUse,
				ToolCalls:  []llm.ToolCall{{ID: "c1", Name: "search_tools", Input: `{"query":"info"}`}},
			},
			{
				StopReason: llm.StopToolUse,
				ToolCalls:  []llm.ToolCall{{ID: "c2", Name: "get_info", Input: "{}"}},
			},
			{
				StopReason: llm.StopEndTurn,
				Text:       "Info details",
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
		LocalTools: []agent.Tool{readTool},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reply, err := a.Run(ctx, sessionID, "Get info")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(reply.Pending) != 0 {
		t.Errorf("expected 0 pending for read-only tool, got %v", reply.Pending)
	}
	if reply.Text != "Info details" {
		t.Errorf("expected 'Info details', got %q", reply.Text)
	}
	if readTool.callCount != 1 {
		t.Errorf("expected 1 execution, got %d", readTool.callCount)
	}
}

type mcpToolProvider struct{}

func (p mcpToolProvider) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "read_mcp",
			Description: "Read MCP tool",
			Resource:    "read_mcp",
			Action:      model.Read,
			Access:      model.AccessGuarded,
			Execute: func(ctx *context.Context, req mcp.Request) (*mcp.Result, error) {
				return mcp.Text("read result"), nil
			},
		},
		{
			Name:        "modify_mcp",
			Description: "Modify MCP tool",
			Action:      model.Create,
			Resource:    "modify_mcp",
			Access:      model.AccessGuarded,
			Execute: func(ctx *context.Context, req mcp.Request) (*mcp.Result, error) {
				return mcp.Text("modify result"), nil
			},
		},
	}
}

func TestRun_MCPToolsReadOnlyHint(t *testing.T) {
	sessionID := t.Name()
	ctx := context.Background()

	srv, err := mcp.NewServer(
		mcp.Config{Name: "mcptest", Version: "1.0.0", Authorize: mcp.AllowAll},
		[]mcp.ToolProvider{mcpToolProvider{}},
	)
	if err != nil {
		t.Fatalf("mcp.NewServer: %v", err)
	}

	mcpHttpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		resp := srv.HandleMessage(ctx, body)
		w.Header().Set("Content-Type", "application/json")
		var out []byte
		if enc, ok := resp.(model.Encodable); ok {
			webtypjson.Encode(enc, &out)
		}
		w.Write(out)
	}))
	defer mcpHttpSrv.Close()

	scriptLLM1 := &recordingScriptedLLM{
		script: []llm.Response{
			{
				StopReason: llm.StopToolUse,
				ToolCalls:  []llm.ToolCall{{ID: "c1", Name: "search_tools", Input: `{"query":"read"}`}},
			},
			{
				StopReason: llm.StopToolUse,
				ToolCalls:  []llm.ToolCall{{ID: "c2", Name: "read_mcp", Input: "{}"}},
			},
			{
				StopReason: llm.StopEndTurn,
				Text:       "Read complete",
			},
		},
	}

	mem1 := agent.NewMemMemory()
	mem1.EnsureSession(ctx, sessionID)

	a1, err := agent.New(agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: scriptLLM1},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     mem1,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		MCPServers: []string{mcpHttpSrv.URL},
	})
	if err != nil {
		t.Fatalf("New 1: %v", err)
	}

	reply1, err := a1.Run(ctx, sessionID, "Read data")
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}

	if len(reply1.Pending) != 0 || reply1.Text != "Read complete" {
		t.Errorf("expected read MCP tool to run without pausing, got pending=%v, text=%q", reply1.Pending, reply1.Text)
	}

	scriptLLM2 := &recordingScriptedLLM{
		script: []llm.Response{
			{
				StopReason: llm.StopToolUse,
				ToolCalls:  []llm.ToolCall{{ID: "c1", Name: "search_tools", Input: `{"query":"modify"}`}},
			},
			{
				StopReason: llm.StopToolUse,
				Text:       "I want to modify",
				ToolCalls:  []llm.ToolCall{{ID: "c2", Name: "modify_mcp", Input: "{}"}},
			},
		},
	}

	mem2 := agent.NewMemMemory()
	mem2.EnsureSession(ctx, sessionID)

	a2, err := agent.New(agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: scriptLLM2},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     mem2,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		MCPServers: []string{mcpHttpSrv.URL},
	})
	if err != nil {
		t.Fatalf("New 2: %v", err)
	}

	reply2, err := a2.Run(ctx, sessionID, "Modify data")
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}

	if len(reply2.Pending) != 1 || reply2.Pending[0].Name != "modify_mcp" {
		t.Errorf("expected modify MCP tool to pause, got pending=%v", reply2.Pending)
	}
}
