package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"webtyp.com/fmt"
)

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"` // always "2.0"
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type listToolsResult struct {
	Tools []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	} `json:"tools"`
}

type callToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

// HTTPMCPClient handles communication with an MCP server via JSON-RPC 2.0 over HTTP.
type HTTPMCPClient struct {
	BaseURL string
	Client  *http.Client
}

func NewHTTPMCPClient(url string) *HTTPMCPClient {
	return &HTTPMCPClient{
		BaseURL: url,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *HTTPMCPClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      time.Now().UnixNano(),
		Method:  method,
		Params:  params,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errf("failed to decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}
