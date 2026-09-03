// What a standard MCP client does with the HTTP transport, replayed.
//
// The exchange below is not invented: it is the byte sequence the official Go
// client performs against this server, recorded from a live run and pinned
// here. The recording is what a conformance claim is worth, and replaying it is
// how the claim survives without this module depending on a client to make it.
//
// The one thing added to the recording is the Authorization header. It was made
// against a route with no guard, when the transport loaded the session itself;
// the transport now serves only a subject a guard put on the request, and a
// client that is not a browser is admitted by a bearer token.
//
// A dependency would be the other way to write this, and it is the wrong one
// twice over: it puts a whole client library, its transports and its own
// protocol revision inside the build of a package whose entire surface is four
// files, and it makes the answer to "does a client accept this" depend on a
// version this module would then have to track.

package feature

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fhttp "github.com/arandu-io/framework/http"

	"github.com/arandu-io/mcp"
	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// mounted returns the router a client reaches, over the given server: behind
// the bearer-token guard, which is how a client that is not a browser is
// admitted.
func mounted(server *mcp.Server) *fhttp.Router {
	return helpers.MountBehindToken(server, helpers.Bearer())
}

// send posts one message the way a standard client posts it, token included,
// and returns what came back.
func send(router *fhttp.Router, body string) *httptest.ResponseRecorder {
	return helpers.Send(router, body, helpers.WithToken()...)
}

// TestTheExchangeAStandardClientPerformsIsAnswered.
//
// Every step is one a real client took, in the order it took them, and each of
// them is a way the connection ends if it is answered wrongly. The first is the
// one worth reading twice: the client opens by calling a method from a later
// revision, and what keeps the session alive is that the refusal is a refusal
// of that method rather than of the client.
func TestTheExchangeAStandardClientPerformsIsAnswered(t *testing.T) {
	router := mounted(helpers.Everything())

	// 1. A method from a revision after this one. The client tries it first and
	//    falls back on being told it is not implemented, so the code has to be
	//    the one that means exactly that.
	discover := send(router, `{"jsonrpc":"2.0","id":1,"method":"server/discover",`+
		`"params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`)
	code, _ := failureOver(t, discover, "a method from a later revision")
	if code != helpers.CodeMethodNotFound {
		t.Errorf("a method from a later revision was answered with %d, want %d",
			code, helpers.CodeMethodNotFound)
	}

	// 2. The handshake, with the parameters a standard client sends: a revision
	//    later than this server's, its own capabilities, and who it is.
	handshake := send(router, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{`+
		`"clientInfo":{"name":"probe","version":"1.0.0"},"protocolVersion":"2025-11-25",`+
		`"capabilities":{"roots":{"listChanged":true}}}}`)
	var negotiated struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	resultOver(t, handshake, "the handshake", &negotiated)
	if negotiated.Result.ProtocolVersion != mcp.Version {
		t.Errorf("the handshake settled on %q, want %q", negotiated.Result.ProtocolVersion, mcp.Version)
	}

	// 3. The client opens the stream the server would push notifications down.
	//    This transport has none, and the answer that says so is the method not
	//    being allowed -- which the client reads as "no stream here" and carries
	//    on. Anything else is a client that waits for a stream that never opens.
	stream := httptest.NewRecorder()
	get := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	get.Header.Set("Accept", "text/event-stream")
	router.ServeHTTP(stream, get)
	if stream.Code != http.StatusMethodNotAllowed {
		t.Errorf("the stream a client offers to open was answered %d, want %d",
			stream.Code, http.StatusMethodNotAllowed)
	}

	// 4. The notification that ends the handshake. It is waiting for nothing,
	//    and a body where it expects none is one answer too many for the rest
	//    of the session.
	if ended := send(router, `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`); ended.Code != http.StatusAccepted {
		t.Errorf("the notification that ends the handshake was answered %d with %q, want %d and nothing",
			ended.Code, ended.Body.String(), http.StatusAccepted)
	}

	// 5. The three calls a client makes once it is connected.
	var listed struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	resultOver(t, send(router, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`), "tools/list", &listed)
	if len(listed.Result.Tools) != 1 || listed.Result.Tools[0].Name != "list_posts" {
		t.Fatalf("tools/list answered %+v", listed.Result.Tools)
	}
	if listed.Result.Tools[0].InputSchema["type"] != "object" {
		t.Errorf("the schema a client reads is not an object schema: %v", listed.Result.Tools[0].InputSchema)
	}

	var called struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	resultOver(t, send(router, `{"jsonrpc":"2.0","id":4,"method":"tools/call",`+
		`"params":{"name":"list_posts","arguments":{}}}`), "tools/call", &called)
	if len(called.Result.Content) != 1 || called.Result.Content[0].Type != "text" {
		t.Fatalf("tools/call answered content a client cannot read: %+v", called.Result.Content)
	}
	if called.Result.IsError {
		t.Errorf("a call that worked was marked as a failure: %+v", called.Result)
	}

	var read struct {
		Result struct {
			Contents []struct {
				URI      string `json:"uri"`
				MimeType string `json:"mimeType"`
				Text     string `json:"text"`
			} `json:"contents"`
		} `json:"result"`
	}
	resultOver(t, send(router, `{"jsonrpc":"2.0","id":5,"method":"resources/read",`+
		`"params":{"uri":"blog://readme"}}`), "resources/read", &read)
	if len(read.Result.Contents) != 1 || read.Result.Contents[0].URI != "blog://readme" {
		t.Fatalf("resources/read answered %+v", read.Result.Contents)
	}
}

