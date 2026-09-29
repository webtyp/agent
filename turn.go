package agent

import (
	"webtyp.com/agentcontext"
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/llm"
	"webtyp.com/time"
)

// newTurn stamps a message with an ID, its token count and the current time.
func (a *Agent) newTurn(msg llm.Message) agentcontext.Turn {
	return agentcontext.Turn{
		ID:        a.idGen.NewID(),
		Message:   msg,
		Tokens:    a.cfg.Tokens.CountTokens(msg.Content),
		CreatedAt: time.Now() / 1e9,
	}
}

// request loads history, compacts it if needed, and compiles the request for this step.
func (a *Agent) request(ctx *context.Context, sessionID string, offered []llm.ToolDef) (llm.Request, error) {
	summaries, err := a.mem.GetSummaries(ctx, sessionID, a.cfg.RecentSummaries)
	if err != nil {
		return llm.Request{}, fmt.Errf("failed to get summaries: %w", err)
	}
	turns, err := a.mem.GetTurns(ctx, sessionID, a.cfg.RecentTurns)
	if err != nil {
		return llm.Request{}, fmt.Errf("failed to get turns: %w", err)
	}

	in := agentcontext.Input{
		Identity:  a.cfg.Identity,
		Summaries: summaries,
		Turns:     turns,
		Tools:     offered,
	}

	if old := agentcontext.Compact(in, a.cfg.Budget, a.cfg.Tokens); old != nil {
		summarizer := a.llms.Summarizer
		if summarizer == nil {
			summarizer = a.llms.Primary
		}

		resp, err := summarizer.Generate(ctx, agentcontext.SummaryRequest(old, a.cfg.Budget))
		if err != nil {
			return llm.Request{}, fmt.Errf("failed to generate summary: %w", err)
		}

		now := time.Now() / 1e9
		sum := agentcontext.Summary{
			ID:         a.idGen.NewID(),
			Text:       resp.Text,
			Tokens:     a.cfg.Tokens.CountTokens(resp.Text),
			FromTurnID: old[0].ID,
			ToTurnID:   old[len(old)-1].ID,
			CreatedAt:  now,
		}
		if err := a.mem.SaveSummary(ctx, sessionID, sum); err != nil {
			return llm.Request{}, fmt.Errf("failed to save summary: %w", err)
		}

		var ids []string
		for _, t := range old {
			ids = append(ids, t.ID)
		}
		if err := a.mem.DeleteTurns(ctx, sessionID, ids); err != nil {
			return llm.Request{}, fmt.Errf("failed to delete turns: %w", err)
		}

		summaries, err = a.mem.GetSummaries(ctx, sessionID, a.cfg.RecentSummaries)
		if err != nil {
			return llm.Request{}, fmt.Errf("failed to reload summaries: %w", err)
		}
		turns, err = a.mem.GetTurns(ctx, sessionID, a.cfg.RecentTurns)
		if err != nil {
			return llm.Request{}, fmt.Errf("failed to reload turns: %w", err)
		}

		in = agentcontext.Input{
			Identity:  a.cfg.Identity,
			Summaries: summaries,
			Turns:     turns,
			Tools:     offered,
		}
	}

	return agentcontext.Compile(in, a.cfg.Budget, a.cfg.Tokens)
}
