package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/time"
)

// Run executes the full ReAct + Reflection loop for a single user query.
func (a *Agent) Run(ctx *context.Context, sessionID, userQuery string) (string, error) {
	// 2. Initialize: Ensure session exists
	if err := a.mem.EnsureSession(ctx, sessionID); err != nil {
		return "", fmt.Errf("failed to ensure session: %w", err)
	}

	// Append user message
	userMsg := Message{
		ID:         a.idGen.NewID(),
		SessionID:  sessionID,
		Role:       "user",
		Content:    userQuery,
		CreatedAt:  time.Now() / 1e9,
		TokenCount: 0,
	}
	if err := a.mem.AppendMessage(ctx, sessionID, userMsg); err != nil {
		return "", fmt.Errf("failed to append user message: %w", err)
	}

	// 3. Loop
	iterations := 0
	actingFailures := 0

	// Reset FSM
	a.fsm.current = StateIdle
	if err := a.fsm.transition(StateReasoning); err != nil {
		return "", err
	}

	for iterations < a.cfg.MaxIterations {
		iterations++

		// Build Context Window
		req, err := a.prepareContext(ctx, sessionID)
		if err != nil {
			return "", fmt.Errf("failed to prepare context: %w", err)
		}

		// Add tools to request
		req.Tools = a.registry.getTools()

		if a.fsm.current != StateReasoning {
			if err := a.fsm.transition(StateReasoning); err != nil {
				return "", err
			}
		}

		resp, err := a.llms.Primary.Generate(ctx, *req)
		if err != nil {
			return "", fmt.Errf("LLM generation failed: %w", err)
		}

		// Acting
		if resp.StopReason == "tool_use" {
			if err := a.fsm.transition(StateActing); err != nil {
				return "", err
			}

			// Append assistant message with tool calls
			assistantMsg := Message{
				ID:         a.idGen.NewID(),
				SessionID:  sessionID,
				Role:       "assistant",
				Content:    resp.Text,
				ToolCalls:  resp.ToolCalls,
				TokenCount: 0,
				CreatedAt:  time.Now() / 1e9,
			}

			if err := a.mem.AppendMessage(ctx, sessionID, assistantMsg); err != nil {
				return "", fmt.Errf("failed to save assistant message: %w", err)
			}

			// Execute tools
			for _, call := range resp.ToolCalls {
				startTime := time.Now()
				output, err := a.registry.execute(ctx, call.Name, call.Input)
				duration := (time.Now() - startTime) / 1e6

				var errText string
				if err != nil {
					errText = err.Error()
					actingFailures++
				} else {
					actingFailures = 0
				}

				// Log tool execution
				if logErr := a.mem.LogToolCall(ctx, sessionID, call.Name, call.Input, output, errText, duration); logErr != nil {
					fmt.Printf("failed to log tool call: %v\n", logErr)
				}

				// Append tool result message
				toolMsg := Message{
					ID:         a.idGen.NewID(),
					SessionID:  sessionID,
					Role:       "tool",
					Content:    output,
					ToolName:   call.Name,
					ToolCallID: call.ID,
					CreatedAt:  time.Now() / 1e9,
					TokenCount: 0,
				}
				if err != nil {
					toolMsg.Content = fmt.Sprintf("Error: %s", err.Error())
				}

				if err := a.mem.AppendMessage(ctx, sessionID, toolMsg); err != nil {
					return "", fmt.Errf("failed to save tool message: %w", err)
				}

				if actingFailures >= a.cfg.MaxRetries {
					if err := a.fsm.transition(StateResponding); err != nil {
						return "", err
					}
					return "Maximum tool retries reached. Please try again.", nil
				}
			}
			continue
		}

		// Reflection
		if resp.StopReason == "end_turn" {
			if err := a.fsm.transition(StateReflecting); err != nil {
				return "", err
			}

			candidateMsg := Message{
				ID:         a.idGen.NewID(),
				SessionID:  sessionID,
				Role:       "assistant",
				Content:    resp.Text,
				TokenCount: 0,
				CreatedAt:  time.Now() / 1e9,
			}
			if err := a.mem.AppendMessage(ctx, sessionID, candidateMsg); err != nil {
				return "", fmt.Errf("failed to save candidate message: %w", err)
			}

			reflector := a.cfg.LLMs.Reflector
			if reflector == nil {
				reflector = a.cfg.LLMs.Primary
			}

			reflectionPrompt := fmt.Sprintf(`
Analyze the following user query and the assistant's response.
User Query: "%s"
Assistant Response: "%s"

Is the response complete and accurate?
If YES, respond with "SUFFICIENT".
If NO, respond with "INSUFFICIENT" followed by a short critique.
`, userQuery, resp.Text)

			reflectReq := LLMRequest{
				SystemPrompt: "You are a critic that evaluates AI responses.",
				Messages: []Message{
					{Role: "user", Content: reflectionPrompt},
				},
				MaxTokens: 100,
			}

			reflectResp, err := reflector.Generate(ctx, reflectReq)
			if err != nil {
				fmt.Printf("Reflection failed: %v\n", err)
			} else {
				isSufficient := reflectResp.Text == "SUFFICIENT" || reflectResp.Text == "SUFFICIENT." || fmt.Contains(reflectResp.Text, "SUFFICIENT") && !fmt.Contains(reflectResp.Text, "INSUFFICIENT")

				if isSufficient {
					if err := a.fsm.transition(StateResponding); err != nil {
						return "", err
					}
					if err := a.fsm.transition(StateIdle); err != nil {
						return "", err
					}
					return resp.Text, nil
				} else {
					critique := reflectResp.Text

					feedbackMsg := Message{
						ID:         a.idGen.NewID(),
						SessionID:  sessionID,
						Role:       "user",
						Content:    fmt.Sprintf("Reflection feedback: %s. Please improve the answer.", critique),
						CreatedAt:  time.Now() / 1e9,
						TokenCount: 0,
					}
					if err := a.mem.AppendMessage(ctx, sessionID, feedbackMsg); err != nil {
						return "", fmt.Errf("failed to save feedback: %w", err)
					}
					continue
				}
			}

			if err := a.fsm.transition(StateResponding); err != nil {
				return "", err
			}
			if err := a.fsm.transition(StateIdle); err != nil {
				return "", err
			}
			return resp.Text, nil
		}
	}

	return "", fmt.Errf("max iterations reached")
}
