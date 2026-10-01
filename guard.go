package agent

import (
	"unicode"

	"webtyp.com/fmt"
)

// DefaultMaxChars is the longest message the guard lets through when Guard.MaxChars is 0.
const DefaultMaxChars = 2000

// Guard is the code check every message passes before any model reads it (HYBRID_DESIGN D7).
type Guard struct {
	MaxChars int      // longer messages are refused, in characters (default DefaultMaxChars)
	Phrases  []string // the application's phrases that flag a message, in its language: "tus instrucciones", "desde ahora eres"
	Roles    []string // role words, besides system, assistant and developer, that flag a line starting with "<role>:"
}

type verdict uint8

const (
	verdictClean verdict = iota
	verdictTooLong
	verdictFlagged
)

// chatMarkers are pieces of chat-template syntax that no person types by accident.
var chatMarkers = []string{"<|", "|>", "<tool_call", "</tool_call", "<tool_response", "<function=",
	"<think>", "</think>", "[inst]", "[/inst]", "<<sys>>", "<start_of_turn>", "<end_of_turn>"}

// builtinRoles are the role words every chat template uses.
var builtinRoles = []string{"system", "assistant", "developer"}

// check cleans msg and says whether it may go on.
func (g Guard) check(msg string) (string, verdict) {
	clean := fmt.TrimSpace(sanitize(msg))
	if len([]rune(clean)) > g.MaxChars {
		return clean, verdictTooLong
	}
	folded := fold(clean)
	for _, m := range chatMarkers {
		if fmt.Contains(folded, m) {
			return clean, verdictFlagged
		}
	}
	if g.roleLine(folded) {
		return clean, verdictFlagged
	}
	for _, p := range g.Phrases {
		if p != "" && fmt.Contains(folded, fold(p)) {
			return clean, verdictFlagged
		}
	}
	return clean, verdictClean
}

func sanitize(s string) string {
	runes := []rune(s)
	out := make([]rune, 0, len(runes))
	for _, r := range runes {
		if (r < 0x20 && r != '\n' && r != '\t') || (r >= 0x7F && r <= 0x9F) {
			continue
		}
		if (r >= 0x200B && r <= 0x200F) || (r >= 0x202A && r <= 0x202E) || (r >= 0x2060 && r <= 0x2064) || (r >= 0x2066 && r <= 0x2069) || r == 0xFEFF {
			continue
		}
		if r >= 0xE0000 && r <= 0xE007F {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func fold(s string) string {
	runes := []rune(s)
	out := make([]rune, len(runes))
	for i, r := range runes {
		r = unicode.ToLower(r)
		switch r {
		case 'á', 'à', 'â', 'ä', 'ã':
			r = 'a'
		case 'é', 'è', 'ê', 'ë':
			r = 'e'
		case 'í', 'ì', 'î', 'ï':
			r = 'i'
		case 'ó', 'ò', 'ô', 'ö', 'õ':
			r = 'o'
		case 'ú', 'ù', 'û', 'ü':
			r = 'u'
		case 'ñ':
			r = 'n'
		case 'ç':
			r = 'c'
		}
		out[i] = r
	}
	return string(out)
}

func (g Guard) roleLine(folded string) bool {
	roles := make([]string, 0, len(builtinRoles)+len(g.Roles))
	roles = append(roles, builtinRoles...)
	for _, r := range g.Roles {
		roles = append(roles, fold(r))
	}

	start := 0
	for start < len(folded) {
		end := start
		for end < len(folded) && folded[end] != '\n' {
			end++
		}
		line := folded[start:end]
		if end < len(folded) {
			start = end + 1
		} else {
			start = len(folded)
		}

		idx := 0
		for idx < len(line) {
			b := line[idx]
			if b == ' ' || b == '\t' || b == '#' || b == '*' || b == '>' || b == '[' || b == '-' {
				idx++
			} else {
				break
			}
		}
		sub := line[idx:]
		for _, r := range roles {
			if r != "" && fmt.HasPrefix(sub, r) {
				rest := sub[len(r):]
				ridx := 0
				for ridx < len(rest) && (rest[ridx] == ' ' || rest[ridx] == '\t') {
					ridx++
				}
				if ridx < len(rest) && (rest[ridx] == ':' || rest[ridx] == ']') {
					return true
				}
			}
		}
	}
	return false
}
