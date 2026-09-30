package agent

const CriticMinConfidence = 0.8

const (
	criticQuestion  = "Does the assistant's answer state anything that the tool results do not support?"
	criticRetryNote = "Your previous draft stated things the tool results do not support. Answer the person again using only the tool results; if you do not have the data, call a tool."
)

var criticOptions = []string{
	"no, everything it says is supported by the tool results",
	"yes, it states something the tool results do not support",
}