// TestAStandardClientIsToldWhichRefusalsAreRefusals.
//
// The two codes this server answers a call it could not perform with are the
// ones the client turns into an error the caller sees. A client that received
// a successful result instead would report a resource that is empty and a
// prompt that has nothing to say, and the caller would believe it.
func TestAStandardClientIsToldWhichRefusalsAreRefusals(t *testing.T) {
	router := mounted(helpers.Conversations())

	for _, refusal := range []struct {
		about string
		body  string
		code  int
	}{
		{
			"a resource that is not there",
			`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"blog://nothing"}}`,
			-32002,
		},
		{
			"a prompt that is not there",
			`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"nothing"}}`,
			helpers.CodeInvalidParams,
		},
		{
			"a prompt that refused to render",
			`{"jsonrpc":"2.0","id":3,"method":"prompts/get","params":{"name":"unrenderable"}}`,
			helpers.CodeInternal,
		},
	} {
		code, message := failureOver(t, send(router, refusal.body), refusal.about)
		if code != refusal.code {
			t.Errorf("%s was answered with %d, want %d", refusal.about, code, refusal.code)
		}
		if message == "" {
			t.Errorf("%s was refused without saying anything a caller can read", refusal.about)
		}
	}
}

// TestABodyOverTheLimitReachesTheClientAsTheLimit.
//
// The bound is on the request and the answer is the status that names it, so a
// client reports a message that was too large rather than a server that
// stopped answering. There is no event stream to bound: this transport answers
// one message per request and pushes nothing.
func TestABodyOverTheLimitReachesTheClientAsTheLimit(t *testing.T) {
	router := mounted(helpers.Everything())

	oversized := send(router, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_posts",`+
		`"arguments":{"status":"`+strings.Repeat("a", 2<<20)+`"}}}`)
	if oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a body over the limit was answered %d, want %d",
			oversized.Code, http.StatusRequestEntityTooLarge)
	}

	// And the message after it is answered, so the limit refuses a message and
	// not a client.
	var after struct {
		Result map[string]any `json:"result"`
	}
	resultOver(t, send(router, `{"jsonrpc":"2.0","id":2,"method":"ping"}`), "the call after an oversized one", &after)
}

// cancelling is a tool that reports what its context was doing when it ran.
type cancelling struct{ err error }

// Name, Description and Schema make it a tool.
func (*cancelling) Name() string        { return "list_posts" }
func (*cancelling) Description() string { return "Lists the posts of this blog." }
func (*cancelling) Schema() mcp.Schema  { return mcp.Object() }

// Handle records whether the request that reached it was still wanted.
func (t *cancelling) Handle(ctx context.Context, _ mcp.Request) (mcp.Response, error) {
	t.err = ctx.Err()
	return mcp.Text("one post"), nil
}

// TestAnHTTPCallCarriesTheCancellationOfItsRequest.
//
// The handler is the last place that can stop, and it can only stop if the
// cancellation reached it. A tool that keeps working for a client that hung up
// is a database query nobody is waiting for, and under load it is all of them.
func TestAnHTTPCallCarriesTheCancellationOfItsRequest(t *testing.T) {
	tool := &cancelling{}
	router := mounted(helpers.Blog(tool))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	request := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_posts"}}`))
	request.Header.Set("Authorization", "Bearer "+helpers.Token)
	router.ServeHTTP(httptest.NewRecorder(), request.WithContext(ctx))

	if !errors.Is(tool.err, context.Canceled) {
		t.Fatalf("the tool ran with a context reporting %v, and the request was cancelled before it", tool.err)
	}
}

// failureOver reads the failure an HTTP answer carried, failing the test when
// it carried a result instead.
func failureOver(t *testing.T, rec *httptest.ResponseRecorder, about string) (int, string) {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("%s was answered %d, and a protocol failure is carried in a 200", about, rec.Code)
	}
	var answer helpers.AnswerShape
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatalf("%s was answered with something that is not a response: %v", about, err)
	}
	if answer.Error == nil {
		t.Fatalf("%s was answered with a result: %s", about, rec.Body.String())
	}
	return answer.Error.Code, answer.Error.Message
}

// resultOver decodes the result an HTTP answer carried into out, failing the
// test when it carried a failure instead.
func resultOver(t *testing.T, rec *httptest.ResponseRecorder, about string, out any) {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("%s was answered %d, want %d", about, rec.Code, http.StatusOK)
	}
	var answer helpers.AnswerShape
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatalf("%s was answered with something that is not a response: %v", about, err)
	}
	if answer.Error != nil {
		t.Fatalf("%s was refused: %s", about, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("%s answered a result of a shape a client does not read: %v", about, err)
	}
}
