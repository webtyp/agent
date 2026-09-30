package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/llm"
)

func (a *Agent) criticContext(ctx *context.Context, sessionID, userQuery, answer string) (string, error) {
	turns, err := a.mem.GetTurns(ctx, sessionID, a.cfg.RecentTurns)
	if err != nil {
		return "", err
	}

	lastUserIdx := -1
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Message.Role == llm.RoleUser {
			lastUserIdx = i
			break
		}
	}

	out := fmt.Sprintf("User asked: %s\n", userQuery)

	if lastUserIdx >= 0 {
		for i := lastUserIdx + 1; i < len(turns); i++ {
			if turns[i].Message.Role == llm.RoleTool {
				out += fmt.Sprintf("Tool %s returned: %s\n", turns[i].Message.ToolName, turns[i].Message.Content)
			}
		}
	}

	out += fmt.Sprintf("Assistant answered: %s", answer)
	return out, nil
}

func (a *Agent) criticRejects(ctx *context.Context, sessionID, userQuery, answer string) (bool, error) {
	if a.cfg.Critic == nil {
		return false, nil
	}

	cCtx, err := a.criticContext(ctx, sessionID, userQuery, answer)
	if err != nil {
		return false, fmt.Errf("agent: critic: %w", err)
	}

	q := llm.Question{
		Context: cCtx,
		Text:    criticQuestion,
		Options: criticOptions,
	}

	dec, err := a.cfg.Critic.Decide(ctx, q)
	if err != nil {
		return false, fmt.Errf("agent: critic: %w", err)
	}

	if dec.Choice == 1 && dec.Confidence >= CriticMinConfidence {
		return true, nil
	}
	return false, nil
}
