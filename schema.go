package mcp

import (
	"fmt"
	"sort"
	"strings"
)

// Schema declares what a tool takes.
//
// It is a small typed builder rather than a map or a struct tag, for the reason
// the rest of this framework prefers a signature to a convention: a schema
// written as JSON in a string is a schema nothing checks, and the first time it
// is wrong the model sends an argument the tool ignores.
//
//	mcp.Object(
//		mcp.String("slug", "The post to read").Required(),
//		mcp.Int("limit", "How many to return"),
//	)
type Schema struct {
	fields []field
}

type field struct {
	name        string
	kind        string
	description string
	required    bool
	enum        []string
}

// Object builds a schema from its fields.
func Object(fields ...Field) Schema {
	s := Schema{}
	for _, f := range fields {
		s.fields = append(s.fields, f.field)
	}
	return s
}

// Field is one declared argument.
type Field struct{ field field }

// String, Int and Bool declare an argument of that type.
func String(name, description string) Field {
	return Field{field{name: name, kind: "string", description: description}}
}
func Int(name, description string) Field {
	return Field{field{name: name, kind: "integer", description: description}}
}
func Bool(name, description string) Field {
	return Field{field{name: name, kind: "boolean", description: description}}
}

// Required marks the argument as mandatory. A call without it is refused before
// the tool runs.
func (f Field) Required() Field { f.field.required = true; return f }

// Enum limits a string to a set. It is worth reaching for: a model given a
// closed list picks from it, and a model given "the status" invents one.
func (f Field) Enum(values ...string) Field { f.field.enum = values; return f }

// JSON renders the schema the way the protocol carries it.
func (s Schema) JSON() map[string]any {
	properties := map[string]any{}
	var required []string

	for _, f := range s.fields {
		p := map[string]any{"type": f.kind}
		if f.description != "" {
			p["description"] = f.description
		}
		if len(f.enum) > 0 {
			p["enum"] = f.enum
		}
		properties[f.name] = p
		if f.required {
			required = append(required, f.name)
		}
	}
	sort.Strings(required)

	out := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// Validate checks a call's arguments against the schema.
//
// It runs before Handle, so a tool never sees an argument it did not declare or
// a missing one it marked required -- which is what lets a tool read an argument
// without checking, and what stops a model's invented parameter from reaching
// application code.
//
// Every problem is reported at once. A model that is told one mistake per call
// spends three calls on a form it could have filled in on the second.
func (s Schema) Validate(args map[string]any) error {
	var problems []string

	declared := map[string]bool{}
	for _, f := range s.fields {
		declared[f.name] = true
	}

	for _, f := range s.fields {
		v, present := args[f.name]
		if !present {
			if f.required {
				problems = append(problems, fmt.Sprintf("%s is required", f.name))
			}
			continue
		}
		if problem := f.problem(v); problem != "" {
			problems = append(problems, problem)
		}
	}

	// An argument nobody declared is reported rather than ignored: a model that
	// invents one and is not told keeps inventing it.
	for name := range args {
		if !declared[name] {
			problems = append(problems, fmt.Sprintf("%s is not an argument of this tool", name))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// problem reports what is wrong with one value for this field, or the empty
// string if nothing is. It is asked only about a value the call carried, so an
// argument that is absent is the caller's question and not this one's.
func (f field) problem(v any) string {
	switch f.kind {
	case "string":
		text, ok := v.(string)
		switch {
		case !ok:
			return fmt.Sprintf("%s must be a string", f.name)
		case len(f.enum) > 0 && !contains(f.enum, text):
			return fmt.Sprintf("%s must be one of %s", f.name, strings.Join(f.enum, ", "))
		}
	case "integer":
		if !isNumber(v) {
			return fmt.Sprintf("%s must be a number", f.name)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Sprintf("%s must be true or false", f.name)
		}
	}
	return ""
}

// isNumber reports whether a decoded argument is a number. JSON carries one
// numeric type and it decodes as float64; the int is for a caller that built
// the map in Go rather than from a message.
func isNumber(v any) bool {
	switch v.(type) {
	case float64, int:
		return true
	}
	return false
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
