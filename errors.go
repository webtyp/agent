package agent

const (
	errPrimaryRequired   = "agent: LLMs.Primary is required"
	errTokensRequired    = "agent: Tokens is required"
	errMemoryRequired    = "agent: Memory is required"
	errIDGenRequired     = "agent: IDGen is required"
	errOutputTruncated   = "agent: the model stopped at Budget.OutputTokens before finishing; raise OutputTokens"
	errToolIndexRequired = "agent: ToolIndex is required"
	errSearchToolsName   = "agent: a tool named search_tools is reserved for tool search"
)
