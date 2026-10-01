package tests

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/context"
	"webtyp.com/llm"
)

// TestRoute verifies candidate tool selection and routing choices.
func TestRoute(t *testing.T) {
	ctx := context.Background()

	setupAgent := func(dec *scriptedDecider, tools []agent.Tool, candidates int, index agent.ToolIndex) *agent.Agent {
		if index == nil {
			index = agent.NewMemToolIndex()
		}
		a, err := agent.New(agent.Config{
			Decider:    dec,
			Texts:      cote(),
			Tokens:     quarterCounter{},
			Memory:     agent.NewMemMemory(),
			IDGen:      testIDGen,
			ToolIndex:  index,
			Candidates: candidates,
			LocalTools: tools,
		})
		if err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
		return a
	}

	// Each subtest gets fresh tools, so the calls of one subtest are not counted in the next.
	newTools := func() (*fakeTool, *fakeTool, *fakeTool) {
		return &fakeTool{name: "hours", description: "horarios de atencion", result: "9 a 18"},
			&fakeTool{name: "patients", description: "informacion de pacientes", result: "Juan Perez"},
			&fakeTool{name: "services", description: "servicios ofrecidos", result: "Medicina general"}
	}

	t.Run("high confidence choice runs tool", func(t *testing.T) {
		hours, patients, services := newTools()
		dec := &scriptedDecider{
			t: t,
			answers: []scripted{
				{Text: "Which tool should the assistant use?", Choice: 0, Confidence: 0.95},
				{Text: "Is the message a question whose answer is yes or no?", Choice: 0, Confidence: 0.95},
			},
		}
		a := setupAgent(dec, []agent.Tool{hours, patients, services}, 5, nil)

		reply, err := a.Run(ctx, "s1", "A que hora abren?")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if reply.Text != cote().Found+"\n"+hours.result {
			t.Fatalf("unexpected reply text: %q", reply.Text)
		}
		if len(hours.calls) != 1 {
			t.Fatalf("expected hours to be called 1 time, called %d", len(hours.calls))
		}
		if hours.calls[0] != "{}" {
			t.Fatalf("expected args {}, got %q", hours.calls[0])
		}
	})

	t.Run("choice none returns NoTool", func(t *testing.T) {
		hours, patients, services := newTools()
		dec := &scriptedDecider{
			t: t,
			answers: []scripted{
				{Text: "Which tool should the assistant use?", Choice: 3, Confidence: 0.95},
			},
		}
		a := setupAgent(dec, []agent.Tool{hours, patients, services}, 5, nil)

		reply, err := a.Run(ctx, "s1", "Hola, buenas tardes")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if reply.Text != cote().NoTool {
			t.Fatalf("expected NoTool text, got %q", reply.Text)
		}
		if len(hours.calls) != 0 || len(patients.calls) != 0 || len(services.calls) != 0 {
			t.Fatalf("expected no tools to run")
		}
	})

	t.Run("low confidence returns clarify with top two candidate descriptions", func(t *testing.T) {
		hours, patients, services := newTools()
		dec := &scriptedDecider{
			t: t,
			answers: []scripted{
				{Text: "Which tool should the assistant use?", Choice: 0, Confidence: 0.5},
			},
		}
		a := setupAgent(dec, []agent.Tool{hours, patients, services}, 5, nil)

		reply, err := a.Run(ctx, "s1", "quiero saber de la consulta")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedClarify := cote().Clarify + "\n- " + hours.description + "\n- " + patients.description
		if reply.Text != expectedClarify {
			t.Fatalf("expected clarify:\n%q\ngot:\n%q", expectedClarify, reply.Text)
		}
		if len(hours.calls) != 0 || len(patients.calls) != 0 || len(services.calls) != 0 {
			t.Fatalf("expected no tools to run")
		}
	})

	t.Run("route question context and options format", func(t *testing.T) {
		hours, patients, services := newTools()
		dec := &scriptedDecider{
			t: t,
			answers: []scripted{
				{Text: "Which tool should the assistant use?", Choice: 3, Confidence: 0.95},
			},
		}
		a := setupAgent(dec, []agent.Tool{hours, patients, services}, 5, nil)

		msg := "A que hora abren?"
		_, err := a.Run(ctx, "s1", msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(dec.asked) == 0 {
			t.Fatalf("expected decider to be asked")
		}
		q := dec.asked[0]
		expectedContext := cote().Speaker + " wrote: " + msg
		if q.Context != expectedContext {
			t.Fatalf("expected context %q, got %q", expectedContext, q.Context)
		}

		expectedOptions := []string{
			"hours: " + hours.description,
			"patients: " + patients.description,
			"services: " + services.description,
			"none: " + cote().NoToolOption,
		}
		if len(q.Options) != len(expectedOptions) {
			t.Fatalf("expected %d options, got %d", len(expectedOptions), len(q.Options))
		}
		for i, opt := range expectedOptions {
			if q.Options[i] != opt {
				t.Fatalf("option %d expected %q, got %q", i, opt, q.Options[i])
			}
		}
	})

	t.Run("search tools preselection when candidates limit smaller than total tools", func(t *testing.T) {
		t1 := &fakeTool{name: "t1", description: "d1"}
		t2 := &fakeTool{name: "t2", description: "d2"}
		t3 := &fakeTool{name: "t3", description: "d3"}
		t4 := &fakeTool{name: "t4", description: "d4"}
		t5 := &fakeTool{name: "t5", description: "d5"}
		t6 := &fakeTool{name: "t6", description: "d6"}
		allTools := []agent.Tool{t1, t2, t3, t4, t5, t6}

		fakeIndex := &fixedIndex{results: []string{"t3", "t5"}}

		dec := &scriptedDecider{
			t: t,
			answers: []scripted{
				{Text: "Which tool should the assistant use?", Choice: 2, Confidence: 0.95},
			},
		}

		a := setupAgent(dec, allTools, 2, fakeIndex)
		reply, err := a.Run(ctx, "s1", "busco t3 o t5")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if reply.Text != cote().NoTool {
			t.Fatalf("expected NoTool for choice 2 (none), got %q", reply.Text)
		}

		if len(dec.asked) == 0 {
			t.Fatalf("expected decider asked")
		}
		q := dec.asked[0]
		expectedOptions := []string{
			"t3: d3",
			"t5: d5",
			"none: " + cote().NoToolOption,
		}
		if len(q.Options) != len(expectedOptions) {
			t.Fatalf("expected %d options, got %d", len(expectedOptions), len(q.Options))
		}
		for i, opt := range expectedOptions {
			if q.Options[i] != opt {
				t.Fatalf("option %d expected %q, got %q", i, opt, q.Options[i])
			}
		}
	})
}

type fixedIndex struct {
	results []string
}

func (f *fixedIndex) IndexTools(ctx *context.Context, tools []llm.ToolDef) error { return nil }
func (f *fixedIndex) SearchTools(ctx *context.Context, query string, limit int) ([]string, error) {
	return f.results, nil
}
