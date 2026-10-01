package agent

// Texts are the application's words: what the person reads, and the two English phrases that
// tell the decision model who is talking to whom (the questions were measured with them).
// Every field is required, except the three Writer* fields, required only when Config.Writer is set.
type Texts struct {
	Speaker      string // who writes to the agent, in English: "A staff member of a clinic"
	Assistant    string // who the agent is, in English: "a clinic assistant"
	NoToolOption string // the "no tool" option the decision model reads: "ninguna herramienta: saludo, agradecimiento u otra cosa"

	NoTool   string // the answer when no tool fits (a greeting, thanks)
	Refused  string // the answer to a message flagged as an attempt to change the agent's rules
	TooLong  string // the answer to a message longer than Guard.MaxChars
	Clarify  string // asked when the tool is unclear; the likely tools' descriptions follow, one per line
	Confirm  string // shown with Reply.Pending
	Declined string // the answer after Decline
	Failed   string // the answer when the tool returned an error
	Yes      string // the answer to a yes/no question whose answer is yes
	No       string // the answer to a yes/no question whose answer is no
	Found    string // shown before the tool's data when nothing else phrases it

	WriterSystem  string // the writer's system prompt
	DataLabel     string // before the data in the writer's prompt: "Datos del consultorio:"
	QuestionLabel string // before the message in the writer's prompt: "Pregunta:"
}

// Template answers from one tool's result in the application's words.
type Template struct {
	Tool   string                      // the tool's name, as registered
	Answer func(d Data) (string, bool) // false: this result needs another answer (the writer, or the data as is)
}

// Data is what a template reads.
type Data struct {
	Message          string // the person's message, as the guard cleaned it
	Result           string // the tool's output
	Now              int64  // Clock.Now(), unix nanoseconds
	UTCOffsetMinutes int    // Clock.UTCOffsetMinutes()
}

// missingText returns the name of the first required field that is empty, or "".
func (t Texts) missingText() string {
	fields := []struct {
		name  string
		value string
	}{
		{"Speaker", t.Speaker},
		{"Assistant", t.Assistant},
		{"NoToolOption", t.NoToolOption},
		{"NoTool", t.NoTool},
		{"Refused", t.Refused},
		{"TooLong", t.TooLong},
		{"Clarify", t.Clarify},
		{"Confirm", t.Confirm},
		{"Declined", t.Declined},
		{"Failed", t.Failed},
		{"Yes", t.Yes},
		{"No", t.No},
		{"Found", t.Found},
	}
	for _, f := range fields {
		if f.value == "" {
			return f.name
		}
	}
	return ""
}

// missingWriterText does the same for WriterSystem, DataLabel, QuestionLabel.
func (t Texts) missingWriterText() string {
	fields := []struct {
		name  string
		value string
	}{
		{"WriterSystem", t.WriterSystem},
		{"DataLabel", t.DataLabel},
		{"QuestionLabel", t.QuestionLabel},
	}
	for _, f := range fields {
		if f.value == "" {
			return f.name
		}
	}
	return ""
}
