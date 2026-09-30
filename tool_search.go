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

	allTools := a.registry.getTools()
	var newTools []llm.ToolDef

	for _, name := range foundNames {
		var toolDef llm.ToolDef
		found := false
		for _, t := range allTools {
			if t.Name == name {
				toolDef = t
				found = true
				break
			}
		}
		if !found {
			continue
		}

		already := false
		for _, o := range *offered {
			if o.Name == toolDef.Name {
				already = true
				break
			}
		}
		if !already {
			*offered = append(*offered, toolDef)
			newTools = append(newTools, toolDef)
		}
	}

	if len(newTools) == 0 {
		return "No tools matched. Try other words."
	}

	var res string
	for _, t := range newTools {
		res += fmt.Sprintf("%s: %s\n", t.Name, t.Description)
	}
	return res
}

func (a *Agent) preselect(ctx *context.Context, userQuery string) ([]llm.ToolDef, error) {
	all := a.registry.getTools()
	if len(all) <= a.cfg.PreselectTools {
		res := make([]llm.ToolDef, 0, len(all)+1)
		res = append(res, searchToolsDef)
		res = append(res, all...)
		return res, nil
	}

	names, err := a.cfg.ToolIndex.SearchTools(ctx, userQuery, a.cfg.PreselectTools)
	if err != nil {
		return nil, fmt.Errf("agent: preselect tools: %w", err)
	}

	res := make([]llm.ToolDef, 0, len(names)+1)
	res = append(res, searchToolsDef)

	for _, name := range names {
		var toolDef llm.ToolDef
		found := false
		for _, t := range all {
			if t.Name == name {
				toolDef = t
				found = true
				break
			}
		}
		if !found {
			continue
		}

		already := false
		for _, o := range res {
			if o.Name == toolDef.Name {
				already = true
				break
			}
		}
		if !already {
			res = append(res, toolDef)
		}
	}

	return res, nil
}
