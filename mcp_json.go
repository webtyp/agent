package agent

import (
	"webtyp.com/json"
	"webtyp.com/model"
)

type toolEntry struct {
	Name        string
	Description string
	InputSchema string
}

func (t *toolEntry) IsNil() bool { return t == nil }

func (t *toolEntry) Schema() []model.Field { return nil }
func (t *toolEntry) Pointers() []any       { return nil }

func (t *toolEntry) DecodeFields(r model.FieldReader) {
	if v, ok := r.String("name"); ok {
		t.Name = v
	}
	if v, ok := r.String("description"); ok {
		t.Description = v
	}
	if raw, ok := r.Raw("inputSchema"); ok {
		t.InputSchema = raw
	} else if v, ok := r.String("inputSchema"); ok {
		t.InputSchema = v
	}
}

type toolEntryList struct {
	items []toolEntry
}

func (l *toolEntryList) IsNil() bool { return l == nil }

func (l *toolEntryList) Len() int { return len(l.items) }

func (l *toolEntryList) At(i int) model.Fielder {
	return &l.items[i]
}

func (l *toolEntryList) Append() model.Fielder {
	l.items = append(l.items, toolEntry{})
	return &l.items[len(l.items)-1]
}

func (l *toolEntryList) DecodeFields(r model.FieldReader) {}

type listToolsResult struct {
	Tools []toolEntry
}

func (l *listToolsResult) IsNil() bool { return l == nil }

func (l *listToolsResult) DecodeFields(r model.FieldReader) {
	if raw, ok := r.Raw("tools"); ok {
		var list toolEntryList
		if err := json.Decode([]byte(raw), &list); err == nil {
			l.Tools = list.items
		}
	}
}
