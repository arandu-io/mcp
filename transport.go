package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/exception"
	hhttp "github.com/arandu-io/hesape/http"
	hlog "github.com/arandu-io/hesape/log"
)

// The two transports, and the difference between them is who is asking.
//
// It is the whole security story of this package, so it is stated once here
// rather than implied twice below.
//
//	Web    a remote client, over HTTP, identified by the subject the guard in
//	       front of the route put on the request -- a session or a bearer
//	       token, exactly like every other request this application answers.
//	       A request that carries none is refused, and never served as anybody.
//	Local  a process on the same machine, over a pipe. There is no session on a
//	       pipe, so the identity is declared where the server is started and is
//	       visible where a reviewer reads it.
//
// The local one is the one to be careful with, and it is careful on purpose: it
// takes a Subject rather than defaulting to one, so an application that wants an
// assistant to act as an administrator has written that down somewhere a
// reviewer reads.

// MaxMessage is the largest message either transport reads, in bytes.
//
// A message is held whole before it can be parsed, so without a bound the
// process holds whatever the other end sends -- and sending is the cheap half of
// that exchange. The number is the same on both transports: a message one
// accepts and the other refuses is a message whose fate depends on how it
// arrived, which is the hardest kind of report to act on.
const MaxMessage = 1 << 20

// Web mounts the server on a route.
//
// It serves the subject the middleware in front of the route put on the
// request, read with Context.User exactly as a controller reads it: the one a
// session guard loaded, or the one a bearer-token guard resolved. Web loads no
// session and resolves no token, so it takes neither a store nor a resolver.
//
// A request that carries no subject is refused with 401 and
// WWW-Authenticate: Bearer before its body is read. The refusal is returned as
// an error, so the router answers it the way it answers every status an action
// returns: a problem document to a client that asked for JSON, the status and
// its sentence to anything else. A client is never served as a guest it did not
// ask to be: who an anonymous caller is, when an application has one, is
// declared by the middleware that puts that subject on the request.
func Web(s *Server) func(*hhttp.Context) error {
	return func(ctx *hhttp.Context) error {
		// From the request, never from the body. A client that could name its
		// own subject is a client that could name anybody's.
		subject, ok := ctx.User()
		if !ok {
			return unauthenticated()
		}

		body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, MaxMessage+1))
		if err != nil {
			return ctx.Status(http.StatusBadRequest)
		}
		if len(body) > MaxMessage {
			// One byte past the limit is read so that too large can be told from
			// exactly large enough. Parsing the prefix instead would answer
			// "the message is not JSON" about a message that was JSON, and send
			// whoever reads that to look at their encoder.
			return ctx.Status(http.StatusRequestEntityTooLarge)
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

// unauthenticated is the refusal of a request that reached Web carrying no
// subject.
//
// It is built per request rather than held in a variable, because it carries a
// header map and a map shared between requests is one any of them can change.
// The challenge is Bearer because a token is the credential a client that is
// not a browser holds; a browser with a session never gets here, since the
// session guard turns it away first.
func unauthenticated() error {
	headers := http.Header{}
	headers.Set("WWW-Authenticate", "Bearer")
	return &exception.HTTPError{
		Status:  http.StatusUnauthorized,
		Message: "this server answers a request that carries a signed-in subject",
		Headers: headers,
	}
}

// Local serves the server over stdin and stdout.
//
// It is what a client on the same machine starts.
// One message per line, which is how the protocol frames itself on a pipe, and
// no message longer than MaxMessage.
//
// Nothing is ever written to stdout except an answer. A log line there is a
// parse error at the client, and it is the most common way a stdio server
// appears broken while working -- so the logger is the framework's, which writes
// to stderr.
//
// Cancelling the context ends the serve, including from inside the read it
// spends its life in. Reaching that read means closing it, so a reader that can
// be closed is closed on the way out: the alternative is a shutdown that waits
// for an EOF, and a peer that has hung up, crashed or gone quiet never sends
// one. A reader that is not a Closer cannot be interrupted, and there the serve
// still ends at the next message boundary.
func Local(ctx context.Context, s *Server, subject auth.Subject, in io.Reader, out io.Writer) error {
	if err := s.Validate(); err != nil {
		return err
	}

	// The watcher ends with the serve, whichever way the serve ends. Without
	// the second case it would outlive every serve that finished on its own,
	// which is a goroutine per connection held by a context nobody will cancel.
	if closer, ok := in.(io.Closer); ok {
		finished := make(chan struct{})
		defer close(finished)
		go func() {
			select {
			case <-ctx.Done():
				_ = closer.Close()
			case <-finished:
			}
		}()
	}

	reader := bufio.NewReader(in)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, oversized, err := readLine(reader, MaxMessage)

		if err != nil && ctx.Err() != nil {
			// The read ended because the shutdown above closed it, not because
			// the peer said anything. What is reported is the cancellation:
			// a closed file is what this function did, and naming it would send
			// whoever reads the error looking for a broken pipe.
			return ctx.Err()
		}

		switch {
		case oversized:
			if _, werr := fmt.Fprintf(out, "%s\n", tooLong()); werr != nil {
				return werr
			}
		case len(bytes.TrimSpace(line)) == 0:
			// A blank line carries no message, and answering one turns a byte
			// into an answer -- which is a stream of newlines turning into as
			// much output as the other end cares to ask for.
		default:
			if answer := s.Handle(ctx, subject, line); answer != nil {
				if _, werr := fmt.Fprintf(out, "%s\n", answer); werr != nil {
					return werr
				}
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

// readLine reads one newline-terminated message, and reports that it refused one
// longer than limit instead of returning it.
//
// bufio.Reader.ReadBytes grows a buffer until it finds the byte it was asked
// for, which hands whoever is writing the choice of how much memory this process
// uses: not sending a newline costs the writer nothing. This keeps a bounded
// prefix and then discards the rest of the over-long line through the reader's
// own buffer, so a line of any length costs the same once the limit is passed.
// Either way the stream is left at the start of the next message, because a
// reader that gives up mid-line reads the remains of one message as many.
func readLine(r *bufio.Reader, limit int) ([]byte, bool, error) {
	var line []byte
	oversized := false

	for {
		chunk, err := r.ReadSlice('\n')
		if !oversized {
			if len(line)+len(chunk) > limit {
				// What was read so far is dropped rather than kept: a message
				// that will not be parsed is worth no memory at all.
				line, oversized = nil, true
			} else {
				// ReadSlice returns the reader's own buffer, which the next read
				// overwrites, so this copy is what makes the message outlive it.
				line = append(line, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, oversized, err
	}
}

// Start is Local over the process's own stdin and stdout.
func Start(ctx context.Context, s *Server, subject auth.Subject) error {
	hlog.For(ctx).Info("mcp: serving over stdio",
		"server", s.Name, "tools", len(s.Tools), "subject", subject.ID)
	return Local(ctx, s, subject, os.Stdin, os.Stdout)
}

// Describe prints what a server offers to out.
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
