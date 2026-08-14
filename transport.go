package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/observability"
	"github.com/arandu-io/framework/security"
)

// The two transports, and the difference between them is who is asking.
//
// It is the whole security story of this package, so it is stated once here
// rather than implied twice below.
//
//	Web    a remote client, over HTTP, identified by the session it carries --
//	       exactly like every other request this application answers. No session
//	       is no subject, and no subject reaches no repository.
//	Local  a process on the same machine, over a pipe. There is no session on a
//	       pipe, so the identity is declared where the server is registered and
//	       is visible in routes/ai.go.
//
// The local one is the one to be careful with, and it is careful on purpose: it
// takes a Subject rather than defaulting to one, so an application that wants an
// assistant to act as an administrator has written that down somewhere a
// reviewer reads.

// Web mounts the server on a route.
//
// The subject comes from the session. A client with none is a guest, and what a
// guest may do is the policy's answer -- the same answer a browser would get.
func Web(s *Server, sessions *security.SessionStore, tenant string) func(*fhttp.Context) error {
	return func(ctx *fhttp.Context) error {
		body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, 1<<20))
		if err != nil {
			return ctx.Status(http.StatusBadRequest)
		}

		// From the session, never from the body. A client that could name its
		// own subject is a client that could name anybody's.
		subject, err := sessions.Load(ctx.Ctx(), ctx.Request)
		if err != nil {
			subject = security.Guest(tenant)
		}

		answer := s.Handle(ctx.Ctx(), subject, body)
		if answer == nil {
			// A notification. 202 rather than 200 with an empty body, so a
			// client can tell "nothing to say" from "an empty answer".
			return ctx.Status(http.StatusAccepted)
		}

		ctx.Response.Header().Set("Content-Type", "application/json")
		_, err = ctx.Response.Write(answer)
		return err
	}
}

// Local serves the server over stdin and stdout.
//
// It is what an assistant on the same machine starts, and it is `aru mcp:start`.
// One message per line, which is how the protocol frames itself on a pipe.
//
// Nothing is ever written to stdout except an answer. A log line there is a
// parse error at the client, and it is the most common way a stdio server
// appears broken while working -- so the logger is the framework's, which writes
// to stderr.
func Local(ctx context.Context, s *Server, subject security.Subject, in io.Reader, out io.Writer) error {
	if err := s.Validate(); err != nil {
		return err
	}

	reader := bufio.NewReader(in)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadBytes('\n')
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			continue
		}

		answer := s.Handle(ctx, subject, line)
		if answer != nil {
			if _, werr := fmt.Fprintf(out, "%s\n", answer); werr != nil {
				return werr
			}
		}

		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// Start is Local over the process's own stdin and stdout.
func Start(ctx context.Context, s *Server, subject security.Subject) error {
	observability.Log(ctx).Info("mcp: serving over stdio",
		"server", s.Name, "tools", len(s.Tools), "subject", subject.ID)
	return Local(ctx, s, subject, os.Stdin, os.Stdout)
}

// Describe prints what a server offers, for `aru mcp:list`.
//
// It exists because the alternative is connecting a client to find out, and the
// question "what can this thing do" is asked far more often than it is answered
// by an assistant.
func Describe(s *Server, out io.Writer) {
	fmt.Fprintf(out, "%s %s\n", s.Name, s.Version)
	if s.Instructions != "" {
		fmt.Fprintf(out, "\n%s\n", s.Instructions)
	}

	fmt.Fprintf(out, "\ntools (%d)\n", len(s.Tools))
	for _, t := range s.Tools {
		fmt.Fprintf(out, "  %-24s %s\n", t.Name(), t.Description())
		schema, _ := json.Marshal(t.Schema().JSON()["properties"])
		fmt.Fprintf(out, "  %-24s %s\n", "", schema)
	}

	if len(s.Resources) > 0 {
		fmt.Fprintf(out, "\nresources (%d)\n", len(s.Resources))
		for _, r := range s.Resources {
			fmt.Fprintf(out, "  %-24s %s\n", r.URI(), r.Description())
		}
	}
	if len(s.Prompts) > 0 {
		fmt.Fprintf(out, "\nprompts (%d)\n", len(s.Prompts))
		for _, p := range s.Prompts {
			fmt.Fprintf(out, "  %-24s %s\n", p.Name(), p.Description())
		}
	}
}
