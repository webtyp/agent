package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"webtyp.com/context"
)

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

	registry := newMCPRegistry()
	client := NewHTTPMCPClient(server.URL, 30000)

	if err := registry.addMCPClient(context.Background(), client); err != nil {
		t.Fatalf("addMCPClient failed: %v", err)
	}

	tools := registry.getTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "remote_tool" {
		t.Errorf("expected tool name 'remote_tool', got '%s'", tools[0].Name)
	}
}

// MockTool implements Tool interface for testing local tools
type MockTool struct {
	NameVal     string
	DescVal     string
	SchemaVal   string
	ExecuteFunc func(ctx *context.Context, argsJSON string) (string, error)
}

func (m *MockTool) Name() string        { return m.NameVal }
func (m *MockTool) Description() string { return m.DescVal }
func (m *MockTool) InputSchema() string { return m.SchemaVal }
func (m *MockTool) Execute(ctx *context.Context, argsJSON string) (string, error) {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, argsJSON)
	}
	return "mock output", nil
}

func TestMCPRegistry_LocalTool(t *testing.T) {
	registry := newMCPRegistry()
	localTool := &MockTool{
		NameVal:   "local_tool",
		DescVal:   "Local tool",
		SchemaVal: `{"type": "object"}`,
	}
	registry.addLocalTool(localTool)

	tools := registry.getTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "local_tool" {
		t.Errorf("expected tool name 'local_tool', got '%s'", tools[0].Name)
	}

	out, err := registry.execute(context.Background(), "local_tool", "{}")
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if out != "mock output" {
		t.Errorf("expected output 'mock output', got '%s'", out)
	}
}
