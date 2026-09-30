package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/llm"
)

// Run executes the full ReAct + Reflection loop for a single user query.
func (a *Agent) Run(ctx *context.Context, sessionID, userQuery string) (string, error) {
	// Initialize: Ensure session exists
	if err := a.mem.EnsureSession(ctx, sessionID); err != nil {
		return "", fmt.Errf("failed to ensure session: %w", err)
	}

	// Append user turn
	userTurn := a.newTurn(llm.Message{
		Role:    llm.RoleUser,
		Content: userQuery,
	})
	if err := a.mem.AppendTurn(ctx, sessionID, userTurn); err != nil {
		return "", fmt.Errf("failed to append user turn: %w", err)
	}

	// Tool search: initialize offered tools per Run
	offered := []llm.ToolDef{searchToolsDef}

	// Loop
	iterations := 0
	actingFailures := 0

	// Reset FSM
	a.fsm.current = StateIdle
	if err := a.fsm.transition(StateReasoning); err != nil {
		return "", err
	}

	for iterations < a.cfg.MaxIterations {
		iterations++

		// Build Request
		req, err := a.request(ctx, sessionID, offered)
		if err != nil {
			return "", fmt.Errf("failed to prepare context: %w", err)
		}

		if a.fsm.current != StateReasoning {
			if err := a.fsm.transition(StateReasoning); err != nil {
				return "", err
			}
		}

		resp, err := a.llms.Primary.Generate(ctx, req)
		if err != nil {
			return "", fmt.Errf("LLM generation failed: %w", err)
		}

		if resp.StopReason == llm.StopMaxTokens {
			if err := a.fsm.transition(StateResponding); err != nil {
				return "", err
			}
			if err := a.fsm.transition(StateIdle); err != nil {
				return "", err
			}
			return "", fmt.Errf(errOutputTruncated)
		}

		// Acting
		if resp.StopReason == llm.StopToolUse {
			if err := a.fsm.transition(StateActing); err != nil {
				return "", err
			}

			// Append assistant turn with tool calls
			assistantTurn := a.newTurn(llm.Message{
				Role:      llm.RoleAssistant,
				Content:   resp.Text,
				ToolCalls: resp.ToolCalls,
			})

			if err := a.mem.AppendTurn(ctx, sessionID, assistantTurn); err != nil {
				return "", fmt.Errf("failed to save assistant turn: %w", err)
			}

			// Execute tools
			for _, call := range resp.ToolCalls {
				startTime := a.cfg.Clock.Now()

				var output string
				var execErr error

				if call.Name == searchToolsName {
					output = a.searchTools(ctx, sessionID, call, &offered)
				} else {
					isOffered := false
					for _, o := range offered {
						if o.Name == call.Name {
							isOffered = true
							break
						}
					}

					if !isOffered {
						execErr = fmt.Errf("tool %s is not available; call search_tools first", call.Name)
						output = fmt.Sprintf("Error: %s", execErr.Error())
					} else {
						output, execErr = a.registry.execute(ctx, call.Name, call.Input)
					}
				}

				duration := (a.cfg.Clock.Now() - startTime) / 1e6

				var errText string
				if execErr != nil {
					errText = execErr.Error()
					actingFailures++
				} else {
					actingFailures = 0
				}

				// Log tool execution
				if logErr := a.mem.LogToolCall(ctx, sessionID, call.Name, call.Input, output, errText, duration); logErr != nil {
					fmt.Printf("failed to log tool call: %v\n", logErr)
				}

				// Append tool result message
				toolMsg := llm.Message{
					Role:       llm.RoleTool,
					Content:    output,
					ToolName:   call.Name,
					ToolCallID: call.ID,
				}
				if execErr != nil && !fmt.Contains(output, "Error:") {
					toolMsg.Content = fmt.Sprintf("Error: %s", execErr.Error())
				}

				toolTurn := a.newTurn(toolMsg)
				if err := a.mem.AppendTurn(ctx, sessionID, toolTurn); err != nil {
					return "", fmt.Errf("failed to save tool turn: %w", err)
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
		if resp.StopReason == llm.StopEndTurn {
			if err := a.fsm.transition(StateReflecting); err != nil {
				return "", err
			}

			candidateTurn := a.newTurn(llm.Message{
				Role:    llm.RoleAssistant,
				Content: resp.Text,
			})
			if err := a.mem.AppendTurn(ctx, sessionID, candidateTurn); err != nil {
				return "", fmt.Errf("failed to save candidate turn: %w", err)
			}

			reflector := a.cfg.LLMs.Reflector
			if reflector == nil {
				reflector = a.cfg.LLMs.Primary
			}

			reflectionPrompt := fmt.Sprintf(reflectionPromptFormat, userQuery, resp.Text)

			reflectReq := llm.Request{
				System: reflectorSystem,
				Messages: []llm.Message{
					{Role: llm.RoleUser, Content: reflectionPrompt},
				},
				MaxOutputTokens: reflectionOutputTokens,
			}

			reflectResp, err := reflector.Generate(ctx, reflectReq)
			if err != nil {
				fmt.Printf("Reflection failed: %v\n", err)
			} else {
				isSufficient := reflectResp.Text == "SUFFICIENT" || reflectResp.Text == "SUFFICIENT." || (fmt.Contains(reflectResp.Text, "SUFFICIENT") && !fmt.Contains(reflectResp.Text, "INSUFFICIENT"))

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

					feedbackTurn := a.newTurn(llm.Message{
						Role:    llm.RoleUser,
						Content: fmt.Sprintf("Reflection feedback: %s. Please improve the answer.", critique),
					})
					if err := a.mem.AppendTurn(ctx, sessionID, feedbackTurn); err != nil {
						return "", fmt.Errf("failed to save feedback turn: %w", err)
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

		return "", fmt.Errf("agent: unknown stop reason %q", resp.StopReason)
	}

	return "", fmt.Errf("max iterations reached")
}
