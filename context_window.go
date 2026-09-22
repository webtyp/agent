package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/time"
)

// prepareContext builds the context window for the next LLM turn.
// It handles token budgeting and triggers summarization if necessary.
func (a *Agent) prepareContext(ctx *context.Context, sessionID string) (*LLMRequest, error) {
	// 1. Load recent episodes
	episodes, err := a.mem.GetEpisodes(ctx, sessionID, a.cfg.ContextWindow.MaxEpisodes)
	if err != nil {
		return nil, fmt.Errf("failed to load episodes: %w", err)
	}

	// 2. Load recent messages
	messages, err := a.mem.GetMessages(ctx, sessionID, a.cfg.ContextWindow.MaxRecentMsgs)
	if err != nil {
		return nil, fmt.Errf("failed to load messages: %w", err)
	}

	// 3. Calculate current token usage
	totalTokens := 0
	sysPrompt := a.buildSystemPrompt()
	totalTokens += len(sysPrompt) / 4

	for _, ep := range episodes {
		totalTokens += ep.TokenCount
	}
	for _, msg := range messages {
		totalTokens += msg.TokenCount
	}

	threshold := float64(a.cfg.ContextWindow.MaxTokens) * a.cfg.ContextWindow.SummarizeAt

	// 4. Check threshold and summarize if needed
	if float64(totalTokens) > threshold && len(messages) > 1 {
		// Summarize oldest 50%
		numToSummarize := len(messages) / 2
		if numToSummarize < 1 {
			numToSummarize = 1
		}

		toSummarize := messages[:numToSummarize]
		remaining := messages[numToSummarize:]

		// Perform summarization
		summary, tokenCount, err := a.summarizeMessages(ctx, toSummarize)
		if err != nil {
			return nil, fmt.Errf("summarization failed: %w", err)
		}

		// Save Episode
		fromID := toSummarize[0].ID
		toID := toSummarize[len(toSummarize)-1].ID
		if err := a.mem.SaveEpisode(ctx, sessionID, summary, tokenCount, fromID, toID); err != nil {
			return nil, fmt.Errf("failed to save episode: %w", err)
		}

		// Add new episode to the list for this turn context
		newEp := Episode{
			Summary:    summary,
			TokenCount: tokenCount,
			CreatedAt:  time.Now() / 1e9,
		}
		// episodes is sorted by created_at DESC (newest first). Prepend.
		episodes = append([]Episode{newEp}, episodes...)

		// Delete summarized messages
		var ids []string
		for _, m := range toSummarize {
			ids = append(ids, m.ID)
		}
		if err := a.mem.DeleteMessages(ctx, sessionID, ids); err != nil {
			return nil, fmt.Errf("failed to delete summarized messages: %w", err)
		}

		// Use remaining messages for this turn
		messages = remaining
	}

	// 5. Build LLMRequest
	finalMessages := make([]Message, 0, len(messages)+1)

	if len(episodes) > 0 {
		sb := fmt.Convert("")
		sb.WrString(fmt.BuffOut, "Previous conversation summary:\n")
		for i := len(episodes) - 1; i >= 0; i-- {
			sb.WrString(fmt.BuffOut, fmt.Sprintf("- %s\n", episodes[i].Summary))
		}

		finalMessages = append(finalMessages, Message{
			Role:      "system",
			Content:   sb.GetString(fmt.BuffOut),
			CreatedAt: time.Now() / 1e9,
		})
	}

	finalMessages = append(finalMessages, messages...)

	req := &LLMRequest{
		SystemPrompt: sysPrompt,
		Messages:     finalMessages,
		Tools:        nil,
		MaxTokens:    a.cfg.ContextWindow.MaxTokens,
	}

	return req, nil
}

func (a *Agent) summarizeMessages(ctx *context.Context, msgs []Message) (string, int, error) {
	summarizer := a.cfg.LLMs.Summarizer
	if summarizer == nil {
		summarizer = a.cfg.LLMs.Primary
	}

	sb := fmt.Convert("")
	sb.WrString(fmt.BuffOut, "Summarize the following conversation segment concisely:\n\n")
	for _, m := range msgs {
		sb.WrString(fmt.BuffOut, fmt.Sprintf("%s: %s\n", m.Role, m.Content))
		for _, tc := range m.ToolCalls {
			sb.WrString(fmt.BuffOut, fmt.Sprintf("Tool Call %s: %s(%s)\n", tc.ID, tc.Name, tc.Input))
		}
	}

	req := LLMRequest{
		SystemPrompt: "You are a helpful assistant that summarizes conversations.",
		Messages: []Message{
			{Role: "user", Content: sb.GetString(fmt.BuffOut)},
		},
		MaxTokens: 500,
	}

	resp, err := summarizer.Generate(ctx, req)
	if err != nil {
		return "", 0, err
	}

	return resp.Text, resp.TokensUsed, nil
}

func (a *Agent) buildSystemPrompt() string {
	sb := fmt.Convert("")
	sb.WrString(fmt.BuffOut, fmt.Sprintf("You are %s. %s\n", a.cfg.Identity.Name, a.cfg.Identity.Role))
	sb.WrString(fmt.BuffOut, a.cfg.Identity.Instructions+"\n")
	if len(a.cfg.Identity.Goals) > 0 {
		sb.WrString(fmt.BuffOut, "Goals:\n")
		for _, g := range a.cfg.Identity.Goals {
			sb.WrString(fmt.BuffOut, fmt.Sprintf("- %s\n", g))
		}
	}
	return sb.GetString(fmt.BuffOut)
}
