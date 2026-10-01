package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
)

// New creates and configures a new Agent instance.
func New(cfg Config) (*Agent, error) {
	if cfg.Decider == nil {
		return nil, fmt.Err(errDeciderRequired)
	}
	if cfg.Tokens == nil {
		return nil, fmt.Err(errTokensRequired)
	}
	if cfg.Memory == nil {
		return nil, fmt.Err(errMemoryRequired)
	}
	if cfg.IDGen == nil {
		return nil, fmt.Err(errIDGenRequired)
	}
	if cfg.ToolIndex == nil {
		return nil, fmt.Err(errToolIndexRequired)
	}

	if name := cfg.Texts.missingText(); name != "" {
		return nil, fmt.Errf(errTextRequired, name)
	}
	if cfg.Writer != nil {
		if name := cfg.Texts.missingWriterText(); name != "" {
			return nil, fmt.Errf(errWriterTextRequired, name)
		}
	}

	if cfg.Candidates == 0 {
		cfg.Candidates = 5
	} else if cfg.Candidates < 1 || cfg.Candidates > 9 {
		return nil, fmt.Err(errCandidatesRange)
	}

	if cfg.Guard.MaxChars < 0 {
		return nil, fmt.Err(errMaxCharsNegative)
	} else if cfg.Guard.MaxChars == 0 {
		cfg.Guard.MaxChars = DefaultMaxChars
	}

	if cfg.Clock == nil {
		cfg.Clock = MachineClock{}
	}
	if cfg.WriterMaxTokens == 0 {
		cfg.WriterMaxTokens = 128
	}
	if cfg.MCPTimeoutMS == 0 {
		cfg.MCPTimeoutMS = 30000
	}

	ctx := context.Background()

	registry := newMCPRegistry()
	for _, tool := range cfg.LocalTools {
		registry.addLocalTool(tool)
	}
	for _, handler := range cfg.MCPHandlers {
		if err := registry.addMCPServer(ctx, handler, cfg.MCPTimeoutMS); err != nil {
			return nil, fmt.Errf("failed to add MCP handler: %w", err)
		}
	}
	for _, serverURL := range cfg.MCPServers {
		if err := registry.addMCPServer(ctx, stringMCPServer(serverURL), cfg.MCPTimeoutMS); err != nil {
			return nil, fmt.Errf("failed to add MCP server: %w", err)
		}
	}

	tools := registry.getTools()
	for _, t := range cfg.Templates {
		if t.Answer == nil {
			return nil, fmt.Errf(errTemplateNoAnswer, t.Tool)
		}
		found := false
		for _, tool := range tools {
			if tool.Name == t.Tool {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errf(errTemplateUnknownTool, t.Tool)
		}
	}

	if err := cfg.ToolIndex.IndexTools(ctx, tools); err != nil {
		return nil, fmt.Errf("failed to index tools: %w", err)
	}

	return &Agent{
		cfg:      cfg,
		mem:      cfg.Memory,
		registry: registry,
		idGen:    cfg.IDGen,
	}, nil
}

type stringMCPServer string

func (s stringMCPServer) URL() string { return string(s) }
