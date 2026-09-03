package mcp

import (
	"fmt"
	"math"
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
		if _, ok := wholeNumber(v); !ok {
			return fmt.Sprintf("%s must be a whole number this server can carry", f.name)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Sprintf("%s must be true or false", f.name)
		}
	}
	return ""
}

// wholeNumber reads a decoded argument as an integer, and reports whether the
// value it carried is one at all.
//
// JSON carries one numeric type and it decodes as float64, so 1.9 and 1e100
// arrive as perfectly good numbers and a check that asks only whether something
// is a number lets both through. What happens next is not a rounding: the
// conversion turns 1.9 into 1, and a value outside the range of an int into
// something the Go specification leaves to the implementation -- measured as the
// largest int on one machine, and free to differ on the next. Either way the
// tool acts on a number nobody sent, and nothing downstream can tell.
//
// The int case is for a caller that built the map in Go rather than from a
// message.
func wholeNumber(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		// 2^63 is the first value past the range of an int64 and is exactly
		// representable as a float64, which the largest int64 is not: that one
		// rounds up to this. Comparing against the exact bound is what makes
		// the check mean what it says at the boundary, and it is also what
		// refuses an infinity, while a NaN fails the equality above it.
		const beyondInt64 = 9223372036854775808.0
		if n != math.Trunc(n) || n < -beyondInt64 || n >= beyondInt64 {
			return 0, false
		}

		// int is not int64 everywhere, and the range this server can carry is
		// the one the program it is part of can.
		wide := int64(n)
		narrowed := int(wide)
		if int64(narrowed) != wide {
			return 0, false
		}
		return narrowed, true
	}
	return 0, false
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
