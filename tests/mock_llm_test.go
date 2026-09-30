package tests

import (
	"webtyp.com/context"
	"webtyp.com/llm"
)

type quarterCounter struct{}

func (q quarterCounter) CountTokens(s string) int {
	return len(s) / 4
}

type mockLLM struct {
	responses []llm.Response
	requests  []llm.Request
	calls     int
}

func newMockLLM(responses ...llm.Response) *mockLLM {
	return &mockLLM{responses: responses}
}

func (m *mockLLM) Generate(ctx *context.Context, req llm.Request) (llm.Response, error) {
	m.requests = append(m.requests, req)
	if m.calls < len(m.responses) {
		resp := m.responses[m.calls]
		m.calls++
		return resp, nil
	}
	return llm.Response{StopReason: llm.StopEndTurn, Text: "Default mock response"}, nil
}
