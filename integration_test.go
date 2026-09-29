//go:build integration

package agent

import (
	"bytes"
	stdcontext "context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/llm"
	"webtyp.com/unixid"
)

const (
	llamaServerURL    = "http://localhost:8080"
	llamaHealthPath   = "/health"
	llamaChatPath     = "/v1/chat/completions"
	llamaTokenizePath = "/tokenize"
)

type llamaServer struct {
	client *http.Client
}

func newLlamaServer() *llamaServer {
	return &llamaServer{
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

type llamaMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []llamaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type llamaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type llamaToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type llamaChatRequest struct {
	Messages  []llamaMessage `json:"messages"`
	Tools     []llamaToolDef `json:"tools,omitempty"`
	MaxTokens int            `json:"max_tokens,omitempty"`
	Stream    bool           `json:"stream"`
}

type llamaChatResponse struct {
	Choices []struct {
		Message      llamaMessage `json:"message"`
		FinishReason string       `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type llamaTokenizeRequest struct {
	Content string `json:"content"`
}

type llamaTokenizeResponse struct {
	Tokens []any `json:"tokens"`
}

func (l *llamaServer) Generate(ctx *context.Context, req llm.Request) (llm.Response, error) {
	var messages []llamaMessage

	if req.System != "" {
		messages = append(messages, llamaMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	for _, m := range req.Messages {
		msg := llamaMessage{
			Role:    m.Role,
			Content: m.Content,
		}
		if m.Role == llm.RoleTool {
			msg.ToolCallID = m.ToolCallID
		}
		if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 {
			var tcs []llamaToolCall
			for _, tc := range m.ToolCalls {
				tcs = append(tcs, llamaToolCall{
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

	var tools []llamaToolDef
	for _, t := range req.Tools {
		tools = append(tools, llamaToolDef{
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

	bodyObj := llamaChatRequest{
		Messages:  messages,
		Tools:     tools,
		MaxTokens: req.MaxOutputTokens,
		Stream:    false,
	}

	bodyBytes, _ := json.Marshal(bodyObj)
	httpReq, _ := http.NewRequestWithContext(stdcontext.Background(), "POST", llamaServerURL+llamaChatPath, bytes.NewReader(bodyBytes))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(httpReq)
	if err != nil {
		return llm.Response{}, fmt.Errorf("llama-server request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return llm.Response{}, fmt.Errorf("llama-server error %d: %s", resp.StatusCode, string(body))
	}

	var chatResp llamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return llm.Response{}, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return llm.Response{}, fmt.Errorf("no choices returned")
	}

	choice := chatResp.Choices[0]

	llmResp := llm.Response{
		Text:       choice.Message.Content,
		TokensUsed: chatResp.Usage.PromptTokens + chatResp.Usage.CompletionTokens,
	}

	switch choice.FinishReason {
	case "tool_calls":
		llmResp.StopReason = llm.StopToolUse
	case "length":
		llmResp.StopReason = llm.StopMaxTokens
	default:
		if len(choice.Message.ToolCalls) > 0 {
			llmResp.StopReason = llm.StopToolUse
		} else {
			llmResp.StopReason = llm.StopEndTurn
		}
	}

	for _, tc := range choice.Message.ToolCalls {
		llmResp.ToolCalls = append(llmResp.ToolCalls, llm.ToolCall{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: tc.Function.Arguments,
		})
	}

	return llmResp, nil
}

func (l *llamaServer) CountTokens(text string) int {
	bodyBytes, _ := json.Marshal(llamaTokenizeRequest{Content: text})
	httpReq, err := http.NewRequestWithContext(stdcontext.Background(), "POST", llamaServerURL+llamaTokenizePath, bytes.NewReader(bodyBytes))
	if err != nil {
		return len(text) / 4
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(httpReq)
	if err != nil {
		return len(text) / 4
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return len(text) / 4
	}

	var tokResp llamaTokenizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokResp); err != nil {
		return len(text) / 4
	}

	return len(tokResp.Tokens)
}

func llamaServerAvailable() bool {
	resp, err := http.Get(llamaServerURL + llamaHealthPath)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func TestIntegration_ClinicHours(t *testing.T) {
	if !llamaServerAvailable() {
		t.Skip("llama-server not running on :8080")
	}

	sessionID := t.Name()
	ctx := context.Background()

	idGen, err := unixid.NewUnixID()
	if err != nil {
		t.Fatalf("unixid.NewUnixID: %v", err)
	}
	mem := NewMemMemory()

	client := newLlamaServer()

	cfg := Config{
		Identity: agentcontext.Identity{
			Name:         "Recepcionista",
			Role:         "Recepcionista de Clínica San Miguel",
			Instructions: "Responde siempre en español, de forma concisa.",
			Goals:        []string{"Informar horarios", "Gestionar citas"},
		},
		LLMs: LLMConfig{
			Primary: client,
		},
		Tokens: client,
		Budget: agentcontext.Budget{ContextTokens: 4096, OutputTokens: 512},
		Memory: mem,
		IDGen:  idGen,
	}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	answer, err := agent.Run(ctx, sessionID, "¿A qué hora abren los lunes?")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	t.Logf("Answer: %s", answer)

	if !strings.Contains(strings.ToLower(answer), "8") {
		t.Errorf("expected answer to contain '8', got: %s", answer)
	}
}

func TestIntegration_SessionIsolation(t *testing.T) {
	if !llamaServerAvailable() {
		t.Skip("llama-server not running on :8080")
	}

	ctx := context.Background()
	idGen, err := unixid.NewUnixID()
	if err != nil {
		t.Fatalf("unixid.NewUnixID: %v", err)
	}
	mem := NewMemMemory()
	client := newLlamaServer()

	agent, _ := New(Config{
		Identity: agentcontext.Identity{Name: "Bot", Role: "Bot", Instructions: "Be helpful."},
		LLMs:     LLMConfig{Primary: client},
		Tokens:   client,
		Budget:   agentcontext.Budget{ContextTokens: 4096, OutputTokens: 512},
		Memory:   mem,
		IDGen:    idGen,
	})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		agent.Run(ctx, "sessionA", "My secret is A")
	}()

	go func() {
		defer wg.Done()
		agent.Run(ctx, "sessionB", "My secret is B")
	}()

	wg.Wait()

	turnsA, _ := mem.GetTurns(ctx, "sessionA", 100)
	for _, tr := range turnsA {
		if strings.Contains(tr.Message.Content, "B") {
			t.Errorf("Session A contains info from Session B: %s", tr.Message.Content)
		}
	}

	turnsB, _ := mem.GetTurns(ctx, "sessionB", 100)
	for _, tr := range turnsB {
		if strings.Contains(tr.Message.Content, "A") {
			t.Errorf("Session B contains info from Session A: %s", tr.Message.Content)
		}
	}
}
