//go:build integration

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"webtyp.com/unixid"
)

// Inline OllamaClient implementation for integration tests.
type OllamaClient struct {
	baseURL string
	model   string
	client  *http.Client
}

func NewOllamaClient(model string) *OllamaClient {
	return &OllamaClient{
		baseURL: "http://localhost:11434",
		model:   model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// Wire types for Ollama (OpenAI-compatible)
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}
type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // always "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}
type openAIToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}
type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Tools    []openAIToolDef `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
}
type openAIResponse struct {
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"` // "stop"|"tool_calls"
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *OllamaClient) Generate(ctx context.Context, req LLMRequest) (LLMResponse, error) {
	// Convert LLMRequest to OpenAI format
	var messages []openAIMessage

	// System prompt as first message
	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{
			Role:    "system",
			Content: req.SystemPrompt,
		})
	}

	for _, m := range req.Messages {
		msg := openAIMessage{
			Role:    m.Role,
			Content: m.Content,
		}
		if m.Role == "tool" {
			msg.ToolCallID = m.ToolCallID
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			var tcs []openAIToolCall
			for _, tc := range m.ToolCalls {
				tcs = append(tcs, openAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Name,
						Arguments: tc.Input,
					},
				})
			}
			msg.ToolCalls = tcs
		}

		messages = append(messages, msg)
	}

	// Tools
	var tools []openAIToolDef
	for _, t := range req.Tools {
		tools = append(tools, openAIToolDef{
			Type: "function",
			Function: struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			}{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(t.InputSchema),
			},
		})
	}

	oaiReq := openAIRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}

	bodyBytes, _ := json.Marshal(oaiReq)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(bodyBytes))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return LLMResponse{}, fmt.Errorf("ollama error %d: %s", resp.StatusCode, string(body))
	}

	var oaiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return LLMResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return LLMResponse{}, fmt.Errorf("no choices returned")
	}

	choice := oaiResp.Choices[0]

	// Map back to LLMResponse
	llmResp := LLMResponse{
		Text:       choice.Message.Content,
		TokensUsed: oaiResp.Usage.PromptTokens + oaiResp.Usage.CompletionTokens,
	}

	if choice.FinishReason == "tool_calls" || len(choice.Message.ToolCalls) > 0 {
		llmResp.StopReason = "tool_use"
		for _, tc := range choice.Message.ToolCalls {
			llmResp.ToolCalls = append(llmResp.ToolCalls, ToolCall{
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: tc.Function.Arguments,
			})
		}
	} else {
		llmResp.StopReason = "end_turn"
	}

	return llmResp, nil
}

func ollamaAvailable() bool {
	resp, err := http.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func TestIntegration_ClinicHours(t *testing.T) {
	if !ollamaAvailable() {
		t.Skip("Ollama not available")
	}

	sessionID := t.Name()
	ctx := context.Background()

	// Use Qwen 2.5 7B as per doc, but fallback if not pulled?
	// User needs to pull it. "Skipped automatically if not running."
	// We assume user pulled it or we fail?
	// The instructions say "Required only ... Skipped automatically if not running".
	// But doesn't say skipped if model missing.

	model := "qwen2.5:7b"

	// Setup Agent
	// Memory
	idGen, err := unixid.NewUnixID()
	if err != nil {
		t.Fatalf("unixid.NewUnixID: %v", err)
	}
	mem := NewMemMemory(idGen)

	client := NewOllamaClient(model)

	cfg := Config{
		Identity: IdentityConfig{
			Name:         "Recepcionista",
			Role:         "Recepcionista de Clínica San Miguel",
			Instructions: "Responde siempre en español, de forma concisa.",
			Goals:        []string{"Informar horarios", "Gestionar citas"},
		},
		LLMs: LLMConfig{
			Primary: client,
		},
		Memory: mem,
	}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	// Run
	answer, err := agent.Run(ctx, sessionID, "¿A qué hora abren los lunes?")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	t.Logf("Answer: %s", answer)

	if !strings.Contains(strings.ToLower(answer), "8") {
		// "Horario: Lunes a Viernes 8h-20h"
		// Expect 8 to be mentioned.
		t.Errorf("expected answer to contain '8', got: %s", answer)
	}
}

func TestIntegration_SessionIsolation(t *testing.T) {
	if !ollamaAvailable() {
		t.Skip("Ollama not available")
	}

	ctx := context.Background()
	idGen, err := unixid.NewUnixID()
	if err != nil {
		t.Fatalf("unixid.NewUnixID: %v", err)
	}
	mem := NewMemMemory(idGen)
	client := NewOllamaClient("qwen2.5:7b")

	agent, _ := New(Config{
		Identity: IdentityConfig{Name: "Bot", Role: "Bot", Instructions: "Be helpful."},
		LLMs: LLMConfig{Primary: client},
		Memory: mem,
	})

	var wg sync.WaitGroup
	wg.Add(2)

	// Session A
	go func() {
		defer wg.Done()
		agent.Run(ctx, "sessionA", "My secret is A")
	}()

	// Session B
	go func() {
		defer wg.Done()
		agent.Run(ctx, "sessionB", "My secret is B")
	}()

	wg.Wait()

	// Verify memory isolation
	msgsA, _ := mem.GetMessages(ctx, "sessionA", 100)
	for _, m := range msgsA {
		if strings.Contains(m.Content, "B") {
			t.Errorf("Session A contains info from Session B: %s", m.Content)
		}
	}

	msgsB, _ := mem.GetMessages(ctx, "sessionB", 100)
	for _, m := range msgsB {
		if strings.Contains(m.Content, "A") {
			t.Errorf("Session B contains info from Session A: %s", m.Content)
		}
	}
}
