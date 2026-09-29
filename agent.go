package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
)

// New creates a new Agent instance.
func New(cfg Config) (*Agent, error) {
	if cfg.LLMs.Primary == nil {
		return nil, fmt.Errf(errPrimaryRequired)
	}
	if cfg.Tokens == nil {
		return nil, fmt.Errf(errTokensRequired)
	}
	if cfg.Memory == nil {
		return nil, fmt.Errf(errMemoryRequired)
	}
	if cfg.IDGen == nil {
		return nil, fmt.Errf(errIDGenRequired)
	}
	if cfg.ToolIndex == nil {
		return nil, fmt.Errf(errToolIndexRequired)
	}
	if err := cfg.Budget.Validate(); err != nil {
		return nil, fmt.Errf("agent: Budget: %w", err)
	}

	// Apply defaults
	if cfg.ToolSearchLimit == 0 {
		cfg.ToolSearchLimit = 5
	}
	if cfg.RecentTurns == 0 {
		cfg.RecentTurns = 20
	}
	if cfg.RecentSummaries == 0 {
		cfg.RecentSummaries = 5
	}
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 10
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.MCPTimeoutMS == 0 {
		cfg.MCPTimeoutMS = 30000
	}

	// Initialize Registry
	registry := newMCPRegistry()

	// Add local tools
	for _, t := range cfg.LocalTools {
		registry.addLocalTool(t)
	}

	// Add MCP handlers (programmatic servers)
	// We need a context for initialization (listing tools)
	ctx := context.Background()

	for _, handler := range cfg.MCPHandlers {
		if err := registry.addMCPServer(ctx, handler, cfg.MCPTimeoutMS); err != nil {
			return nil, fmt.Errf("agent: failed to add MCP handler %s: %w", handler.URL(), err)
		}
	}

	// Connect to remote MCP servers
	for _, url := range cfg.MCPServers {
		client := NewHTTPMCPClient(url, cfg.MCPTimeoutMS)
		if err := registry.addMCPClient(ctx, client); err != nil {
			return nil, fmt.Errf("agent: failed to connect to MCP server %s: %w", url, err)
		}
	}

	// Check reserved tool name search_tools
	tools := registry.getTools()
	for _, t := range tools {
		if t.Name == searchToolsName {
			return nil, fmt.Errf(errSearchToolsName)
		}
	}

	// Index registered tools
	if err := cfg.ToolIndex.IndexTools(ctx, tools); err != nil {
		return nil, fmt.Errf("agent: indexing tools: %w", err)
	}

	return &Agent{
		cfg:      cfg,
		mem:      cfg.Memory,
		llms:     cfg.LLMs,
		registry: registry,
		fsm:      &fsm{current: StateIdle},
		idGen:    cfg.IDGen,
	}, nil
}
