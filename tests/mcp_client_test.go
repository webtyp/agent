package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"webtyp.com/agent"
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/mcp"
)

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
}

func TestMCPClient_Discovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req jsonRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
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
							"name": "test_tool",
							"description": "A test tool",
							"inputSchema": {"type": "object"}
						}
					]
				}`),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "unknown method", http.StatusNotFound)
	}))
	defer server.Close()

	client := agent.NewHTTPMCPClient(server.URL, 30000)
	res, err := client.Call(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if !fmt.Contains(string(res), "test_tool") {
		t.Errorf("expected response to contain 'test_tool', got '%s'", string(res))
	}
}

func TestMCPClient_CallTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if req.Method == "tools/call" {
			resp := jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"content":[{"type":"text","text":"tool output"}]}`),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "unknown method", http.StatusNotFound)
	}))
	defer server.Close()

	client := agent.NewHTTPMCPClient(server.URL, 30000)
	res, err := client.Call(context.Background(), "tools/call", &mcp.CallToolParams{
		Name:      "test_tool",
		Arguments: "{}",
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	result, err := mcp.ParseResult(res)
	if err != nil {
		t.Fatalf("ParseResult failed: %v", err)
	}

	if !fmt.Contains(result.Content, "tool output") {
		t.Errorf("expected output to contain 'tool output', got '%s'", result.Content)
	}
}
