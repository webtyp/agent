package agent

const (
	reflectorSystem        = "You are a critic that evaluates AI responses."
	reflectionPromptFormat = `
Analyze the following user query and the assistant's response.
User Query: "%s"
Assistant Response: "%s"

Is the response complete and accurate?
If YES, respond with "SUFFICIENT".
If NO, respond with "INSUFFICIENT" followed by a short critique.
`
	reflectionOutputTokens = 100
)
