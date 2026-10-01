package agent

import (
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/llm"
)

func (a *Agent) newTurn(msg llm.Message) agentcontext.Turn {
	tokens := a.cfg.Tokens.CountTokens(msg.Content)
	return agentcontext.Turn{
		ID:        a.idGen.NewID(),
		CreatedAt: a.cfg.Clock.Now() / 1e9,
		Message:   msg,
		Tokens:    tokens,
	}
}

// Run answers one message from the person (docs/HYBRID_DESIGN.md, "La especificación de v1").
func (a *Agent) Run(ctx *context.Context, sessionID, message string) (Reply, error) {
	if err := a.mem.EnsureSession(ctx, sessionID); err != nil {
		return Reply{}, fmt.Errf("failed to ensure session: %w", err)
	}
	// A new message declines whatever was waiting for confirmation.
	waiting, err := a.pending(ctx, sessionID)
	if err != nil {
		return Reply{}, err
	}
	if err := a.decline(ctx, sessionID, waiting); err != nil {
		return Reply{}, err
	}

	msg, v := a.cfg.Guard.check(message)
	if err := a.mem.AppendTurn(ctx, sessionID, a.newTurn(llm.Message{Role: llm.RoleUser, Content: msg})); err != nil {
		return Reply{}, fmt.Errf("failed to append user turn: %w", err)
	}
	switch v {
	case verdictTooLong:
		return a.say(ctx, sessionID, a.cfg.Texts.TooLong)
	case verdictFlagged:
		return a.say(ctx, sessionID, a.cfg.Texts.Refused)
	}

	tool, text, err := a.route(ctx, msg)
	if err != nil {
		return Reply{}, err
	}
	if tool == nil {
		return a.say(ctx, sessionID, text)
	}
	args, err := a.arguments(ctx, msg, *tool)
	if err != nil {
		return Reply{}, err
	}
	call := llm.ToolCall{ID: a.idGen.NewID(), Name: tool.Name, Input: args}

	if !a.registry.readOnly(tool.Name) {
		injected, err := a.injected(ctx, msg)
		if err != nil {
			return Reply{}, err
		}
		if injected {
			return a.say(ctx, sessionID, a.cfg.Texts.Refused)
		}
		if err := a.saveCall(ctx, sessionID, a.cfg.Texts.Confirm, call); err != nil {
			return Reply{}, err
		}
		return Reply{Text: a.cfg.Texts.Confirm, Pending: []llm.ToolCall{call}}, nil
	}

	if err := a.saveCall(ctx, sessionID, "", call); err != nil {
		return Reply{}, err
	}
	result, failed, err := a.runCall(ctx, sessionID, call)
	if err != nil {
		return Reply{}, err
	}
	if failed {
		return a.say(ctx, sessionID, a.cfg.Texts.Failed)
	}
	answer, err := a.answer(ctx, msg, tool.Name, result, false)
	if err != nil {
		return Reply{}, err
	}
	return a.say(ctx, sessionID, answer)
}

// Confirm runs the calls waiting for the person and answers with their result.
func (a *Agent) Confirm(ctx *context.Context, sessionID string) (Reply, error) {
	calls, err := a.pending(ctx, sessionID)
	if err != nil {
		return Reply{}, err
	}
	if len(calls) == 0 {
		return Reply{}, fmt.Err(errNothingToConfirm)
	}
	msg, err := a.lastUserQuery(ctx, sessionID)
	if err != nil {
		return Reply{}, err
	}
	var lastResult string
	var lastFailed bool
	for _, call := range calls {
		res, failed, err := a.runCall(ctx, sessionID, call)
		if err != nil {
			return Reply{}, err
		}
		if failed {
			return a.say(ctx, sessionID, a.cfg.Texts.Failed)
		}
		lastResult = res
		lastFailed = failed
	}
	_ = lastFailed
	answer, err := a.answer(ctx, msg, calls[len(calls)-1].Name, lastResult, true)
	if err != nil {
		return Reply{}, err
	}
	return a.say(ctx, sessionID, answer)
}

