package tests

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/llm"
	"webtyp.com/model"
)

func loopGuardAgent(t *testing.T, script []llm.Response, tool *trackTool, maxIterations int) (*agent.Agent, *recordingScriptedLLM) {
	t.Helper()
	m := &recordingScriptedLLM{script: script}
	a, err := agent.New(agent.Config{
		Identity:      agentcontext.Identity{Name: "Bot"},
		LLMs:          agent.LLMConfig{Primary: m},
		Tokens:        quarterCounter{},
		Budget:        agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:        agent.NewMemMemory(),
		IDGen:         testIDGen,
		ToolIndex:     agent.NewMemToolIndex(),
		LocalTools:    []agent.Tool{tool},
		MaxIterations: maxIterations,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, m
}

func callHours(id string) llm.Response {
	return llm.Response{StopReason: llm.StopToolUse, ToolCalls: []llm.ToolCall{{ID: id, Name: "hours", Input: "{}"}}}
}

// A model that calls the same tool with the same arguments again gets told it already has the
// result; the tool runs once.
func TestLoop_IdenticalRepeatedCallRunsOnce(t *testing.T) {
	tool := &trackTool{name: "hours", action: model.Read}
	a, m := loopGuardAgent(t, []llm.Response{
		callHours("c1"), callHours("c2"), callHours("c3"),
		{StopReason: llm.StopEndTurn, Text: "Hasta las 18:00."},
	}, tool, 10)

	reply, err := a.Run(context.Background(), t.Name(), "¿Hasta qué hora?")
	if err != nil {
		t.Fatal(err)
	}
	if tool.callCount != 1 {
		t.Fatalf("the tool ran %d times, want 1", tool.callCount)
	}
	if reply.Text != "Hasta las 18:00." {
		t.Fatalf("reply %q", reply.Text)
	}
	last := m.requests[len(m.requests)-1].Messages
	if got := last[len(last)-1].Content; got != "You already called hours with these arguments in this turn; its result is above. Answer the person with it." {
		t.Fatalf("the repeated call's result is %q", got)
	}
}

// The last allowed step offers no tools, so the model answers instead of running out of steps.
func TestLoop_LastStepOffersNoTools(t *testing.T) {
	tool := &trackTool{name: "hours", action: model.Read}
	a, m := loopGuardAgent(t, []llm.Response{
		callHours("c1"), callHours("c2"),
		{StopReason: llm.StopEndTurn, Text: "Hasta las 18:00."},
	}, tool, 3)

	reply, err := a.Run(context.Background(), t.Name(), "¿Hasta qué hora?")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Hasta las 18:00." {
		t.Fatalf("reply %q", reply.Text)
	}
	if n := len(m.requests); n != 3 {
		t.Fatalf("%d requests, want 3", n)
	}
	if tools := m.requests[2].Tools; len(tools) != 0 {
		t.Fatalf("the last step offered %d tools, want none", len(tools))
	}
	if len(m.requests[0].Tools) == 0 {
		t.Fatal("earlier steps must still offer tools")
	}
}
