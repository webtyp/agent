package tests

import (
	"strings"
	"testing"

	"webtyp.com/agent"
	"webtyp.com/context"
)

// TestArguments verifies JSON input schema interpretation and parameter extraction.
func TestArguments(t *testing.T) {
	ctx := context.Background()

	setupAgent := func(dec *scriptedDecider, tool *fakeTool) *agent.Agent {
		a, err := agent.New(agent.Config{
			Decider:    dec,
			Texts:      cote(),
			Tokens:     quarterCounter{},
			Memory:     agent.NewMemMemory(),
			IDGen:      testIDGen,
			ToolIndex:  agent.NewMemToolIndex(),
			LocalTools: []agent.Tool{tool},
		})
		if err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
		return a
	}

	t.Run("string property passes whole message", func(t *testing.T) {
		tool := &fakeTool{
			name:        "search",
			description: "search info",
			schema:      `{"type":"object","properties":{"query":{"type":"string"}}}`,
			result:      "ok",
		}
		dec := &scriptedDecider{
			t: t,
			answers: []scripted{
				{Text: "Which tool should the assistant use?", Choice: 0, Confidence: 0.95},
				{Text: "Is the message a question whose answer is yes or no?", Choice: 0, Confidence: 0.95},
			},
		}
		a := setupAgent(dec, tool)
		msg := "buscar informacion de dr perez"
		_, err := a.Run(ctx, "s1", msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tool.calls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(tool.calls))
		}
		expectedArgs := `{"query":"` + msg + `"}`
		if tool.calls[0] != expectedArgs {
			t.Fatalf("expected args %q, got %q", expectedArgs, tool.calls[0])
		}
	})

	t.Run("enum property prompts decider for choice", func(t *testing.T) {
		tool := &fakeTool{
			name:        "appointment",
			description: "manage appointment",
			schema:      `{"type":"object","properties":{"status":{"type":"string","enum":["anulada","confirmada"]}}}`,
			result:      "ok",
		}
		dec := &scriptedDecider{
			t: t,
			answers: []scripted{
				{Text: "Which tool should the assistant use?", Choice: 0, Confidence: 0.95},
				{Text: "Which value of \"status\" does the message ask for?", Choice: 0, Confidence: 0.95},
				{Text: "Is the message a question whose answer is yes or no?", Choice: 0, Confidence: 0.95},
			},
		}
		a := setupAgent(dec, tool)
		_, err := a.Run(ctx, "s1", "quiero anular mi cita")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tool.calls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(tool.calls))
		}
		expectedArgs := `{"status":"anulada"}`
		if tool.calls[0] != expectedArgs {
			t.Fatalf("expected args %q, got %q", expectedArgs, tool.calls[0])
		}
	})

	t.Run("single value enum populated without asking decider and integer property omitted", func(t *testing.T) {
		tool := &fakeTool{
			name:        "filter",
			description: "filter items",
			schema:      `{"type":"object","properties":{"fixed":{"type":"string","enum":["default"]},"count":{"type":"integer"}}}`,
			result:      "ok",
		}
		dec := &scriptedDecider{
			t: t,
			answers: []scripted{
				{Text: "Which tool should the assistant use?", Choice: 0, Confidence: 0.95},
				{Text: "Is the message a question whose answer is yes or no?", Choice: 0, Confidence: 0.95},
			},
		}
		a := setupAgent(dec, tool)
		_, err := a.Run(ctx, "s1", "filtra por favor")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tool.calls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(tool.calls))
		}
		expectedArgs := `{"fixed":"default"}`
		if tool.calls[0] != expectedArgs {
			t.Fatalf("expected args %q, got %q", expectedArgs, tool.calls[0])
		}
	})

	t.Run("invalid schema json returns tool schema error", func(t *testing.T) {
		tool := &fakeTool{
			name:        "broken",
			description: "broken schema",
			schema:      `not json`,
			result:      "ok",
		}
		dec := &scriptedDecider{
			t: t,
			answers: []scripted{
				{Text: "Which tool should the assistant use?", Choice: 0, Confidence: 0.95},
			},
		}
		a := setupAgent(dec, tool)
		_, err := a.Run(ctx, "s1", "test")
		if err == nil {
			t.Fatalf("expected error for invalid schema, got nil")
		}
		if !strings.HasPrefix(err.Error(), "agent: tool broken: reading InputSchema:") {
			t.Fatalf("expected error starting with 'agent: tool broken: reading InputSchema:', got %q", err.Error())
		}
	})
}