// Decline cancels the calls waiting for the person.
func (a *Agent) Decline(ctx *context.Context, sessionID string) (Reply, error) {
	calls, err := a.pending(ctx, sessionID)
	if err != nil {
		return Reply{}, err
	}
	if len(calls) == 0 {
		return Reply{}, fmt.Err(errNothingToDecline)
	}
	if err := a.decline(ctx, sessionID, calls); err != nil {
		return Reply{}, err
	}
	return a.say(ctx, sessionID, a.cfg.Texts.Declined)
}

func (a *Agent) say(ctx *context.Context, sessionID, text string) (Reply, error) {
	msg := llm.Message{Role: llm.RoleAssistant, Content: text}
	if err := a.mem.AppendTurn(ctx, sessionID, a.newTurn(msg)); err != nil {
		return Reply{}, fmt.Errf("failed to append assistant turn: %w", err)
	}
	return Reply{Text: text}, nil
}

func (a *Agent) saveCall(ctx *context.Context, sessionID, text string, call llm.ToolCall) error {
	msg := llm.Message{Role: llm.RoleAssistant, Content: text, ToolCalls: []llm.ToolCall{call}}
	return a.mem.AppendTurn(ctx, sessionID, a.newTurn(msg))
}

func (a *Agent) pending(ctx *context.Context, sessionID string) ([]llm.ToolCall, error) {
	turns, err := a.mem.GetTurns(ctx, sessionID, 1)
	if err != nil {
		return nil, fmt.Errf("failed to get turns: %w", err)
	}
	if len(turns) == 0 {
		return nil, nil
	}
	last := turns[0]
	if last.Message.Role == llm.RoleAssistant && len(last.Message.ToolCalls) > 0 {
		return last.Message.ToolCalls, nil
	}
	return nil, nil
}

func (a *Agent) lastUserQuery(ctx *context.Context, sessionID string) (string, error) {
	turns, err := a.mem.GetTurns(ctx, sessionID, 20)
	if err != nil {
		return "", fmt.Errf("failed to get turns for query: %w", err)
	}
	for _, t := range turns {
		if t.Message.Role == llm.RoleUser {
			return t.Message.Content, nil
		}
	}
	return "", nil
}

func (a *Agent) decline(ctx *context.Context, sessionID string, calls []llm.ToolCall) error {
	for range calls {
		turn := a.newTurn(llm.Message{
			Role:    llm.RoleTool,
			Content: declinedToolResult,
		})
		if err := a.mem.AppendTurn(ctx, sessionID, turn); err != nil {
			return fmt.Errf("failed to append declined tool turn: %w", err)
		}
	}
	return nil
}

func (a *Agent) runCall(ctx *context.Context, sessionID string, call llm.ToolCall) (string, bool, error) {
	start := a.cfg.Clock.Now()
	output, execErr := a.registry.execute(ctx, call.Name, call.Input)
	durationMS := (a.cfg.Clock.Now() - start) / 1e6

	var errText string
	if execErr != nil {
		errText = execErr.Error()
	}

	if err := a.mem.LogToolCall(ctx, sessionID, call.Name, call.Input, output, errText, durationMS); err != nil {
		return "", false, fmt.Errf("failed to log tool call: %w", err)
	}

	content := output
	if execErr != nil {
		content = execErr.Error()
	}
	turn := a.newTurn(llm.Message{
		Role:    llm.RoleTool,
		Content: content,
	})
	if err := a.mem.AppendTurn(ctx, sessionID, turn); err != nil {
		return "", false, fmt.Errf("failed to append tool turn: %w", err)
	}

	return output, execErr != nil, nil
}

