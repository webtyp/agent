package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
)

// New creates a new Agent instance.
func New(cfg Config) (*Agent, error) {
	if cfg.LLMs.Primary == nil {
		return nil, fmt.Errf("agent: LLMs.Primary is required")
	}
	if cfg.Memory == nil {
		return nil, fmt.Errf("agent: Memory is required")
	}
	if cfg.IDGen == nil {
		return nil, fmt.Errf("agent: IDGen is required")
	}

	// Apply defaults
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 10
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.MCPTimeoutMS == 0 {
		cfg.MCPTimeoutMS = 30000
	}
	// Context Window defaults
	if cfg.ContextWindow.MaxTokens == 0 {
		cfg.ContextWindow.MaxTokens = 8192 // Default limit
	}
	if cfg.ContextWindow.SummarizeAt == 0 {
		cfg.ContextWindow.SummarizeAt = 0.8
	}
	if cfg.ContextWindow.MaxRecentMsgs == 0 {
		cfg.ContextWindow.MaxRecentMsgs = 20
	}
	if cfg.ContextWindow.MaxEpisodes == 0 {
		cfg.ContextWindow.MaxEpisodes = 5
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
		if err := registry.addMCPServer(ctx, handler); err != nil {
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

	return &Agent{
		cfg:      cfg,
		mem:      cfg.Memory,
		llms:     cfg.LLMs,
		registry: registry,
		fsm:      &fsm{current: StateIdle},
		idGen:    cfg.IDGen,
	}, nil
}
