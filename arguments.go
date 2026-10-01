package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/json"
	"webtyp.com/llm"
	"webtyp.com/model"
)

type schemaObject struct{ properties string }

func (s *schemaObject) IsNil() bool { return s == nil }
func (s *schemaObject) DecodeFields(r model.FieldReader) {
	if v, ok := r.Raw("properties"); ok {
		s.properties = v
	}
}

type schemaProperty struct {
	typ  string
	enum []string
}

func (p *schemaProperty) IsNil() bool { return p == nil }
func (p *schemaProperty) DecodeFields(r model.FieldReader) {
	p.typ, _ = r.String("type")
	if arr, ok := r.Array("enum"); ok {
		for i := 0; i < arr.Len(); i++ {
			p.enum = append(p.enum, arr.String(i))
		}
	}
}

type propertiesObject struct {
	names []string
	props []schemaProperty
}

func (o *propertiesObject) IsNil() bool { return o == nil }
func (o *propertiesObject) DecodeFields(r model.FieldReader) {
	for i, n := range o.names {
		r.Object(n, &o.props[i])
	}
}

type argsObject struct{ names, values []string }

func (o *argsObject) IsNil() bool { return o == nil }
func (o *argsObject) EncodeFields(w model.FieldWriter) {
	for i := range o.names {
		w.String(o.names[i], o.values[i])
	}
}

// arguments builds the JSON arguments of a call to tool from its InputSchema: a property with an
// enum is chosen by the decision model, a string property gets the whole message (a search tool
// reads it as its query), and any other property is left out.
func (a *Agent) arguments(ctx *context.Context, msg string, tool llm.ToolDef) (string, error) {
	var s schemaObject
	if err := json.Decode(tool.InputSchema, &s); err != nil {
		return "", fmt.Errf(errInputSchema, tool.Name, err)
	}
	if s.properties == "" {
		return "{}", nil
	}

	names, err := json.Keys(s.properties)
	if err != nil {
		return "", fmt.Errf(errInputSchema, tool.Name, err)
	}

	propsObj := propertiesObject{names: names, props: make([]schemaProperty, len(names))}
	if err := json.Decode(s.properties, &propsObj); err != nil {
		return "", fmt.Errf(errInputSchema, tool.Name, err)
	}

	outNames := make([]string, 0, len(names))
	outValues := make([]string, 0, len(names))

	for i, name := range names {
		p := propsObj.props[i]
		if len(p.enum) == 1 {
			outNames = append(outNames, name)
			outValues = append(outValues, p.enum[0])
		} else if len(p.enum) >= 2 && len(p.enum) <= 10 {
			val, err := a.enumValue(ctx, msg, name, p.enum)
			if err != nil {
				return "", err
			}
			outNames = append(outNames, name)
			outValues = append(outValues, val)
		} else if p.typ == "string" {
			outNames = append(outNames, name)
			outValues = append(outValues, msg)
		}
	}

	if len(outNames) == 0 {
		return "{}", nil
	}

	var out string
	if err := json.Encode(&argsObject{names: outNames, values: outValues}, &out); err != nil {
		return "", fmt.Errf(errInputSchema, tool.Name, err)
	}
	return out, nil
}
