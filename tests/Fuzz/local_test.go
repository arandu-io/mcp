package fuzz

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/arandu-io/framework/security"

	"github.com/arandu-io/mcp"
	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// FuzzLocal drives the framing: a byte stream in, a stream of answers out.
//
// It is the half a client does not write and cannot see. A reader that keeps
// reading until it finds a newline lets whoever is on the other end of the pipe
// choose how much memory this process uses, and a reader that answers something
// that was never a message turns one byte into a hundred.
func FuzzLocal(f *testing.F) {
	seeds := []string{
		"",
		"\n",
		"\n\n\n\n",
		"   \n",
		"x",
		"x\n",
		"{}\n",
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\n",
		"{\"jsonrpc\":\"2.0\",\"method\":\"ping\"}\n",
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n",
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}",
		"\r\n{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\r\n",
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"list_posts\"}}\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	s := helpers.Everything()
	who := security.Subject{ID: "u1", Tenant: "t1"}

	f.Fuzz(func(t *testing.T, stream []byte) {
		var out bytes.Buffer
		if err := mcp.Local(context.Background(), s, who, bytes.NewReader(stream), &out); err != nil {
			t.Fatalf("reading a stream of %d bytes failed: %v", len(stream), err)
		}

		answers := 0
		for line := range bytes.SplitSeq(bytes.TrimSuffix(out.Bytes(), []byte("\n")), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			answers++

			var a helpers.AnswerShape
			if err := json.Unmarshal(line, &a); err != nil {
				t.Fatalf("an answer is not a JSON-RPC response: %v, %s", err, line)
			}
			if a.JSONRPC != "2.0" {
				t.Fatalf("an answer does not name the protocol: %s", line)
			}
			if a.ID == nil {
				t.Fatalf("an answer carries no id: %s", line)
			}
		}

		// At most one answer per line, because a line is what a message is.
		if lines := bytes.Count(stream, []byte("\n")) + 1; answers > lines {
			t.Fatalf("%d bytes in %d lines produced %d answers", len(stream), lines, answers)
		}

		// A pipe that answers more than it is told is a pipe that fills whatever
		// is on the other side of it.
		if limit := 128*len(stream) + 128; out.Len() > limit {
			t.Fatalf("%d bytes in produced %d bytes out, over the %d allowed",
				len(stream), out.Len(), limit)
		}
	})
}
