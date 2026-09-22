package agent

import (
	"sync"

	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/json"
	"webtyp.com/mcp"
)

// mcpCaller abstracts the JSON-RPC transport for MCP protocol calls.
// HTTPMCPClient implements this interface; MockMCPClient implements it in tests.
type mcpCaller interface {
	Call(ctx *context.Context, method string, params any) ([]byte, error)
}

type mcpToolEntry struct {
	Client mcpCaller
	Def    ToolDef
}

type mcpRegistry struct {
	localTools []Tool
	mcpTools   []mcpToolEntry
	mu         sync.RWMutex
}

func newMCPRegistry() *mcpRegistry {
	return &mcpRegistry{}
}

func (r *mcpRegistry) addLocalTool(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.localTools = append(r.localTools, t)
}

func (r *mcpRegistry) addMCPServer(ctx *context.Context, server MCPServer) error {
	client := NewHTTPMCPClient(server.URL(), 30000)
	return r.addMCPClient(ctx, client)
}

func (r *mcpRegistry) addMCPClient(ctx *context.Context, client mcpCaller) error {
	resRaw, err := client.Call(ctx, "tools/list", nil)
	if err != nil {
		return fmt.Errf("tools/list failed: %w", err)
	}

	var list listToolsResult
	if err := json.Decode(resRaw, &list); err != nil {
		return fmt.Errf("failed to unmarshal tools list: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range list.Tools {
		r.mcpTools = append(r.mcpTools, mcpToolEntry{
			Client: client,
			Def: ToolDef{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			},
		})
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

func (r *mcpRegistry) execute(ctx *context.Context, name string, argsJSON string) (string, error) {
	r.mu.RLock()
	var local Tool
	for _, t := range r.localTools {
		if t.Name() == name {
			local = t
			break
		}
	}

	var mcpEntry mcpToolEntry
	var hasMCP bool
	if local == nil {
		for _, entry := range r.mcpTools {
			if entry.Def.Name == name {
				mcpEntry = entry
				hasMCP = true
				break
			}
		}
	}
	r.mu.RUnlock()

	if local != nil {
		return local.Execute(ctx, argsJSON)
	}

	if hasMCP {
		params := &mcp.CallToolParams{
			Name:      name,
			Arguments: argsJSON,
		}

		resRaw, err := mcpEntry.Client.Call(ctx, "tools/call", params)
		if err != nil {
			return "", err
		}

		res, err := mcp.ParseResult(resRaw)
		if err != nil {
			return "", fmt.Errf("failed to parse tool result: %w", err)
		}

		if res.IsError {
			errMsg := res.Content
			if errMsg == "" {
				errMsg = "unknown tool error"
			}
			return "", fmt.Errf("tool execution failed: %s", errMsg)
		}

		return res.Content, nil
	}

	return "", fmt.Errf("tool not found: %s", name)
}
