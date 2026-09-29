package agent

import (
	"sync"

	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/llm"
)

type memToolIndex struct {
	mu    sync.RWMutex
	tools []llm.ToolDef
}

// NewMemToolIndex returns a keyword-based reference implementation of ToolIndex.
func NewMemToolIndex() ToolIndex {
	return &memToolIndex{}
}

func (m *memToolIndex) IndexTools(ctx *context.Context, tools []llm.ToolDef) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	copied := make([]llm.ToolDef, len(tools))
	copy(copied, tools)
	m.tools = copied
	return nil
}

func isWordChar(r rune) bool {
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
		return true
	}
	// Latin-1 Supplement accented letters (e.g. À-ÿ except ÷ and ×)
	if (r >= 0x00C0 && r <= 0x00D6) || (r >= 0x00D8 && r <= 0x00F6) || (r >= 0x00F8 && r <= 0x00FF) {
		return true
	}
	return false
}

func extractWords(s string) []string {
	lower := fmt.ToLower(s)
	runes := []rune(lower)

	var words []string
	var current []rune

	for _, r := range runes {
		if isWordChar(r) {
			current = append(current, r)
		} else {
			if len(current) > 0 {
				w := string(current)
				already := false
				for _, existing := range words {
					if existing == w {
						already = true
						break
					}
				}
				if !already {
					words = append(words, w)
				}
				current = current[:0]
			}
		}
	}
	if len(current) > 0 {
		w := string(current)
		already := false
		for _, existing := range words {
			if existing == w {
				already = true
				break
			}
		}
		if !already {
			words = append(words, w)
		}
	}

	return words
}

type toolScore struct {
	tool  llm.ToolDef
	score int
	index int
}

func (m *memToolIndex) SearchTools(ctx *context.Context, query string, limit int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	queryWords := extractWords(query)
	if len(queryWords) == 0 || len(m.tools) == 0 {
		return nil, nil
	}

	var matches []toolScore
	for i, t := range m.tools {
		haystack := fmt.ToLower(fmt.Sprintf("%s %s", t.Name, t.Description))
		score := 0
		for _, w := range queryWords {
			if fmt.Contains(haystack, w) {
				score++
			}
		}
		if score > 0 {
			matches = append(matches, toolScore{
				tool:  t,
				score: score,
				index: i,
			})
		}
	}

	// Insertion sort (stable sort descending by score, then by index ascending)
	for i := 1; i < len(matches); i++ {
		key := matches[i]
		j := i - 1
		for j >= 0 && (matches[j].score < key.score || (matches[j].score == key.score && matches[j].index > key.index)) {
			matches[j+1] = matches[j]
			j--
		}
		matches[j+1] = key
	}

	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}

	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = m.tool.Name
	}

	return result, nil
}
