package mcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/arandu-io/framework/security"
)

// Server is what a client connects to: a name, and what it can do.
//
// The three lists are slices rather than a registry somebody appends to at boot.
// A tool that exists and is not reachable is then visible in one file, which is
// the same reason bootstrap/app.go is a list rather than a container.
type Server struct {
	// Name and Version identify the server to the client.
	Name    string
	Version string
	// Instructions are read by the model before anything else, and they are the
	// place to say what this application is for. A server whose instructions are
	// empty is a server the model guesses about.
	Instructions string

	Tools     []Tool
	Resources []Resource
	Prompts   []Prompt
}

// Tool finds one by name.
func (s *Server) Tool(name string) (Tool, bool) {
	for _, t := range s.Tools {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}

// Call runs a tool, as the given subject.
//
// This is the one door. Both transports come through it, so the validation, the
// authorization boundary and the shape of a failure are decided once -- and a
// third transport cannot arrive with its own idea of any of them.
func (s *Server) Call(ctx context.Context, subject security.Subject, name string, args map[string]any) Response {
	tool, ok := s.Tool(name)
	if !ok {
		return Error("there is no tool called %q. %s", name, s.available())
	}

	if args == nil {
		args = map[string]any{}
	}
	if err := tool.Schema().Validate(args); err != nil {
		// A refusal the model can act on, rather than an error the transport
		// turns into a disconnect: it is being told how to call again.
		return Error("%s: %v", name, err)
	}

	out, err := tool.Handle(ctx, Request{Arguments: args, subject: subject})
	if err != nil {
		// A refused authorization is answered as a failure and never as an empty
		// result. A model handed an empty list concludes there is nothing there;
		// a model told it may not, stops.
		return Error("%s: %v", name, err)
	}
	return out
}

// Read returns a resource's content.
func (s *Server) Read(ctx context.Context, subject security.Subject, uri string) Response {
	for _, r := range s.Resources {
		if r.URI() == uri {
			out, err := r.Read(ctx, subject)
			if err != nil {
				return Error("%s: %v", uri, err)
			}
			return out
		}
	}
	return Error("there is no resource at %q", uri)
}

// available lists the tool names, for the message a wrong one produces.
//
// Listing them is the difference between a model retrying the same wrong name
// and a model picking the right one on the next call.
func (s *Server) available() string {
	if len(s.Tools) == 0 {
		return "This server has no tools."
	}
	names := make([]string, 0, len(s.Tools))
	for _, t := range s.Tools {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	return fmt.Sprintf("Available: %v", names)
}

// Validate reports what is wrong with the server itself.
//
// It runs at boot rather than at the first call, because everything it checks is
// a mistake in a declaration: two tools with one name, a tool with no
// description, a schema field nobody named. A server that starts and answers
// nonsense is worse than one that refuses to start.
func (s *Server) Validate() error {
	var problems []string

	if s.Name == "" {
		problems = append(problems, "the server has no name")
	}

	seen := map[string]bool{}
	for _, t := range s.Tools {
		switch {
		case t.Name() == "":
			problems = append(problems, "a tool has no name")
		case seen[t.Name()]:
			problems = append(problems, fmt.Sprintf("two tools are called %q", t.Name()))
		default:
			seen[t.Name()] = true
		}
		if t.Description() == "" {
			problems = append(problems, fmt.Sprintf("%s has no description: it is what the model reads to "+
				"decide whether to call it, and a tool without one is called at random", t.Name()))
		}
	}

	uris := map[string]bool{}
	for _, r := range s.Resources {
		if r.URI() == "" {
			problems = append(problems, "a resource has no URI")
			continue
		}
		if uris[r.URI()] {
			problems = append(problems, fmt.Sprintf("two resources are at %q", r.URI()))
		}
		uris[r.URI()] = true
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("mcp: %v", problems)
	}
	return nil
}
