package tests

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/context"
	"webtyp.com/mcp"
	"webtyp.com/model"
)

type hoursProvider struct{}

func (hoursProvider) Tools() []mcp.Tool {
	return []mcp.Tool{{
		Name:        "calendar.list_business_hours",
		Description: "horarios de atencion",
		Resource:    "calendar",
		Action:      model.Read,
		Execute: func(ctx *context.Context, req mcp.Request) (*mcp.Result, error) {
			return mcp.Text("9 a 18"), nil
		},
	}}
}

// A real mcp.Server reached in process through Config.MCPClients: its tools are listed, chosen
// and run exactly as over HTTP, and the answer is the tool's text (D28).
func TestMCPClients_LocalServer(t *testing.T) {
	srv, err := mcp.NewServer(mcp.Config{Name: "clinic", Version: "1", Authorize: mcp.AllowAll},
		[]mcp.ToolProvider{hoursProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	dec := &scriptedDecider{t: t, answers: []scripted{
		{Text: "Which tool should the assistant use?", Choice: 0, Confidence: 0.95},
		{Text: "Is the message a question whose answer is yes or no?", Choice: 0, Confidence: 0.95},
	}}
	a, err := agent.New(agent.Config{
		Decider:    dec,
		Texts:      cote(),
		Tokens:     quarterCounter{},
		Memory:     agent.NewMemMemory(),
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		MCPClients: []*mcp.Client{mcp.NewLocalClient(srv, "staff-1")},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	reply, err := a.Run(context.Background(), "s1", "A que hora abren?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := cote().Found + "\n9 a 18"; reply.Text != want {
		t.Fatalf("reply %q, want %q", reply.Text, want)
	}
}
