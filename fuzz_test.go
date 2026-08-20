package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/arandu-io/framework/security"

	"github.com/arandu-io/mcp"
)

// What arrives at this package is written by a program it does not control, so
// the tests below assert about every byte sequence rather than about the ones a
// client is supposed to send. A server that stops answering because a message
// was malformed has stopped answering the client that sent it well-formed ones.

// The declared error codes, repeated here because the package keeps them
// unexported and a test that imported them would be asserting a constant equals
// itself.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInternal       = -32603
)

// answerShape is the response the protocol carries, read back loosely: every
// member is raw, so a test can tell "absent" from "null".
type answerShape struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// fuzzServer carries one of each kind, so a message can reach every branch.
func fuzzServer() *mcp.Server {
	return &mcp.Server{
		Name: "blog", Version: "1.0.0",
		Instructions: "The posts of a blog.",
		Tools:        []mcp.Tool{&posts{}},
		Resources:    []mcp.Resource{readme{}},
		Prompts:      []mcp.Prompt{summarise{}},
	}
}

type readme struct{}

func (readme) URI() string         { return "blog://readme" }
func (readme) Name() string        { return "readme" }
func (readme) Description() string { return "What this blog is." }
func (readme) MimeType() string    { return "" }
func (readme) Read(context.Context, security.Subject) (mcp.Response, error) {
	return mcp.Text("a blog"), nil
}

type summarise struct{}

func (summarise) Name() string        { return "summarise" }
func (summarise) Description() string { return "Summarises a post." }
func (summarise) Arguments() []mcp.Argument {
	return []mcp.Argument{{Name: "slug", Description: "The post", Required: true}}
}
func (summarise) Render(_ context.Context, r mcp.Request) ([]mcp.Message, error) {
	slug, _ := r.String("slug")
	return []mcp.Message{mcp.User("Summarise " + slug)}, nil
}

// FuzzHandle drives the decoding: one message in, one answer or none out.
//
// The invariants are the four promises the protocol makes to a client, and each
// of them is a way a real client breaks when it is not kept: an answer that is
// not JSON is a client that cannot parse; an answer with no id is a client that
// cannot match it to the call it is waiting on; an answer to a notification is a
// client that receives one answer too many for the rest of the session; and a
// parse error about a message that parsed is a person reading the wrong half of
// the problem.
func FuzzHandle(f *testing.F) {
	seeds := []string{
		``,
		`{`,
		`null`,
		`[1,2,3]`,
		`"hello"`,
		`{}`,
		`{"jsonrpc":"2.0"}`,
		`{"jsonrpc":"1.0","method":"ping"}`,
		`{"jsonrpc":"2.0","method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":{"a":1},"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":5}`,
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
		`{"jsonrpC":"2.0","method":"ping"}`,
		`{"jsonrpc":"2.0","method":"ping","ID":7}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","METHOD":"tools/call"}`,
		`{"jsonrpc":"2.0","id":1,"METHOD":"tools/call","params":{"NAME":"list_posts"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":7}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":null}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"list_posts"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":["list_posts",{}]}`,
		`{"jsonrpc":"2.0","method":"tools/call","params":["list_posts",{}]}`,
		`{"jsonrpc":"2.0","id":1,"method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":null,"method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","nonsense":{"a":1}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_posts","arguments":{"status":"draft"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"blog://readme"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"summarise","arguments":{"slug":"x"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"nope"}`,
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	s := fuzzServer()
	who := security.Subject{ID: "u1", Tenant: "t1"}

	f.Fuzz(func(t *testing.T, body []byte) {
		answer := s.Handle(context.Background(), who, body)

		members, isObject := objectMembers(body)
		id, hasID := members["id"]
		method, hasMethod := members["method"]
		notification := isObject && sameJSON(members["jsonrpc"], []byte(`"2.0"`)) &&
			!hasID && hasMethod && isNonEmptyString(method)

		if answer == nil {
			if !notification {
				t.Fatalf("a message that is not a notification got no answer: %q", body)
			}
			return
		}
		if notification {
			t.Fatalf("a notification was answered with %s", answer)
		}

		var out answerShape
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Fatalf("the answer is not a JSON-RPC response: %v, %s", err, answer)
		}
		if out.JSONRPC != "2.0" {
			t.Fatalf("the answer does not name the protocol: %s", answer)
		}
		if out.ID == nil {
			t.Fatalf("the answer carries no id, so a client cannot match it to a call: %s", answer)
		}
		if (out.Result == nil) == (out.Error == nil) {
			t.Fatalf("an answer is a result or a failure and never both or neither: %s", answer)
		}

		if out.Error != nil {
			switch out.Error.Code {
			case codeParse, codeInvalidRequest, codeMethodNotFound, codeInternal:
			default:
				t.Fatalf("the answer carries an undeclared code: %s", answer)
			}
			if out.Error.Code == codeParse && json.Valid(body) {
				t.Fatalf("a message that is JSON was reported as not being JSON: %q", body)
			}
		}

		if hasID && !sameJSON(out.ID, id) && !sameJSON(out.ID, []byte("null")) {
			t.Fatalf("the answer carries an id the request did not send: %s for %q", answer, body)
		}

		// The answer is written once and read by somebody else, so what it means
		// has to survive being encoded and decoded again.
		var once any
		if err := json.Unmarshal(answer, &once); err != nil {
			t.Fatalf("the answer does not decode: %v", err)
		}
		again, err := json.Marshal(once)
		if err != nil {
			t.Fatalf("the answer does not encode again: %v", err)
		}
		var twice any
		if err := json.Unmarshal(again, &twice); err != nil {
			t.Fatalf("the re-encoded answer does not decode: %v", err)
		}
		if !reflect.DeepEqual(once, twice) {
			t.Fatalf("the answer does not survive a round trip: %s then %s", answer, again)
		}
	})
}

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

	s := fuzzServer()
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

			var a answerShape
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

// objectMembers reads a message as a JSON object, and reports whether it was
// one. It is how a test tells a member that is absent from one that is null,
// which is the difference between a notification and a request.
func objectMembers(body []byte) (map[string]json.RawMessage, bool) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(body, &members); err != nil || members == nil {
		return nil, false
	}
	return members, true
}

// sameJSON reports whether two encodings carry the same value.
func sameJSON(a, b []byte) bool {
	if a == nil || b == nil {
		return false
	}
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &y); err != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// isNonEmptyString reports whether a raw member is a string with something in
// it, which is what the protocol requires of a method name.
func isNonEmptyString(raw json.RawMessage) bool {
	var s string
	return json.Unmarshal(raw, &s) == nil && s != ""
}
