package feature

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/arandu-io/framework/security"

	"github.com/arandu-io/mcp"
	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// The limits, measured rather than read.
//
// A fuzz target finds the input that breaks an invariant, and it finds it out of
// inputs small enough to mutate quickly. The size of a message is not that kind
// of question: the answer is the same for every input and it only shows up at a
// scale the fuzzer never reaches, so it is asked here, once, with a big one.

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
	if err := mcp.Local(context.Background(), helpers.Everything(), security.Subject{ID: "u1"}, in, &out); err != nil {
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
	if err := mcp.Local(context.Background(), helpers.Everything(), security.Subject{ID: "u1"}, in, &out); err != nil {
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
	if err := mcp.Local(context.Background(), helpers.Everything(), security.Subject{ID: "u1"}, &stream, &out); err != nil {
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
	if err := mcp.Local(context.Background(), helpers.Everything(), security.Subject{ID: "u1"}, in, &out); err != nil {
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
