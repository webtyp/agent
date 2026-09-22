package agent

import (
	"webtyp.com/context"
)

// MockLLMClient is a mock implementation of LLMClient.
type MockLLMClient struct {
	GenerateFunc func(ctx *context.Context, req LLMRequest) (LLMResponse, error)
}

func (m *MockLLMClient) Generate(ctx *context.Context, req LLMRequest) (LLMResponse, error) {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, req)
	}
	return LLMResponse{}, nil
}