func (a *Agent) route(ctx *context.Context, msg string) (*llm.ToolDef, string, error) {
	all := a.registry.getTools()
	if len(all) == 0 {
		return nil, a.cfg.Texts.NoTool, nil
	}

	var candidates []llm.ToolDef
	if len(all) <= a.cfg.Candidates {
		candidates = all
	} else {
		foundNames, err := a.cfg.ToolIndex.SearchTools(ctx, msg, a.cfg.Candidates)
		if err != nil {
			return nil, "", fmt.Errf("agent: tool candidates: %w", err)
		}
		seen := make([]string, 0, len(foundNames))
		for _, name := range foundNames {
			already := false
			for _, s := range seen {
				if s == name {
					already = true
					break
				}
			}
			if already {
				continue
			}
			for _, t := range all {
				if t.Name == name {
					candidates = append(candidates, t)
					seen = append(seen, name)
					break
				}
			}
		}
	}

	if len(candidates) == 0 {
		return nil, a.cfg.Texts.NoTool, nil
	}

	options := make([]string, 0, len(candidates)+1)
	for _, t := range candidates {
		options = append(options, fmt.Sprintf(toolOption, t.Name, t.Description))
	}
	options = append(options, fmt.Sprintf(noneOption, a.cfg.Texts.NoToolOption))

	q := llm.Question{
		Context: fmt.Sprintf(wroteContext, a.cfg.Texts.Speaker, msg),
		Text:    routeQuestion,
		Options: options,
	}

	dec, err := a.cfg.Decider.Decide(ctx, q)
	if err != nil {
		return nil, "", fmt.Errf("agent: decide: %w", err)
	}

	if dec.Choice == len(candidates) {
		return nil, a.cfg.Texts.NoTool, nil
	}

	if dec.Confidence < MinConfidence {
		best1, best2 := topTwoCandidates(dec.Probs, len(candidates))
		clarify := a.cfg.Texts.Clarify
		if best1 >= 0 {
			clarify += "\n- " + candidates[best1].Description
		}
		if best2 >= 0 {
			clarify += "\n- " + candidates[best2].Description
		}
		return nil, clarify, nil
	}

	if dec.Choice < 0 || dec.Choice >= len(candidates) {
		return nil, a.cfg.Texts.NoTool, nil
	}

	return &candidates[dec.Choice], "", nil
}

func topTwoCandidates(probs []float64, numCandidates int) (int, int) {
	best1, best2 := -1, -1
	p1, p2 := -1.0, -1.0
	for i := 0; i < numCandidates && i < len(probs); i++ {
		p := probs[i]
		if p > p1 {
			p2 = p1
			best2 = best1
			p1 = p
			best1 = i
		} else if p > p2 {
			p2 = p
			best2 = i
		}
	}
	return best1, best2
}

func (a *Agent) answer(ctx *context.Context, msg, toolName, result string, injectionChecked bool) (string, error) {
	yes, err := a.isYesNo(ctx, msg)
	if err != nil {
		return "", err
	}
	if yes {
		text, ok, err := a.factsAnswer(ctx, msg, result)
		if err != nil {
			return "", err
		}
		if ok {
			return text, nil
		}
	}

	for _, t := range a.cfg.Templates {
		if t.Tool == toolName {
			d := Data{
				Message:          msg,
				Result:           result,
				Now:              a.cfg.Clock.Now(),
				UTCOffsetMinutes: a.cfg.Clock.UTCOffsetMinutes(),
			}
			text, ok := t.Answer(d)
			if ok {
				return text, nil
			}
		}
	}

	if a.cfg.Writer != nil {
		if !injectionChecked {
			injected, err := a.injected(ctx, msg)
			if err != nil {
				return "", err
			}
			if injected {
				return a.cfg.Texts.Refused, nil
			}
		}

		now := a.cfg.Clock.Now()
		offset := a.cfg.Clock.UTCOffsetMinutes()
		stamp := agentcontext.Stamp(now/1e9, offset)
		prompt := stamp + a.cfg.Texts.DataLabel + " " + result + "\n\n" + a.cfg.Texts.QuestionLabel + " " + msg

		resp, err := a.cfg.Writer.Generate(ctx, llm.Request{
			System:          a.cfg.Texts.WriterSystem,
			Messages:        []llm.Message{{Role: llm.RoleUser, Content: prompt}},
			MaxOutputTokens: a.cfg.WriterMaxTokens,
		})
		if err != nil {
			return "", fmt.Errf("agent: writer: %w", err)
		}

		draft := fmt.TrimSpace(resp.Text)
		if draft != "" && resp.StopReason == llm.StopEndTurn {
			rejected, err := a.criticRejects(ctx, msg, toolName, result, draft)
			if err != nil {
				return "", err
			}
			if !rejected {
				return draft, nil
			}
		}
	}

	return a.cfg.Texts.Found + "\n" + result, nil
}
