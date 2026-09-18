package agent

import (
	"context"
	"time"

	"webtyp.com/fmt"
)

// prepareContext builds the context window for the next LLM turn.
// It handles token budgeting and triggers summarization if necessary.
func (a *Agent) prepareContext(ctx context.Context, sessionID string) (*LLMRequest, error) {
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
	// Budget: Limit - Buffer - ToolDefs (approx) - SystemPrompt - Episodes
	// We don't have tool defs size yet, but we can estimate or ignore for now if Buffer covers it.
	// SystemPrompt size also needs estimation.

	// For now, let's just sum up messages and episodes and compare with (Limit * SummarizeAt).

	totalTokens := 0
	// Add system prompt tokens (rough estimate: 1 char ~= 1 token is too high, usually 4 chars ~= 1 token)
	// Let's assume 1 token per 4 characters for now if not stored.
	// Ideally we should tokenize, but we don't have a tokenizer here.
	// We'll rely on stored TokenCount if available, or estimate.

	// Estimate system prompt
	sysPrompt := a.buildSystemPrompt() // We need to implement this
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
			// Log error but continue without summarization? Or fail?
			// Let's fail for now to be safe.
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
			CreatedAt:  time.Now().Unix(),
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
	// We need to inject episodes into the context. usually as a system message or valid user/assistant sequence?
	// The plan says "Build Context Window: Recent messages + Last 5 episodes."
	// Usually episodes are added before messages.

	// We can prepend episodes as a system message or a special context message.
	// Let's format them into the system prompt or a first message.

	// If we modify system prompt, we need to return it.

	// Let's append episodes to the system prompt or insert them as the first message?
	// "Episodes" are summaries of past conversation.

	finalMessages := make([]Message, 0, len(messages) + 1)

	// Helper to format episodes
	if len(episodes) > 0 {
		sb := fmt.Convert("")
		sb.WrString(fmt.BuffOut, "Previous conversation summary:\n")
		// Episodes are returned in descending order (newest first) by GetEpisodes,
		// but for context we want chronological?
		// "GetEpisodes ... ORDER BY created_at DESC"
		// So we should iterate backwards or reverse them.
		for i := len(episodes) - 1; i >= 0; i-- {
			sb.WrString(fmt.BuffOut, fmt.Sprintf("- %s\n", episodes[i].Summary))
		}

		// Add as a system message or prepend to first message?
		// We'll add as a system message.
		finalMessages = append(finalMessages, Message{
			Role: "system",
			Content: sb.GetString(fmt.BuffOut),
			CreatedAt: time.Now().Unix(), // This is ephemeral
		})
	}

	finalMessages = append(finalMessages, messages...)

	req := &LLMRequest{
		SystemPrompt: sysPrompt, // Base system prompt
		Messages:     finalMessages,
		Tools:        nil,                           // Tools are injected by orchestrator
		MaxTokens:    a.cfg.ContextWindow.MaxTokens, // Or remaining? Usually provider limit.
	}

	return req, nil
}

func (a *Agent) summarizeMessages(ctx context.Context, msgs []Message) (string, int, error) {
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
		MaxTokens: 500, // Reasonable limit for summary
	}

	resp, err := summarizer.Generate(ctx, req)
	if err != nil {
		return "", 0, err
	}

	// Return summary text and token usage
	return resp.Text, resp.TokensUsed, nil
}

func (a *Agent) buildSystemPrompt() string {
	sb := fmt.Convert("")
	sb.WrString(fmt.BuffOut, fmt.Sprintf("You are %s. %s\n", a.cfg.Identity.Name, a.cfg.Identity.Role))
	sb.WrString(fmt.BuffOut, a.cfg.Identity.Instructions + "\n")
	if len(a.cfg.Identity.Goals) > 0 {
		sb.WrString(fmt.BuffOut, "Goals:\n")
		for _, g := range a.cfg.Identity.Goals {
			sb.WrString(fmt.BuffOut, fmt.Sprintf("- %s\n", g))
		}
	}
	return sb.GetString(fmt.BuffOut)
}
