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
)
