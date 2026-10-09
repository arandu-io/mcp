// What reaches the process's own stdout when Start serves it.
//
// stdout is the protocol channel on stdio: every byte there is read by the
// client as part of a message, so a log line there is a message the client
// cannot parse, and the strict ones hang up. The framework's root logger writes
// to stdout, and an application's context carries it. These tests serve the
// process's real stdin and stdout, with that logger on the context, and read
// what came out of each stream.

package feature_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	hlog "github.com/arandu-io/hesape/log"

	"github.com/arandu-io/mcp"
)

// chatty is a tool that logs through the context it was handed, which is how
// a service under a tool logs.
type chatty struct{}

func (chatty) Name() string        { return "list_posts" }
func (chatty) Description() string { return "Lists the posts of this blog." }
func (chatty) Schema() mcp.Schema  { return mcp.Object() }

func (chatty) Handle(ctx context.Context, _ mcp.Request) (mcp.Response, error) {
	hlog.For(ctx).Info("list_posts ran")
	return mcp.Text("one post"), nil
}

// served runs Start over the process's own streams with the given messages on
// stdin and the root logger on the context, and returns what came out of
// stdout and stderr.
//
// The root logger is built after stdout is swapped, which is what an
// application's is: hlog.New writes to the os.Stdout it finds, and an
// application builds it at boot, on the stdout the client is reading.
func served(t *testing.T, messages ...string) (stdout, stderr string) {
	t.Helper()

	inR, inW := pipe(t)
	outR, outW := pipe(t)
	errR, errW := pipe(t)

	stdin, stdoutWas, stderrWas := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = inR, outW, errW
	t.Cleanup(func() { os.Stdin, os.Stdout, os.Stderr = stdin, stdoutWas, stderrWas })

	ctx := hlog.Into(context.Background(), hlog.New("production", slog.LevelInfo))
	server := &mcp.Server{Name: "blog", Version: "1.0.0", Tools: []mcp.Tool{chatty{}}}

	gotOut, gotErr := drain(outR), drain(errR)

	if _, err := inW.WriteString(strings.Join(messages, "\n") + "\n"); err != nil {
		t.Fatalf("writing to stdin: %v", err)
	}
	_ = inW.Close()

	if err := mcp.Start(ctx, server, auth.Subject{ID: "assistant", Tenant: "t1"}); err != nil {
		t.Fatalf("serving over stdio: %v", err)
	}
	_ = outW.Close()
	_ = errW.Close()

	return string(<-gotOut), string(<-gotErr)
}

// TestNothingButAnswersReachesStdout.
//
// Measured before the change: Start logged "mcp: serving over stdio" through
// the context's logger, and with the root logger on the context that line went
// to stdout as a JSON object ahead of the first answer -- a message with no
// jsonrpc member and no id, which a client reads as a broken server. A tool
// logging through its context put a second one beside its own answer.
func TestNothingButAnswersReachesStdout(t *testing.T) {
	stdout, stderr := served(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_posts","arguments":{}}}`,
	)

	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("two requests produced %d lines on stdout:\n%s", len(lines), stdout)
	}
	for _, line := range lines {
		var answer struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &answer); err != nil || answer.JSONRPC != "2.0" || len(answer.ID) == 0 {
			t.Errorf("stdout carried a line that is not a JSON-RPC answer: %s", line)
		}
	}

	// The lines went somewhere, and it is the stream a client shows a person.
	for _, want := range []string{"mcp: serving over stdio", "list_posts ran"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not carry %q:\n%s", want, stderr)
		}
	}
}

// pipe returns an os.Pipe, closed when the test ends.
func pipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("opening a pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	return r, w
}

// drain reads r to its end in the background, so a writer is never blocked on a
// full pipe, and delivers what it read.
func drain(r io.Reader) <-chan []byte {
	done := make(chan []byte, 1)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		done <- b.Bytes()
	}()
	return done
}
