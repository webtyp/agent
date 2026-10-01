package tests

import (
	"strings"
	"testing"

	"webtyp.com/agent"
)

func defaultValidConfig() agent.Config {
	return agent.Config{
		Decider:   &scriptedDecider{},
		Texts:     cote(),
		Tokens:    quarterCounter{},
		Memory:    agent.NewMemMemory(),
		IDGen:     testIDGen,
		ToolIndex: agent.NewMemToolIndex(),
	}
}

// TestNew_Validation verifies that New fails when required fields are missing or invalid.
func TestNew_Validation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *agent.Config)
		wantErr string
	}{
		{
			name: "missing Decider",
			mutate: func(c *agent.Config) {
				c.Decider = nil
			},
			wantErr: "agent: Decider is required",
		},
		{
			name: "missing Tokens",
			mutate: func(c *agent.Config) {
				c.Tokens = nil
			},
			wantErr: "agent: Tokens is required",
		},
		{
			name: "missing Memory",
			mutate: func(c *agent.Config) {
				c.Memory = nil
			},
			wantErr: "agent: Memory is required",
		},
		{
			name: "missing IDGen",
			mutate: func(c *agent.Config) {
				c.IDGen = nil
			},
			wantErr: "agent: IDGen is required",
		},
		{
			name: "missing ToolIndex",
			mutate: func(c *agent.Config) {
				c.ToolIndex = nil
			},
			wantErr: "agent: ToolIndex is required",
		},
		{
			name: "missing Texts.Refused",
			mutate: func(c *agent.Config) {
				c.Texts.Refused = ""
			},
			wantErr: "agent: Texts.Refused is required",
		},
		{
			name: "Writer set and missing Texts.DataLabel",
			mutate: func(c *agent.Config) {
				c.Writer = &recordingWriter{}
				c.Texts.DataLabel = ""
			},
			wantErr: "agent: Texts.DataLabel is required when Writer is set",
		},
		{
			name: "Candidates out of range",
			mutate: func(c *agent.Config) {
				c.Candidates = 10
			},
			wantErr: "agent: Candidates must be between 1 and 9",
		},
		{
			name: "MaxChars negative",
			mutate: func(c *agent.Config) {
				c.Guard.MaxChars = -1
			},
			wantErr: "agent: Guard.MaxChars must not be negative",
		},
		{
			name: "Template unknown tool",
			mutate: func(c *agent.Config) {
				c.Templates = []agent.Template{
					{
						Tool: "nope",
						Answer: func(d agent.Data) (string, bool) {
							return "ans", true
						},
					},
				}
			},
			wantErr: "agent: Templates: no tool named nope",
		},
		{
			name: "Template no Answer",
			mutate: func(c *agent.Config) {
				c.LocalTools = []agent.Tool{&fakeTool{name: "hours"}}
				c.Templates = []agent.Template{
					{
						Tool:   "hours",
						Answer: nil,
					},
				}
			},
			wantErr: "agent: Templates: hours has no Answer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultValidConfig()
			tt.mutate(&cfg)
			_, err := agent.New(cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}
