package agent

const (
	errPrimaryRequired   = "agent: LLMs.Primary is required"
	errTokensRequired    = "agent: Tokens is required"
	errMemoryRequired    = "agent: Memory is required"
	errIDGenRequired     = "agent: IDGen is required"
	errOutputTruncated   = "agent: the model stopped at Budget.OutputTokens before finishing; raise OutputTokens"
	errToolIndexRequired = "agent: ToolIndex is required"
	errSearchToolsName   = "agent: a tool named search_tools is reserved for tool search"
	errNothingToConfirm  = "agent: nothing to confirm in this session"
	errNothingToDecline  = "agent: nothing to decline in this session"
	declinedToolResult   = "The person declined this action; it was not executed."
	errToolNotOffered    = "tool %s is not available; call search_tools first"
	maxRetriesReply      = "Maximum tool retries reached. Please try again."
	repeatedCallResult   = "You already called %s with these arguments in this turn; its result is above. Answer the person with it."
	errPreselectNegative = "agent: PreselectTools must not be negative"
)
