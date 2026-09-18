package agent

import (
	"context"
	"time"

	"github.com/google/uuid"
	"webtyp.com/fmt"
)

// Run executes the full ReAct + Reflection loop for a single user query.
func (a *Agent) Run(ctx context.Context, sessionID, userQuery string) (string, error) {
	// 2. Initialize: Ensure session exists
	if err := a.mem.EnsureSession(ctx, sessionID); err != nil {
		return "", fmt.Errf("failed to ensure session: %w", err)
	}

	// Append user message
	userMsg := Message{
		ID:         uuid.New().String(),
		SessionID:  sessionID,
		Role:       "user",
		Content:    userQuery,
		CreatedAt:  time.Now().Unix(),
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

		// Check context for cancellation
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		// Build Context Window
		req, err := a.prepareContext(ctx, sessionID)
		if err != nil {
			return "", fmt.Errf("failed to prepare context: %w", err)
		}

		// Add tools to request
		req.Tools = a.registry.getTools()

		// Reasoning
		// State is Reasoning (already transitioned or loops back)
		// Wait, if we came from Acting or Reflecting, we need to transition to Reasoning?
		// FSM transitions: Acting -> Reasoning, Reflecting -> Reasoning.

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
				ID:         uuid.New().String(),
				SessionID:  sessionID,
				Role:       "assistant",
				Content:    resp.Text, // Might be empty or thought process
				ToolCalls:  resp.ToolCalls,
				TokenCount: 0,
				CreatedAt:  time.Now().Unix(),
			}
			// We need to store tool calls in DB? Message struct has ToolName/ToolCallID but strictly for tool results or tool calls?
			// The schema has `tool_name`, `tool_call_id`. Usually for `role="tool"`.
			// For `role="assistant"`, usually content includes tool calls or specific format.
			// `IMPLEMENTATION.md` doesn't specify storing tool calls in separate fields for assistant message.
			// But modern LLMs (OpenAI) have `tool_calls` field.
			// Our `Message` struct has `Content` string.
			// If `resp.ToolCalls` is present, we should probably serialize them into Content or handle them.
			// But `LLMClient` adapter handles wire format.
			// For memory, we store what we sent/received.
			// If `resp.Text` is empty but `resp.ToolCalls` is not, we need to represent it.

			// Let's assume `resp.Text` contains the thought process.
			// And we append tool results as `role="tool"`.

			if err := a.mem.AppendMessage(ctx, sessionID, assistantMsg); err != nil {
				return "", fmt.Errf("failed to save assistant message: %w", err)
			}

			// Execute tools
			for _, call := range resp.ToolCalls {
				startTime := time.Now()
				output, err := a.registry.execute(ctx, call.Name, call.Input)
				duration := time.Since(startTime).Milliseconds()

				var errText string
				if err != nil {
					errText = err.Error()
					// On failure: inject error as observation immediately
					// Increment actingFailures
					actingFailures++
				} else {
					actingFailures = 0 // Reset on success? Or only consecutive? "max consecutive Acting failures"
				}

				// Log tool execution
				if logErr := a.mem.LogToolCall(ctx, sessionID, call.Name, call.Input, output, errText, duration); logErr != nil {
					// Log but continue?
					fmt.Printf("failed to log tool call: %v\n", logErr)
				}

				// Append tool result message
				toolMsg := Message{
					ID:         uuid.New().String(),
					SessionID:  sessionID,
					Role:       "tool",
					Content:    output,
					ToolName:   call.Name,
					ToolCallID: call.ID,
					CreatedAt:  time.Now().Unix(),
					TokenCount: 0,
				}
				if err != nil {
					toolMsg.Content = fmt.Sprintf("Error: %s", err.Error())
				}

				if err := a.mem.AppendMessage(ctx, sessionID, toolMsg); err != nil {
					return "", fmt.Errf("failed to save tool message: %w", err)
				}

				if actingFailures >= a.cfg.MaxRetries {
					// Transition to StateResponding with error
					if err := a.fsm.transition(StateResponding); err != nil {
						return "", err
					}
					// Return error or apology?
					// "transition to StateResponding with 'max tool retries reached'"
					// We should probably generate a response explaining the failure or just return.
					// Let's return the error as the answer.
					return "Maximum tool retries reached. Please try again.", nil
				}
			}
			// Back to loop (StateReasoning)
			continue
		}

		// Reflection
		if resp.StopReason == "end_turn" {
			if err := a.fsm.transition(StateReflecting); err != nil {
				return "", err
			}

			// Save candidate answer first so LLM can improve on it if rejected
			candidateMsg := Message{
				ID:         uuid.New().String(),
				SessionID:  sessionID,
				Role:       "assistant",
				Content:    resp.Text,
				TokenCount: 0,
				CreatedAt:  time.Now().Unix(),
			}
			if err := a.mem.AppendMessage(ctx, sessionID, candidateMsg); err != nil {
				return "", fmt.Errf("failed to save candidate message: %w", err)
			}

			// Select reflector
			reflector := a.cfg.LLMs.Reflector
			if reflector == nil {
				reflector = a.cfg.LLMs.Primary
			}

			// Build reflection prompt
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
				// If reflection fails, log and accept answer?
				// Fallback to accepting.
				fmt.Printf("Reflection failed: %v\n", err)
			} else {
				// Check if sufficient
				// Note: Ideally we should use strict parsing or boolean output
				isSufficient := reflectResp.Text == "SUFFICIENT" || reflectResp.Text == "SUFFICIENT." || fmt.Contains(reflectResp.Text, "SUFFICIENT") && !fmt.Contains(reflectResp.Text, "INSUFFICIENT")

				if isSufficient {
					// Finalize
					if err := a.fsm.transition(StateResponding); err != nil {
						return "", err
					}

					// Transition to Idle
					if err := a.fsm.transition(StateIdle); err != nil {
						return "", err
					}

					return resp.Text, nil
				} else {
					// INSUFFICIENT
					critique := reflectResp.Text

					// Save feedback
					feedbackMsg := Message{
						ID:         uuid.New().String(),
						SessionID:  sessionID,
						Role:       "user",
						Content:    fmt.Sprintf("Reflection feedback: %s. Please improve the answer.", critique),
						CreatedAt:  time.Now().Unix(),
						TokenCount: 0,
					}
					if err := a.mem.AppendMessage(ctx, sessionID, feedbackMsg); err != nil {
						return "", fmt.Errf("failed to save feedback: %w", err)
					}

					// Back to loop (Reasoning)
					continue
				}
			}

			// Fallback (if reflection skipped due to error)
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
