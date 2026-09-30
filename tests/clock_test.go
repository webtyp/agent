package tests

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/llm"
	"webtyp.com/unixid"
)

type fixedClock struct{}

func (fixedClock) Now() int64            { return 1790686800 * 1e9 }
func (fixedClock) UTCOffsetMinutes() int { return -180 }

type recordingClockLLM struct{ requests []llm.Request }

func (r *recordingClockLLM) Generate(ctx *context.Context, req llm.Request) (llm.Response, error) {
	r.requests = append(r.requests, req)
	return llm.Response{Text: "Hasta las 18:00.", StopReason: llm.StopEndTurn}, nil
}

func TestRun_ModelSeesTheUsersLocalDateAndTime(t *testing.T) {
	ids, err := unixid.NewUnixID()
	if err != nil {
		t.Fatal(err)
	}
	model := &recordingClockLLM{}
	a, err := agent.New(agent.Config{
		Identity:  agentcontext.Identity{Name: "Jose"},
		LLMs:      agent.LLMConfig{Primary: model},
		Tokens:    quarterCounter{},
		Budget:    agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:    agent.NewMemMemory(),
		IDGen:     ids,
		ToolIndex: agent.NewMemToolIndex(),
		Clock:     fixedClock{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := a.Run(context.Background(), "s1", "¿Hasta qué hora atendemos hoy?"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := "[2026-09-29 Tuesday 10:00]\n¿Hasta qué hora atendemos hoy?"
	first := model.requests[0]
	last := first.Messages[len(first.Messages)-1]
	if last.Role != llm.RoleUser || last.Content != want {
		t.Errorf("last message of the first request:\n got %s %q\nwant user %q", last.Role, last.Content, want)
	}
	if fmt.Contains(first.System, "2026-09-29") {
		t.Errorf("the date must not be in System, the stable prefix: %q", first.System)
	}
}
