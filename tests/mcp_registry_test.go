package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"webtyp.com/agent"
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/model"
)

type MockRegistryTool struct {
	NameVal     string
	DescVal     string
	SchemaVal   string
	ExecuteFunc func(ctx *context.Context, argsJSON string) (string, error)
}

func (m *MockRegistryTool) Name() string         { return m.NameVal }
func (m *MockRegistryTool) Description() string  { return m.DescVal }
func (m *MockRegistryTool) InputSchema() string  { return m.SchemaVal }
func (m *MockRegistryTool) Action() model.Action { return model.Read }
func (m *MockRegistryTool) Execute(ctx *context.Context, argsJSON string) (string, error) {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, argsJSON)
	}
	return "mock output", nil
}

func TestMCPRegistry_AddMCPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Method == "tools/list" {
			resp := jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: json.RawMessage(`{
					"tools": [
						{
							"name": "remote_tool",
							"description": "Remote tool",
							"inputSchema": {"type": "object"}
						}
					]
				}`),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
	}))
	defer server.Close()

	mockLLM := &mockLLMClient{}
	cfg := agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: mockLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     testMemory,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		MCPServers: []string{server.URL},
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if a == nil {
		t.Fatalf("expected Agent instance, got nil")
	}
}

func TestMCPRegistry_LocalTool(t *testing.T) {
	localTool := &MockRegistryTool{
		NameVal:   "local_tool",
		DescVal:   "Local tool",
		SchemaVal: `{"type": "object"}`,
	}

	mockLLM := &mockLLMClient{}
	cfg := agent.Config{
		Identity:   agentcontext.Identity{Name: "Bot"},
		LLMs:       agent.LLMConfig{Primary: mockLLM},
		Tokens:     quarterCounter{},
		Budget:     agentcontext.Budget{ContextTokens: 8192, OutputTokens: 512},
		Memory:     testMemory,
		IDGen:      testIDGen,
		ToolIndex:  agent.NewMemToolIndex(),
		LocalTools: []agent.Tool{localTool},
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if a == nil {
		t.Fatalf("expected Agent instance, got nil")
	}
}
