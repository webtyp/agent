package tests

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/context"
	"webtyp.com/llm"
)

// TestMemory verifies turn recording, tool call logging, and session state continuity.
func TestMemory(t *testing.T) {
	ctx := context.Background()

	tool := &fakeTool{name: "hours", description: "horarios de atencion", result: "Atendemos de 9 a 18"}

	dec := &scriptedDecider{
		t: t,
		answers: []scripted{
			{Text: "Which tool should the assistant use?", Choice: 0, Confidence: 0.95},
			{Text: "Is the message a question whose answer is yes or no?", Choice: 0, Confidence: 0.95},
		},
	}

	mem := agent.NewMemMemory()
	a, err := agent.New(agent.Config{
		Decider:    dec,
		Texts:      jose(),
		Tokens:     quarterCounter{},
		Memory:     mem,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		LocalTools: []agent.Tool{tool},
	})
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	userMsg := "Cual es el horario de atencion?"
	reply, err := a.Run(ctx, "s1", userMsg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	turns, err := mem.GetTurns(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("failed to get turns from memory: %v", err)
	}

	if len(turns) != 4 {
		t.Fatalf("expected 4 turns stored, got %d", len(turns))
	}

	// Order of stored turns: user turn, assistant turn (tool call), tool turn, assistant turn (reply)
	if turns[0].Message.Role != llm.RoleUser || turns[0].Message.Content != userMsg {
		t.Fatalf("turn 0 expected User %q, got %s %q", userMsg, turns[0].Message.Role, turns[0].Message.Content)
	}

	if turns[1].Message.Role != llm.RoleAssistant || len(turns[1].Message.ToolCalls) != 1 || turns[1].Message.ToolCalls[0].Name != tool.name {
		t.Fatalf("turn 1 expected Assistant tool call %s, got %v", tool.name, turns[1])
	}

	if turns[2].Message.Role != llm.RoleTool || turns[2].Message.Content != tool.result {
		t.Fatalf("turn 2 expected Tool %q, got %s %q", tool.result, turns[2].Message.Role, turns[2].Message.Content)
	}

	if turns[3].Message.Role != llm.RoleAssistant || turns[3].Message.Content != reply.Text {
		t.Fatalf("turn 3 expected Assistant %q, got %s %q", reply.Text, turns[3].Message.Role, turns[3].Message.Content)
	}

	logs, err := mem.GetToolLogs(ctx, "s1", tool.name, 10)
	if err != nil {
		t.Fatalf("failed to get tool logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 logged tool call, got %d", len(logs))
	}
	if logs[0].ToolName != tool.name || logs[0].OutputText != tool.result {
		t.Fatalf("unexpected tool log record: %+v", logs[0])
	}
}
