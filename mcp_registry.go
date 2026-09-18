package agent

import (
	"context"
	"encoding/json"
	"sync"

	"webtyp.com/fmt"
)

// mcpCaller abstracts the JSON-RPC transport for MCP protocol calls.
// HTTPMCPClient implements this interface; MockMCPClient implements it in tests.
type mcpCaller interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
}

type mcpToolEntry struct {
	Client mcpCaller
	Def    ToolDef
}

type mcpRegistry struct {
	localTools map[string]Tool
	mcpTools   map[string]mcpToolEntry
	mu         sync.RWMutex
}

func newMCPRegistry() *mcpRegistry {
	return &mcpRegistry{
		localTools: make(map[string]Tool),
		mcpTools:   make(map[string]mcpToolEntry),
	}
}

func (r *mcpRegistry) addLocalTool(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.localTools[t.Name()] = t
}

func (r *mcpRegistry) addMCPServer(ctx context.Context, server MCPServer) error {
	client := NewHTTPMCPClient(server.URL())
	return r.addMCPClient(ctx, client)
}

func (r *mcpRegistry) addMCPClient(ctx context.Context, client mcpCaller) error {
	resRaw, err := client.Call(ctx, "tools/list", nil)
	if err != nil {
		return fmt.Errf("tools/list failed: %w", err)
	}

	// It seems listToolsResult structure:
	// type listToolsResult struct {
	// 	Tools []struct { ... } `json:"tools"`
	// }
	// But `Result` is json.RawMessage. So we need to unmarshal it into listToolsResult.

	var list listToolsResult
	if err := json.Unmarshal(resRaw, &list); err != nil {
		return fmt.Errf("failed to unmarshal tools list: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range list.Tools {
		r.mcpTools[t.Name] = mcpToolEntry{
			Client: client,
			Def: ToolDef{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: string(t.InputSchema),
			},
		}
	}
	return nil
}

func (r *mcpRegistry) getTools() []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tools []ToolDef

	for _, t := range r.localTools {
		tools = append(tools, ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}

	for _, t := range r.mcpTools {
		tools = append(tools, t.Def)
	}

	return tools
}

func (r *mcpRegistry) execute(ctx context.Context, name string, argsJSON string) (string, error) {
	r.mu.RLock()
	local, hasLocal := r.localTools[name]
	mcpEntry, hasMCP := r.mcpTools[name]
	r.mu.RUnlock()

	if hasLocal {
		return local.Execute(ctx, argsJSON)
	}
	if hasMCP {
		// Call MCP tool
		var args map[string]any
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errf("invalid args JSON: %w", err)
		}

		params := map[string]any{
			"name":      name,
			"arguments": args,
		}

		resRaw, err := mcpEntry.Client.Call(ctx, "tools/call", params)
		if err != nil {
			return "", err
		}

		var result callToolResult
		if err := json.Unmarshal(resRaw, &result); err != nil {
			return "", fmt.Errf("failed to unmarshal tool result: %w", err)
		}

		if result.IsError {
			// Try to find error text in content if any
			sb := fmt.Convert("")
			for _, c := range result.Content {
				if c.Type == "text" {
					sb.WrString(fmt.BuffOut, c.Text)
				}
			}
			errMsg := sb.GetString(fmt.BuffOut)
			if errMsg == "" {
				errMsg = "unknown tool error"
			}
			return "", fmt.Errf("tool execution failed: %s", errMsg)
		}

		// Aggregate content text
		sb := fmt.Convert("")
		for _, c := range result.Content {
			if c.Type == "text" {
				sb.WrString(fmt.BuffOut, c.Text)
			}
		}
		return sb.GetString(fmt.BuffOut), nil
	}

	return "", fmt.Errf("tool not found: %s", name)
}
