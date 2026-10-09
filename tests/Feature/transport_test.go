package feature

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"

	"github.com/arandu-io/mcp"
	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// The limits, measured rather than read.
//
// A fuzz target finds the input that breaks an invariant, and it finds it out of
// inputs small enough to mutate quickly. The size of a message is not that kind
// of question: the answer is the same for every input and it only shows up at a
// scale the fuzzer never reaches, so it is asked here, once, with a big one.

// announced is a writer that reports each write on a channel, so a test can
// wait for the serve loop to have answered before doing anything else. Waiting
// on the answer is what puts the loop where the test needs it: blocked inside
// the next read, with nothing coming.
type announced struct{ wrote chan struct{} }

// Write reports the write and keeps the bytes for nobody.
func (w announced) Write(p []byte) (int, error) {
	select {
	case w.wrote <- struct{}{}:
	default:
	}
	return len(p), nil
}

// TestCancellingTheContextEndsAServeBlockedInARead.
//
// The loop asked whether it should stop between messages, which is the one
// moment it is never in when it matters: a stdio server spends its life blocked
// in a read with nothing coming. Cancelling there did nothing at all, and the
// serve ended when the other end sent EOF -- which a peer that has hung up,
// crashed or simply gone quiet never does. What shuts down is then the process,
// by whatever is impatient enough to kill it.
func TestCancellingTheContextEndsAServeBlockedInARead(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("opening a pipe: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := announced{wrote: make(chan struct{}, 4)}
	done := make(chan error, 1)
	go func() {
		done <- mcp.Local(ctx, helpers.Everything(), auth.Subject{ID: "u1", Tenant: "t1"}, reader, out)
	}()

	// One message through, so the loop is known to be past its own start and
	// waiting on the read rather than on anything this test still holds.
	if _, err := writer.WriteString(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"); err != nil {
		t.Fatalf("writing to the pipe: %v", err)
	}
	select {
	case <-out.wrote:
	case <-time.After(5 * time.Second):
		t.Fatal("the serve never answered the first message")
	}

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a cancelled serve ended with %v, want the cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled serve was still waiting inside the read: nothing but EOF ends it, " +
			"and a peer that has gone quiet never sends one")
	}
}

// TestACancelledServeLeavesNothingRunning.
//
// Interrupting the read is the first half. The second is that whatever does the
// interrupting is gone afterwards: a watcher per serve that outlives its serve
// is a leak that only shows up in a process that opens many, which is the one
// place it is hardest to find.
func TestACancelledServeLeavesNothingRunning(t *testing.T) {
	before := runtime.NumGoroutine()

	for range 20 {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("opening a pipe: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- mcp.Local(ctx, helpers.Everything(), auth.Subject{ID: "u1"}, reader, io.Discard)
		}()

		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("a cancelled serve did not end")
		}
		_ = writer.Close()
		_ = reader.Close()
	}

	// The goroutines being counted are other tests' as well, so the check is
	// that the count comes back rather than that it never moved.
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("20 cancelled serves left the goroutine count at %d, from %d", after, before)
	}
}

