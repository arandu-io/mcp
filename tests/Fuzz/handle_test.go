package fuzz

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/arandu-io/framework/security"

	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// What arrives at this package is written by a program it does not control, so
// the targets here assert about every byte sequence rather than about the ones a
// client is supposed to send. A server that stops answering because a message
// was malformed has stopped answering the client that sent it well-formed ones.

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

	s := helpers.Everything()
	who := security.Subject{ID: "u1", Tenant: "t1"}

	f.Fuzz(func(t *testing.T, body []byte) {
		answer := s.Handle(context.Background(), who, body)

		members, isObject := helpers.ObjectMembers(body)
		id, hasID := members["id"]
		method, hasMethod := members["method"]
		notification := isObject && helpers.SameJSON(members["jsonrpc"], []byte(`"2.0"`)) &&
			!hasID && hasMethod && helpers.IsNonEmptyString(method)

		if answer == nil {
			if !notification {
				t.Fatalf("a message that is not a notification got no answer: %q", body)
			}
			return
		}
		if notification {
			t.Fatalf("a notification was answered with %s", answer)
		}

		var out helpers.AnswerShape
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
			case helpers.CodeParse, helpers.CodeInvalidRequest, helpers.CodeMethodNotFound, helpers.CodeInternal:
			default:
				t.Fatalf("the answer carries an undeclared code: %s", answer)
			}
			if out.Error.Code == helpers.CodeParse && json.Valid(body) {
				t.Fatalf("a message that is JSON was reported as not being JSON: %q", body)
			}
		}

		if hasID && !helpers.SameJSON(out.ID, id) && !helpers.SameJSON(out.ID, []byte("null")) {
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
