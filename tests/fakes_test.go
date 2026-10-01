package tests

import (
	"strings"
	"testing"
	"time"

	"webtyp.com/agent"
	"webtyp.com/context"
	"webtyp.com/llm"
	"webtyp.com/model"
)

// scriptedDecider answers each question by its Text (and, when Contains is set, only if the
// question's Context contains it). A question it has no answer for fails the test.
type scriptedDecider struct {
	t       *testing.T
	answers []scripted
	asked   []llm.Question
}

type scripted struct {
	Text       string
	Contains   string
	Choice     int
	Confidence float64
}

func (d *scriptedDecider) Decide(ctx *context.Context, q llm.Question) (llm.Decision, error) {
	d.asked = append(d.asked, q)
	for _, a := range d.answers {
		if a.Text == q.Text && (a.Contains == "" || strings.Contains(q.Context, a.Contains)) {
			probs := make([]float64, len(q.Options))
			if len(q.Options) > 1 {
				for i := range probs {
					probs[i] = (1 - a.Confidence) / float64(len(q.Options)-1)
				}
			}
			if a.Choice >= 0 && a.Choice < len(q.Options) {
				probs[a.Choice] = a.Confidence
			}
			return llm.Decision{Choice: a.Choice, Confidence: a.Confidence, Probs: probs}, nil
		}
	}
	d.t.Fatalf("unexpected question %q (context %q)", q.Text, q.Context)
	return llm.Decision{}, nil
}

// recordingWriter returns Text for every request and records them.
type recordingWriter struct {
	text     string
	requests []llm.Request
}

func (w *recordingWriter) Generate(ctx *context.Context, req llm.Request) (llm.Response, error) {
	w.requests = append(w.requests, req)
	return llm.Response{Text: w.text, StopReason: llm.StopEndTurn}, nil
}

// fakeTool is a Tool with a fixed result that counts its calls and records its arguments.
type fakeTool struct {
	name, description, schema, result string
	action                            model.Action
	fail                              bool
	calls                             []string
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return f.description }
func (f *fakeTool) InputSchema() string {
	if f.schema != "" {
		return f.schema
	}
	return `{"type":"object","properties":{}}`
}
func (f *fakeTool) Action() model.Action {
	if f.action != 0 {
		return f.action
	}
	return model.Read
}
func (f *fakeTool) Execute(ctx *context.Context, argsJSON string) (string, error) {
	f.calls = append(f.calls, argsJSON)
	if f.fail {
		return "", &toolErr{msg: "tool execution failed"}
	}
	return f.result, nil
}

type toolErr struct{ msg string }

func (e *toolErr) Error() string { return e.msg }

type quarterCounter struct{}

func (q quarterCounter) CountTokens(text string) int {
	return (len(text) + 3) / 4
}

type fixedClock struct{}

func (c fixedClock) Now() int64 {
	return time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC).UnixNano()
}

func (c fixedClock) UTCOffsetMinutes() int {
	return -180
}

func jose() agent.Texts {
	return agent.Texts{
		Speaker:       "A staff member of a clinic",
		Assistant:     "a clinic assistant",
		NoToolOption:  "ninguna herramienta: saludo, agradecimiento u otra cosa",
		NoTool:        "¡Hola! ¿En qué puedo ayudarte hoy?",
		Refused:       "No puedo cumplir con esa solicitud.",
		TooLong:       "El mensaje es demasiado largo.",
		Clarify:       "¿A qué te refieres con tu solicitud?",
		Confirm:       "Por favor confirma si deseas realizar esta acción.",
		Declined:      "Entendido, la acción fue cancelada.",
		Failed:        "Ocurrió un error al ejecutar la acción.",
		Yes:           "Sí.",
		No:            "No.",
		Found:         "Información encontrada:",
		WriterSystem:  "Eres un asistente amable.",
		DataLabel:     "Datos del consultorio:",
		QuestionLabel: "Pregunta:",
	}
}
