package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/json"
	"webtyp.com/llm"
	"webtyp.com/model"
)

const searchToolsName = "search_tools"

// searchToolsDef is the one tool offered on every step.
var searchToolsDef = llm.ToolDef{
	Name:        searchToolsName,
	Description: "Find the tools that can do what you need. Describe the task in a few words; the matching tools become available for you to call.",
	InputSchema: `{"type":"object","properties":{"query":{"type":"string","description":"the task you need a tool for"}},"required":["query"]}`,
}

type searchQueryInput struct {
	Query string
}

func (s *searchQueryInput) IsNil() bool { return s == nil }

func (s *searchQueryInput) DecodeFields(r model.FieldReader) {
	if v, ok := r.String("query"); ok {
		s.Query = v
	}
}

func (a *Agent) searchTools(ctx *context.Context, sessionID string, call llm.ToolCall, offered *[]llm.ToolDef) string {
	var input searchQueryInput
	if err := json.Decode([]byte(call.Input), &input); err != nil || input.Query == "" {
		return "Error: search_tools needs a query"
	}

	foundNames, err := a.cfg.ToolIndex.SearchTools(ctx, input.Query, a.cfg.ToolSearchLimit)
	if err != nil || len(foundNames) == 0 {
		return "No tools matched. Try other words."
	}

	newTools := addByName(offered, a.registry.getTools(), foundNames)

	if len(newTools) == 0 {
		return "No tools matched. Try other words."
	}

	var res string
	for _, t := range newTools {
		res += fmt.Sprintf("%s: %s\n", t.Name, t.Description)
	}
	return res
}

// preselect is what the first step of Run offers: search_tools plus the tools that match the
// message, or every tool when there are no more than PreselectTools of them.
func (a *Agent) preselect(ctx *context.Context, userQuery string) ([]llm.ToolDef, error) {
	all := a.registry.getTools()
	offered := []llm.ToolDef{searchToolsDef}
	if len(all) <= a.cfg.PreselectTools {
		return append(offered, all...), nil
	}
	names, err := a.cfg.ToolIndex.SearchTools(ctx, userQuery, a.cfg.PreselectTools)
	if err != nil {
		return nil, fmt.Errf("agent: preselect tools: %w", err)
	}
	addByName(&offered, all, names)
	return offered, nil
}

// addByName appends to offered the definition in all of each name, in order, skipping names
// that are unknown or already offered, and returns the ones it added.
func addByName(offered *[]llm.ToolDef, all []llm.ToolDef, names []string) []llm.ToolDef {
	var added []llm.ToolDef
	for _, name := range names {
		if isOffered(*offered, name) {
			continue
		}
		for _, t := range all {
			if t.Name == name {
				*offered = append(*offered, t)
				added = append(added, t)
				break
			}
		}
	}
	return added
}
