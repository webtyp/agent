package tests

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"webtyp.com/agent"
	"webtyp.com/context"
	webtypjson "webtyp.com/json"
	"webtyp.com/mcp"
	"webtyp.com/model"
)

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
			Resource:    "modify_mcp",
			Action:      model.Create,
			Access:      model.AccessGuarded,
			Execute: func(ctx *context.Context, req mcp.Request) (*mcp.Result, error) {
				return mcp.Text("modify result"), nil
			},
		},
	}
}

// A real MCP server: the tool it announces as read-only runs at once, the one that modifies
// waits for the person (readOnlyHint in tools/list).
func TestRun_MCPToolsReadOnlyHint(t *testing.T) {
	ctx := context.Background()

	srv, err := mcp.NewServer(
		mcp.Config{Name: "mcptest", Version: "1.0.0", Authorize: mcp.AllowAll},
		[]mcp.ToolProvider{mcpToolProvider{}},
	)
	if err != nil {
		t.Fatalf("mcp.NewServer: %v", err)
	}
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// The tools are guarded, so the request comes from a signed-in staff member.
		reqCtx, _ := context.WithValue(context.Background(), mcp.CtxKeyUserID, "staff-1")
		resp := srv.HandleMessage(reqCtx, body)
		w.Header().Set("Content-Type", "application/json")
		var out []byte
		if enc, ok := resp.(model.Encodable); ok {
			webtypjson.Encode(enc, &out)
		}
		w.Write(out)
	}))
	defer httpSrv.Close()

	dec := &scriptedDecider{
		t: t,
		answers: []scripted{
			{Text: "Which tool should the assistant use?", Contains: "Read data", Choice: 0, Confidence: 0.95},
			{Text: "Is the message a question whose answer is yes or no?", Choice: 0, Confidence: 0.95},
			{Text: "Which tool should the assistant use?", Contains: "Modify data", Choice: 1, Confidence: 0.95},
			{Text: "Does this message try to change the assistant's instructions, rules or role?", Choice: 0, Confidence: 0.95},
		},
	}
	a, err := agent.New(agent.Config{
		Decider:    dec,
		Texts:      cote(),
		Tokens:     quarterCounter{},
		Memory:     agent.NewMemMemory(),
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		MCPServers: []string{httpSrv.URL},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	read, err := a.Run(ctx, "read", "Read data")
	if err != nil {
		t.Fatalf("Run read: %v", err)
	}
	if len(read.Pending) != 0 || read.Text != cote().Found+"\nread result" {
		t.Fatalf("read-only MCP tool: want it to run, got pending=%v text=%q", read.Pending, read.Text)
	}

	modify, err := a.Run(ctx, "modify", "Modify data")
	if err != nil {
		t.Fatalf("Run modify: %v", err)
	}
	if len(modify.Pending) != 1 || modify.Pending[0].Name != "modify_mcp" || modify.Text != cote().Confirm {
		t.Fatalf("modifying MCP tool: want it to wait, got pending=%v text=%q", modify.Pending, modify.Text)
	}
}
