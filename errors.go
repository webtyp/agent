package agent

const (
	errDeciderRequired     = "agent: Decider is required"
	errTokensRequired      = "agent: Tokens is required"
	errMemoryRequired      = "agent: Memory is required"
	errIDGenRequired       = "agent: IDGen is required"
	errToolIndexRequired   = "agent: ToolIndex is required"
	errTextRequired        = "agent: Texts.%s is required"
	errWriterTextRequired  = "agent: Texts.%s is required when Writer is set"
	errCandidatesRange     = "agent: Candidates must be between 1 and 9"
	errMaxCharsNegative    = "agent: Guard.MaxChars must not be negative"
	errTemplateUnknownTool = "agent: Templates: no tool named %s"
	errTemplateNoAnswer    = "agent: Templates: %s has no Answer"
	errInputSchema         = "agent: tool %s: reading InputSchema: %w"
	errNothingToConfirm    = "agent: nothing to confirm in this session"
	errNothingToDecline    = "agent: nothing to decline in this session"
	declinedToolResult     = "The person declined this action; it was not executed."
)
