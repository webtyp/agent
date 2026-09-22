package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"webtyp.com/context"
	"webtyp.com/fmt"
	webtypjson "webtyp.com/json"
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

	client := NewHTTPMCPClient(server.URL, 30000)
	res, err := client.Call(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var list listToolsResult
	if err := webtypjson.Decode(res, &list); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if len(list.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(list.Tools))
	}
	if list.Tools[0].Name != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got '%s'", list.Tools[0].Name)
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
			var resultBytes []byte
			webtypjson.Encode(mcp.Text("tool output"), &resultBytes)
			resp := jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  resultBytes,
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "unknown method", http.StatusNotFound)
	}))
	defer server.Close()

	client := NewHTTPMCPClient(server.URL, 30000)
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
