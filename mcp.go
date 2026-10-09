// Package mcp exposes an Arandu application to an AI client.
//
// The Model Context Protocol is how an assistant reaches a program: the program
// declares tools it can call, resources it can read and prompts it can use, and
// the client picks. This package is the Arandu side of that.
//
// # What this package checks, and what it does not
//
// A tool reaches data, and a tool is written the way a controller is: it asks a
// service, the service asks a policy, and the auth.Grant the policy issues
// is what the repository signature below it requires.
//
//	func (t Invoices) Handle(ctx context.Context, r mcp.Request) (mcp.Response, error) {
//		found, err := t.svc.List(ctx, r.Subject(), database.Query{Limit: 20})
//		…
//	}
//
// The Server does not verify that a tool did that. It checks the arguments
// against the tool's schema, hands Handle the Subject the transport
// established, and turns an error out of Handle into a failure the model reads.
// It runs no policy of its own: a policy decides about a typed resource, and
// Tool declares neither that type nor an action, so a server holding a slice of
// tools has nothing to ask one about. A tool that skips the service and reads a
// database handle itself is dispatched and answers.
//
// So the boundary is the service, and writing a tool that reaches past it is
// not a style mistake -- an MCP server is a program that hands a language model
// the keys to an application, and the tool that queries the database directly
// is the largest hole this project could ship. A policy that refuses a tool
// refuses it for the same reason it refuses a controller.
//
// What the Server does guarantee is narrower, and worth naming because a caller
// can rely on it: the Subject is an unexported field read through
// Request.Subject, so a tool cannot choose the identity it acts as; arguments
// are checked before Handle runs; and a refusal arrives marked as one rather
// than as an empty result.
//
// Where the Subject comes from is the transport's answer, and the two are
// deliberately different:
//
//	Web    from the request, exactly like a controller: the subject the session
//	       guard or the bearer-token guard in front of the route put there. A
//	       request that carries none is refused with 401.
//	Local  from configuration, over stdio. There is no session on a pipe, so the
//	       identity is declared where the server is started rather than assumed.
//
// # What is deliberately absent
//
// No attributes, because Go has none: a tool's name and description are methods,
// which is more characters and one less mechanism. No facade. No dynamic
// registration -- a server declares its tools in a slice, so a tool that exists
// and is not reachable is visible in one file.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/arandu-io/hesape/auth"
)

// Version is the protocol revision this package speaks.
const Version = "2024-11-05"

// Request is one call from a client.
type Request struct {
	// Arguments are what the client sent, already checked against the tool's
	// schema. Read them with String, Int and Bool rather than by indexing: a
	// missing key is a zero value, and a tool that cannot tell "0" from "absent"
	// is a tool that acts on an argument nobody passed.
	Arguments map[string]any

	// subject is who is asking. It is unexported and read through Subject, so a
	// tool cannot overwrite it -- and a tool that could would be a tool that
	// chooses its own permissions.
	subject auth.Subject
}

// Subject is who is asking, for the service call the tool is about to make.
func (r Request) Subject() auth.Subject { return r.subject }

// String reads a string argument, and reports whether it was there at all.
func (r Request) String(name string) (string, bool) {
	v, ok := r.Arguments[name].(string)
	return v, ok
}

// Int reads a whole number, and reports whether the argument carried one.
//
// JSON has a single numeric type and it decodes as float64, so this is where a
// fraction, an infinity and a value past the range of an int stop being the
// caller's problem. Each of them answers false rather than a converted value: a
// tool told the number is not there asks again, and a tool handed 1 for 1.9 has
// no way to know it should.
func (r Request) Int(name string) (int, bool) {
	return wholeNumber(r.Arguments[name])
}

// Bool reads a boolean.
func (r Request) Bool(name string) (bool, bool) {
	v, ok := r.Arguments[name].(bool)
	return v, ok
}

// Response is what a tool answers with.
type Response struct {
	// Text is what the client shows or feeds to the model.
	Text string
	// IsError marks the answer as a failure the model should react to rather
	// than a result. A refused authorization is one of these: the model should
	// learn it may not, not be handed an empty list and conclude there is
	// nothing there.
	IsError bool
}

// Text is the ordinary answer.
func Text(format string, args ...any) Response {
	return Response{Text: fmt.Sprintf(format, args...)}
}

// Error is an answer the model should treat as a failure.
func Error(format string, args ...any) Response {
	return Response{Text: fmt.Sprintf(format, args...), IsError: true}
}

// JSON is an answer carrying structured data.
//
// Encoded here rather than by the tool, so every tool answers the same shape and
// a marshalling error is one error message instead of one per tool.
func JSON(v any) Response {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return Error("encoding the answer: %v", err)
	}
	return Response{Text: string(body)}
}

// Tool is something a client can call.
type Tool interface {
	// Name is what the client calls it by. Lower case with underscores, because
	// that is what every client displays without quoting.
	Name() string
	// Description is what the model reads to decide whether to call it. It is
	// the single highest-leverage string in this package: a model that calls the
	// wrong tool was told the wrong thing here.
	Description() string
	// Schema declares the arguments. A call whose arguments do not match is
	// refused before Handle runs.
	Schema() Schema
	// Handle does the work. The Grant comes from the Request's Subject, through
	// the service, through the policy -- like everywhere else.
	Handle(ctx context.Context, r Request) (Response, error)
}

// Resource is something a client can read.
type Resource interface {
	// URI addresses it, in a scheme of the application's choosing.
	URI() string
	// Name and Description are what the client lists it as.
	Name() string
	Description() string
	// MimeType is what the content is. Empty means text/plain.
	MimeType() string
	// Read returns the content.
	Read(ctx context.Context, s auth.Subject) (Response, error)
}

// Prompt is a conversation an application knows how to start.
type Prompt interface {
	Name() string
	Description() string
	// Arguments are what the client fills in before the prompt is useful.
	Arguments() []Argument
	// Render builds the messages.
	Render(ctx context.Context, r Request) ([]Message, error)
}

// Argument is one input a prompt takes.
type Argument struct {
	Name        string
	Description string
	Required    bool
}

// checkArguments reports what is wrong with a call's arguments against what the
// prompt declared, or nil if nothing is.
//
// It is what lets Render read an argument it marked required without testing
// whether it arrived: without the check the argument reads as the empty string,
// and the messages a model is about to act on are built from a value nobody
// sent. An argument nobody declared is reported rather than dropped, for the
// reason the tool schema reports one -- a model that invents a parameter and is
// not told keeps inventing it.
//
// Every problem is reported at once and in a fixed order, so two runs of the
// same wrong call produce the same message.
func checkArguments(declared []Argument, args map[string]any) error {
	var problems []string

	known := make(map[string]bool, len(declared))
	for _, a := range declared {
		known[a.Name] = true
		if _, present := args[a.Name]; !present && a.Required {
			problems = append(problems, a.Name+" is required")
		}
	}

	for name := range args {
		if !known[name] {
			problems = append(problems, name+" is not an argument of this prompt")
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// Message is one turn of a prompt.
type Message struct {
	// Role is "user" or "assistant".
	Role string
	Text string
}

// User and Assistant build a message.
func User(text string) Message      { return Message{Role: "user", Text: text} }
func Assistant(text string) Message { return Message{Role: "assistant", Text: text} }
