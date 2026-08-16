// Package mcp exposes an Arandu application to an AI client.
//
// The Model Context Protocol is how an assistant reaches a program: the program
// declares tools it can call, resources it can read and prompts it can use, and
// the client picks. This package is the Arandu side of that.
//
// # Every tool carries a Grant
//
// A tool reaches data, and every path to data in this framework carries a
// security.Grant. So does this one.
//
//	func (t Invoices) Handle(ctx context.Context, r mcp.Request) (mcp.Response, error) {
//		found, err := t.svc.List(ctx, r.Subject(), data.Query{Limit: 20})
//		…
//	}
//
// The Subject is on the Request and there is no way to call a service without
// one. That is not politeness -- an MCP server is a program that hands a
// language model the keys to an application, and the version of this package
// where a tool queries the database directly would be the largest hole this
// project could ship. A policy that refuses a tool refuses it for the same
// reason it refuses a controller.
//
// Where the Subject comes from is the transport's answer, and the two are
// deliberately different:
//
//	Web    from the session, exactly like an HTTP request. A remote client is
//	       somebody signed in, or it is nobody.
//	Local  from configuration, over stdio. There is no session on a pipe, so the
//	       identity is declared when the server is registered and is visible in
//	       routes/ai.go rather than assumed.
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
	"fmt"

	"github.com/arandu-io/framework/security"
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
	subject security.Subject
}

// Subject is who is asking, for the service call the tool is about to make.
func (r Request) Subject() security.Subject { return r.subject }

// String reads a string argument, and reports whether it was there at all.
func (r Request) String(name string) (string, bool) {
	v, ok := r.Arguments[name].(string)
	return v, ok
}

// Int reads a number. JSON has one numeric type and it decodes as float64, so
// this is where that stops being the caller's problem.
func (r Request) Int(name string) (int, bool) {
	switch v := r.Arguments[name].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
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
	Read(ctx context.Context, s security.Subject) (Response, error)
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

// Message is one turn of a prompt.
type Message struct {
	// Role is "user" or "assistant".
	Role string
	Text string
}

// User and Assistant build a message.
func User(text string) Message      { return Message{Role: "user", Text: text} }
func Assistant(text string) Message { return Message{Role: "assistant", Text: text} }