// TestAStreamThatEndsIsNotACancellation, so interrupting the read is not a way
// to report every shutdown as one.
func TestAStreamThatEndsIsNotACancellation(t *testing.T) {
	var out bytes.Buffer
	err := mcp.Local(context.Background(), helpers.Everything(), auth.Subject{ID: "u1"},
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"), &out)

	if err != nil {
		t.Fatalf("a stream that ended reported %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("the message before the end of the stream was not answered")
	}
}

// TestALineIsNotReadIntoUnboundedMemory.
//
// The peer decides when to send a newline. If nothing bounds the wait, the peer
// also decides how much memory this process holds, and it decides it by sending
// bytes and never stopping -- which costs the peer nothing and costs everything
// on this side of the pipe.
func TestALineIsNotReadIntoUnboundedMemory(t *testing.T) {
	const stream = 64 << 20
	const allowed = 8 << 20

	in := bytes.NewReader(bytes.Repeat([]byte("a"), stream))
	var out bytes.Buffer

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if err := mcp.Local(context.Background(), helpers.Everything(), auth.Subject{ID: "u1"}, in, &out); err != nil {
		t.Fatalf("reading the stream failed: %v", err)
	}
	runtime.ReadMemStats(&after)

	if used := after.TotalAlloc - before.TotalAlloc; used > allowed {
		t.Fatalf("a %d byte line with no newline in it allocated %d bytes, over the %d allowed: "+
			"the peer chooses how much memory this process uses", stream, used, allowed)
	}
}

// TestABlankLineIsNotAMessage.
//
// A newline on its own carries nothing to answer. Answering it anyway turns a
// byte into an answer, and a stream of them into as much output as the peer
// asks for.
func TestABlankLineIsNotAMessage(t *testing.T) {
	const blanks = 64 << 10

	in := strings.NewReader(strings.Repeat("\n", blanks) + strings.Repeat(" \t\r\n", blanks))
	var out bytes.Buffer
	if err := mcp.Local(context.Background(), helpers.Everything(), auth.Subject{ID: "u1"}, in, &out); err != nil {
		t.Fatalf("reading the stream failed: %v", err)
	}

	if out.Len() != 0 {
		t.Fatalf("%d blank lines were answered with %d bytes", 2*blanks, out.Len())
	}
}

// TestAnOversizedMessageIsRefusedAndTheStreamResyncs.
//
// Refusing the message is half of it. The other half is finding the next one:
// a reader that gives up in the middle of a line reads the rest of it as
// messages, and one refusal becomes thousands.
func TestAnOversizedMessageIsRefusedAndTheStreamResyncs(t *testing.T) {
	var stream bytes.Buffer
	stream.WriteString(`{"jsonrpc":"2.0","id":1,"method":"ping","params":"`)
	stream.Write(bytes.Repeat([]byte("a"), 8<<20))
	stream.WriteString("\"}\n")
	stream.WriteString(`{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\n")

	var out bytes.Buffer
	if err := mcp.Local(context.Background(), helpers.Everything(), auth.Subject{ID: "u1"}, &stream, &out); err != nil {
		t.Fatalf("reading the stream failed: %v", err)
	}

	lines := bytes.Split(bytes.TrimSuffix(out.Bytes(), []byte("\n")), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("an oversized message and a good one produced %d answers: %s", len(lines), out.Bytes())
	}

	var refusal, good helpers.AnswerShape
	if err := json.Unmarshal(lines[0], &refusal); err != nil {
		t.Fatalf("the refusal is not a response: %v", err)
	}
	if refusal.Error == nil {
		t.Fatalf("the oversized message was answered with a result: %s", lines[0])
	}
	if err := json.Unmarshal(lines[1], &good); err != nil {
		t.Fatalf("the answer after the refusal is not a response: %v", err)
	}
	if good.Error != nil || string(good.ID) != "2" {
		t.Fatalf("the message after the oversized one was not answered on its own terms: %s", lines[1])
	}
}

// TestABodyOverTheLimitIsRefusedRatherThanTruncated.
//
// Reading a bounded prefix and parsing it reports the wrong failure: the client
// sent JSON, the server cut it in half, and the answer says the client sent
// something that is not JSON. The person reading that goes looking at their
// encoder.
func TestABodyOverTheLimitIsRefusedRatherThanTruncated(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"ping","params":"` +
		strings.Repeat("a", 2<<20) + `"}`

	rec := helpers.Post(body)

	if rec.Code == http.StatusOK {
		answered, _ := io.ReadAll(rec.Body)
		t.Fatalf("a body over the limit was cut short and parsed, and answered %s", answered)
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a body over the limit was answered %d, want %d",
			rec.Code, http.StatusRequestEntityTooLarge)
	}
}

// TestAMessageWithinTheLimitStillArrives, so the limit is not a way to refuse
// everything large.
func TestAMessageWithinTheLimitStillArrives(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"note":"` +
		strings.Repeat("a", 512<<10) + `"}}`

	rec := helpers.Post(body)

	if rec.Code != http.StatusOK {
		t.Fatalf("a message inside the limit was answered %d", rec.Code)
	}
	var out helpers.AnswerShape
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the answer is not a response: %v", err)
	}
	if out.Error != nil {
		t.Fatalf("a message inside the limit was refused: %s", rec.Body.Bytes())
	}
}

// TestALargeMessageOverAPipeStillArrives, so the bound on a line is not a way to
// refuse every message that is not small.
func TestALargeMessageOverAPipeStillArrives(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"note":"` +
		strings.Repeat("a", 512<<10) + `"}}` + "\n")

	var out bytes.Buffer
	if err := mcp.Local(context.Background(), helpers.Everything(), auth.Subject{ID: "u1"}, in, &out); err != nil {
		t.Fatalf("reading the stream failed: %v", err)
	}

	var answer helpers.AnswerShape
	if err := json.Unmarshal(bytes.TrimSuffix(out.Bytes(), []byte("\n")), &answer); err != nil {
		t.Fatalf("the answer is not a response: %v, %s", err, out.Bytes())
	}
	if answer.Error != nil {
		t.Fatalf("a message inside the limit was refused: %s", out.Bytes())
	}
}
