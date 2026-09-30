package agent_test

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/llm"
	"webtyp.com/unixid"
)

// fixedClock is Tuesday 2026-09-29 10:00 in Chile (13:00 UTC, UTC-3).
type fixedClock struct{}

func (fixedClock) Now() int64            { return 1790686800 * 1e9 }
func (fixedClock) UTCOffsetMinutes() int { return -180 }

type recordingLLM struct{ requests []llm.Request }

// Generate records the reasoning requests and approves every answer when asked as the critic.
func (r *recordingLLM) Generate(ctx *context.Context, req llm.Request) (llm.Response, error) {
	if fmt.Contains(req.System, "critic") {
		return llm.Response{Text: "SUFFICIENT", StopReason: llm.StopEndTurn}, nil
	}
	r.requests = append(r.requests, req)
	return llm.Response{Text: "Hasta las 18:00.", StopReason: llm.StopEndTurn}, nil
}

type charCounter struct{}

func (charCounter) CountTokens(s string) int { return len(s) / 4 }

func TestRun_ModelSeesTheUsersLocalDateAndTime(t *testing.T) {
	ids, err := unixid.NewUnixID()
	if err != nil {
		t.Fatal(err)
	}
	model := &recordingLLM{}
	a, err := agent.New(agent.Config{
		Identity:  agentcontext.Identity{Name: "Jose"},
		LLMs:      agent.LLMConfig{Primary: model},
		Tokens:    charCounter{},
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

	want := "[2026-09-29 Tuesday 10:00 UTC-03:00]\n¿Hasta qué hora atendemos hoy?"
	first := model.requests[0]
	last := first.Messages[len(first.Messages)-1]
	if last.Role != llm.RoleUser || last.Content != want {
		t.Errorf("last message of the first request:\n got %s %q\nwant user %q", last.Role, last.Content, want)
	}
	if fmt.Contains(first.System, "2026-09-29") {
		t.Errorf("the date must not be in System, the stable prefix: %q", first.System)
	}
}
