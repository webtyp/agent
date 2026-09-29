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
	// Temperature 0 makes the scenarios reproducible: the same prompt gives the same answer.
	Temperature float64 `json:"temperature"`
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
			Role:    string(m.Role),
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
		Messages:    messages,
		Tools:       tools,
		MaxTokens:   req.MaxOutputTokens,
		Stream:      false,
		Temperature: 0,
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
		Text:  choice.Message.Content,
		Usage: llm.Usage{InputTokens: chatResp.Usage.PromptTokens, OutputTokens: chatResp.Usage.CompletionTokens},
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

// clinicHours is the tool the model must discover with search_tools and call: the only source
// of the real opening hours, so an answer with "8" proves the whole tool-search path worked.
type clinicHours struct{ calls int }

func (c *clinicHours) Name() string { return "clinic_hours" }
func (c *clinicHours) Description() string {
	return "Opening hours of the clinic (horario de atención de la clínica) for each day of the week"
}
func (c *clinicHours) InputSchema() string {
	return `{"type":"object","properties":{"day":{"type":"string","description":"day of the week"}}}`
}
func (c *clinicHours) Execute(ctx *context.Context, argsJSON string) (string, error) {
	c.calls++
	return "Lunes a viernes: 8:00 a 20:00. Sábado: 9:00 a 14:00. Domingo: cerrado.", nil
}

func llamaServerAvailable() bool {
	resp, err := http.Get(llamaServerURL + llamaHealthPath)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// TestIntegration_ClinicHours is the end-to-end acceptance of tool search with a real small model:
// the answer must come from clinic_hours, which the model can only reach through search_tools.
// Measured with Qwen3.5-0.8B (llama-server, 2026-09-29): the model calls search_tools, the index
// finds clinic_hours, and the model then answers from memory instead of calling it (each tool hop
// succeeds ~50–65% of the time). This test fails until the agent removes a hop for small models;
// see the open decision "runtime tool pre-retrieval" in docs/AGENT_ECOSYSTEM_MASTER_PLAN.md.
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

	hours := &clinicHours{}
	client := newLlamaServer()

	cfg := Config{
		Identity: agentcontext.Identity{
			Name: "Recepcionista",
			Role: "Recepcionista de Clínica San Miguel",
			// A small model answers simple questions from memory unless told not to: without
			// this sentence Qwen3.5-0.8B called search_tools 0 times in 8, with it 4 in 8
			// (temperature 0.7) and always at temperature 0.
			Instructions: "Responde siempre en español, de forma concisa. Nunca inventes datos de la clínica (horarios, precios, citas): consíguelos siempre con una herramienta. Si no tienes la herramienta adecuada, llama primero a search_tools.",
			Goals:        []string{"Informar horarios", "Gestionar citas"},
		},
		LLMs: LLMConfig{
			Primary: client,
		},
		Tokens:     client,
		Budget:     agentcontext.Budget{ContextTokens: 4096, OutputTokens: 512},
		Memory:     mem,
		IDGen:      idGen,
		ToolIndex:  NewMemToolIndex(),
		LocalTools: []Tool{hours},
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
	if logs, err := mem.GetToolLogs(ctx, sessionID, "", 20); err == nil {
		for _, l := range logs {
			t.Logf("tool %s(%s) -> %q %s", l.ToolName, l.InputJSON, l.OutputText, l.ErrText)
		}
	}

	if hours.calls == 0 {
		t.Errorf("the model never called clinic_hours (it must discover it with search_tools); answer: %s", answer)
	}
	if !strings.Contains(answer, "8") {
		t.Errorf("expected the real opening hour 8 in the answer, got: %s", answer)
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

	agent, err := New(Config{
		Identity:  agentcontext.Identity{Name: "Bot", Role: "Bot", Instructions: "Be helpful."},
		LLMs:      LLMConfig{Primary: client},
		Tokens:    client,
		Budget:    agentcontext.Budget{ContextTokens: 4096, OutputTokens: 512},
		Memory:    mem,
		IDGen:     idGen,
		ToolIndex: NewMemToolIndex(),
	})
	if err != nil {
		t.Fatalf("New agent failed: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		agent.Run(ctx, "sessionA", "Remember my secret code: ALPHA-7431")
	}()

	go func() {
		defer wg.Done()
		agent.Run(ctx, "sessionB", "Remember my secret code: BRAVO-2210")
	}()

	wg.Wait()

	turnsA, _ := mem.GetTurns(ctx, "sessionA", 100)
	for _, tr := range turnsA {
		if strings.Contains(tr.Message.Content, "BRAVO-2210") {
			t.Errorf("Session A contains info from Session B: %s", tr.Message.Content)
		}
	}

	turnsB, _ := mem.GetTurns(ctx, "sessionB", 100)
	for _, tr := range turnsB {
		if strings.Contains(tr.Message.Content, "ALPHA-7431") {
			t.Errorf("Session B contains info from Session A: %s", tr.Message.Content)
		}
	}
}
