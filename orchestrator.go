package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/llm"
)

// Run executes the full ReAct + Reflection loop for a single user query.
func (a *Agent) Run(ctx *context.Context, sessionID, userQuery string) (Reply, error) {
	if err := a.mem.EnsureSession(ctx, sessionID); err != nil {
		return Reply{}, fmt.Errf("failed to ensure session: %w", err)
	}

	pendingCalls, err := a.pending(ctx, sessionID)
	if err != nil {
		return Reply{}, err
	}

	if err := a.decline(ctx, sessionID, pendingCalls); err != nil {
		return Reply{}, err
	}

	userTurn := a.newTurn(llm.Message{
		Role:    llm.RoleUser,
		Content: userQuery,
	})
	if err := a.mem.AppendTurn(ctx, sessionID, userTurn); err != nil {
		return Reply{}, fmt.Errf("failed to append user turn: %w", err)
	}

	offered, err := a.preselect(ctx, userQuery)
	if err != nil {
		return Reply{}, err
	}
	return a.loop(ctx, sessionID, userQuery, offered)
}

// Confirm executes pending tool calls that require confirmation and continues execution.
func (a *Agent) Confirm(ctx *context.Context, sessionID string) (Reply, error) {
	pendingCalls, err := a.pending(ctx, sessionID)
	if err != nil {
		return Reply{}, err
	}
	if len(pendingCalls) == 0 {
		return Reply{}, fmt.Errf(errNothingToConfirm)
	}

	// Pending calls are only ever calls to offered tools (see loop), so they are offered again here.
	offered := a.pendingToolsOffered(pendingCalls)
	for _, call := range pendingCalls {
		if _, err := a.runCall(ctx, sessionID, call, &offered); err != nil {
			return Reply{}, err
		}
	}

	userQuery, err := a.lastUserQuery(ctx, sessionID)
	if err != nil {
		return Reply{}, err
	}

	return a.loop(ctx, sessionID, userQuery, offered)
}

// Decline cancels pending tool calls and continues execution so the model can answer.
func (a *Agent) Decline(ctx *context.Context, sessionID string) (Reply, error) {
	pendingCalls, err := a.pending(ctx, sessionID)
	if err != nil {
		return Reply{}, err
	}
	if len(pendingCalls) == 0 {
		return Reply{}, fmt.Errf(errNothingToDecline)
	}

	if err := a.decline(ctx, sessionID, pendingCalls); err != nil {
		return Reply{}, err
	}

	offered := a.pendingToolsOffered(pendingCalls)

	userQuery, err := a.lastUserQuery(ctx, sessionID)
	if err != nil {
		return Reply{}, err
	}

	return a.loop(ctx, sessionID, userQuery, offered)
}

func (a *Agent) pending(ctx *context.Context, sessionID string) ([]llm.ToolCall, error) {
	turns, err := a.mem.GetTurns(ctx, sessionID, 1)
	if err != nil {
		return nil, fmt.Errf("failed to get turns: %w", err)
	}
	if len(turns) == 0 {
		return nil, nil
	}
	lastTurn := turns[len(turns)-1]
	if lastTurn.Message.Role == llm.RoleAssistant && len(lastTurn.Message.ToolCalls) > 0 {
		return lastTurn.Message.ToolCalls, nil
	}
	return nil, nil
}

func (a *Agent) lastUserQuery(ctx *context.Context, sessionID string) (string, error) {
	turns, err := a.mem.GetTurns(ctx, sessionID, a.cfg.RecentTurns)
	if err != nil {
		return "", fmt.Errf("failed to get turns: %w", err)
	}
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Message.Role == llm.RoleUser {
			return turns[i].Message.Content, nil
		}
	}
	return "", nil
}

func (a *Agent) pendingToolsOffered(pending []llm.ToolCall) []llm.ToolDef {
	offered := []llm.ToolDef{searchToolsDef}
	allTools := a.registry.getTools()
	for _, p := range pending {
		for _, t := range allTools {
			if t.Name == p.Name {
				already := false
				for _, o := range offered {
					if o.Name == t.Name {
						already = true
						break
					}
				}
				if !already {
					offered = append(offered, t)
				}
				break
			}
		}
	}
	return offered
}

