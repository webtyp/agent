package agent_test

import (
	"testing"

	"webtyp.com/agent"
	"webtyp.com/agent/conformance"
	"webtyp.com/unixid"
)

func TestMemMemoryConformance(t *testing.T) {
	conformance.Run(t, conformance.Factory{
		Name: "mem",
		New: func(t *testing.T) agent.MemoryStore {
			idGen, err := unixid.NewUnixID()
			if err != nil {
				t.Fatalf("unixid.NewUnixID: %v", err)
			}
			return agent.NewMemMemory(idGen)
		},
	})
}
