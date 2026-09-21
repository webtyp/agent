package agent_test

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/agent/conformance"
)

func TestMemMemoryConformance(t *testing.T) {
	conformance.Run(t, conformance.Factory{
		Name: "mem",
		New:  func(t *testing.T) agent.MemoryStore { return agent.NewMemMemory() },
	})
}