func (a *Agent) loop(ctx *context.Context, sessionID, userQuery string, offered []llm.ToolDef) (Reply, error) {
	iterations := 0
	actingFailures := 0
	retried := false

	a.fsm.current = StateIdle
	if err := a.fsm.transition(StateReasoning); err != nil {
		return Reply{}, err
	}

	for iterations < a.cfg.MaxIterations {
		iterations++

		req, err := a.request(ctx, sessionID, offered)
		if err != nil {
			return Reply{}, fmt.Errf("failed to prepare context: %w", err)
		}

		if retried {
			req.Messages = append(req.Messages, llm.Message{
				Role:    llm.RoleSystem,
				Content: criticRetryNote,
			})
		}

		if a.fsm.current != StateReasoning {
			if err := a.fsm.transition(StateReasoning); err != nil {
				return Reply{}, err
			}
		}

		resp, err := a.llms.Primary.Generate(ctx, req)
		if err != nil {
			return Reply{}, fmt.Errf("LLM generation failed: %w", err)
		}

		if resp.StopReason == llm.StopMaxTokens {
			if err := a.fsm.transition(StateResponding); err != nil {
				return Reply{}, err
			}
			if err := a.fsm.transition(StateIdle); err != nil {
				return Reply{}, err
			}
			return Reply{}, fmt.Errf(errOutputTruncated)
		}

		// Acting
		if resp.StopReason == llm.StopToolUse {
			// Only a tool the model was offered can wait for the person. A call to a tool that was
			// never offered is refused below like any other, so confirming cannot run it.
			var pending []llm.ToolCall
			hasModifying := false
			for _, call := range resp.ToolCalls {
				if call.Name == searchToolsName || isOffered(offered, call.Name) {
					pending = append(pending, call)
					if call.Name != searchToolsName && !a.registry.readOnly(call.Name) {
						hasModifying = true
					}
				}
			}

			toolCalls := resp.ToolCalls
			if hasModifying {
				toolCalls = pending
			}
			assistantTurn := a.newTurn(llm.Message{
				Role:      llm.RoleAssistant,
				Content:   resp.Text,
				ToolCalls: toolCalls,
			})
			if err := a.mem.AppendTurn(ctx, sessionID, assistantTurn); err != nil {
				return Reply{}, fmt.Errf("failed to save assistant turn: %w", err)
			}

			if hasModifying {
				if err := a.fsm.transition(StateResponding); err != nil {
					return Reply{}, err
				}
				if err := a.fsm.transition(StateIdle); err != nil {
					return Reply{}, err
				}
				return Reply{Text: resp.Text, Pending: pending}, nil
			}

			if err := a.fsm.transition(StateActing); err != nil {
				return Reply{}, err
			}

			for _, call := range resp.ToolCalls {
				failed, err := a.runCall(ctx, sessionID, call, &offered)
				if err != nil {
					return Reply{}, err
				}
				if failed {
					actingFailures++
				} else {
					actingFailures = 0
				}
				if actingFailures >= a.cfg.MaxRetries {
					if err := a.fsm.transition(StateResponding); err != nil {
						return Reply{}, err
					}
					return Reply{Text: maxRetriesReply}, nil
				}
			}
			continue
		}

		// Critic / End turn
		if resp.StopReason == llm.StopEndTurn {
			if err := a.fsm.transition(StateReflecting); err != nil {
				return Reply{}, err
			}

			if a.cfg.Critic != nil && !retried {
				rejects, err := a.criticRejects(ctx, sessionID, userQuery, resp.Text)
				if err != nil {
					return Reply{}, err
				}
				if rejects {
					retried = true
					if err := a.fsm.transition(StateReasoning); err != nil {
						return Reply{}, err
					}
					continue
				}
			}

			candidateTurn := a.newTurn(llm.Message{
				Role:    llm.RoleAssistant,
				Content: resp.Text,
			})
			if err := a.mem.AppendTurn(ctx, sessionID, candidateTurn); err != nil {
				return Reply{}, fmt.Errf("failed to save candidate turn: %w", err)
			}

			if err := a.fsm.transition(StateResponding); err != nil {
				return Reply{}, err
			}
			if err := a.fsm.transition(StateIdle); err != nil {
				return Reply{}, err
			}
			return Reply{Text: resp.Text}, nil
		}

		return Reply{}, fmt.Errf("agent: unknown stop reason %q", resp.StopReason)
	}

	return Reply{}, fmt.Errf("max iterations reached")
}

// decline answers each pending call with declinedToolResult, so no tool call is left without
// its result.
func (a *Agent) decline(ctx *context.Context, sessionID string, calls []llm.ToolCall) error {
	for _, call := range calls {
		turn := a.newTurn(llm.Message{
			Role:       llm.RoleTool,
			Content:    declinedToolResult,
			ToolName:   call.Name,
			ToolCallID: call.ID,
		})
		if err := a.mem.AppendTurn(ctx, sessionID, turn); err != nil {
			return fmt.Errf("failed to save declined turn: %w", err)
		}
	}
	return nil
}

// runCall executes one tool call, logs it and stores its result turn. A tool that is not in
// offered is refused with an error result. failed reports whether the call failed.
func (a *Agent) runCall(ctx *context.Context, sessionID string, call llm.ToolCall, offered *[]llm.ToolDef) (failed bool, err error) {
	startTime := a.cfg.Clock.Now()

	var output string
	var execErr error
	switch {
	case call.Name == searchToolsName:
		output = a.searchTools(ctx, sessionID, call, offered)
	case !isOffered(*offered, call.Name):
		execErr = fmt.Errf(errToolNotOffered, call.Name)
	default:
		output, execErr = a.registry.execute(ctx, call.Name, call.Input)
	}

	duration := (a.cfg.Clock.Now() - startTime) / 1e6

	var errText string
	if execErr != nil {
		errText = execErr.Error()
		output = fmt.Sprintf("Error: %s", errText)
	}

	if logErr := a.mem.LogToolCall(ctx, sessionID, call.Name, call.Input, output, errText, duration); logErr != nil {
		return false, fmt.Errf("agent: failed to log tool call: %w", logErr)
	}

	turn := a.newTurn(llm.Message{
		Role:       llm.RoleTool,
		Content:    output,
		ToolName:   call.Name,
		ToolCallID: call.ID,
	})
	if err := a.mem.AppendTurn(ctx, sessionID, turn); err != nil {
		return false, fmt.Errf("failed to save tool turn: %w", err)
	}
	return execErr != nil, nil
}

func isOffered(offered []llm.ToolDef, name string) bool {
	for _, o := range offered {
		if o.Name == name {
			return true
		}
	}
	return false
}
