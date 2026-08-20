// Package helpers builds the servers, tools and readers that this module's own
// tests drive.
//
// Everything here is exported because the tests import it from another package,
// which is the cost of a test tree that only sees the public API. None of it is
// part of what this module publishes: it is scaffolding for the suite under
// tests/, it carries no compatibility promise, and the layout guard fails if a
// package that ships ever reaches it.
package helpers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"

	"github.com/arandu-io/mcp"
)

// The error codes the protocol declares, repeated here because the package
// keeps them unexported and a test that imported them would be asserting that a
// constant equals itself.
const (
	CodeParse          = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInternal       = -32603
)

// AnswerShape is the response the protocol carries, read back loosely: every
// member is raw, so a test can tell "absent" from "null".
type AnswerShape struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Posts is a tool shaped the way a real one is: it asks a service, and the
// service is handed the Subject the request carried.
type Posts struct {
	// Asked records who the tool was told to act as, which is the thing worth
	// asserting about -- everything else in this package is transport.
	Asked security.Subject
	// Refuse makes the service answer the way an authorization failure does.
	Refuse bool
}

// Name and Description are what a client lists the tool as.
func (p *Posts) Name() string        { return "list_posts" }
func (p *Posts) Description() string { return "Lists the posts of this blog." }

// Schema declares one closed string and one number, so a call can be wrong in
// both of the ways a schema catches.
func (p *Posts) Schema() mcp.Schema {
	return mcp.Object(
		mcp.String("status", "Which posts to list").Enum("published", "draft"),
		mcp.Int("limit", "How many to return"),
	)
}

// Handle records the subject it was called as, and then answers or refuses.
func (p *Posts) Handle(_ context.Context, r mcp.Request) (mcp.Response, error) {
	p.Asked = r.Subject()
	if p.Refuse {
		return mcp.Response{}, errors.New("post.list is not allowed for this subject")
	}
	status, _ := r.String("status")
	return mcp.Text("one post, %s", status), nil
}

// Undescribed is the mistake a server is required to refuse at boot: a tool
// with nothing for the model to read before deciding whether to call it.
type Undescribed struct{}

// Name, Description, Schema and Handle make it a tool, and the empty
// description is the whole point of it.
func (Undescribed) Name() string        { return "do_something" }
func (Undescribed) Description() string { return "" }
func (Undescribed) Schema() mcp.Schema  { return mcp.Object() }
func (Undescribed) Handle(context.Context, mcp.Request) (mcp.Response, error) {
	return mcp.Text("ok"), nil
}

// Readme is a resource, so a message can reach the branches that list and read
// one.
type Readme struct{}

// URI, Name, Description, MimeType and Read make it a resource.
func (Readme) URI() string         { return "blog://readme" }
func (Readme) Name() string        { return "readme" }
func (Readme) Description() string { return "What this blog is." }
func (Readme) MimeType() string    { return "" }
func (Readme) Read(context.Context, security.Subject) (mcp.Response, error) {
	return mcp.Text("a blog"), nil
}

// Summarise is a prompt, so a message can reach the branches that list and
// render one.
type Summarise struct{}

// Name and Description are what a client lists the prompt as.
func (Summarise) Name() string        { return "summarise" }
func (Summarise) Description() string { return "Summarises a post." }

// Arguments declares the one input the prompt needs before it is useful.
func (Summarise) Arguments() []mcp.Argument {
	return []mcp.Argument{{Name: "slug", Description: "The post", Required: true}}
}

// Render builds the single turn.
func (Summarise) Render(_ context.Context, r mcp.Request) ([]mcp.Message, error) {
	slug, _ := r.String("slug")
	return []mcp.Message{mcp.User("Summarise " + slug)}, nil
}

// Blog is a server carrying the one tool it is given, so a test can keep hold
// of the tool and assert about what it was called with.
func Blog(tool mcp.Tool) *mcp.Server {
	return &mcp.Server{
		Name: "blog", Version: "1.0.0",
		Instructions: "The posts of a blog.",
		Tools:        []mcp.Tool{tool},
	}
}

// Everything carries one of each kind, so a message can reach every branch.
func Everything() *mcp.Server {
	return &mcp.Server{
		Name: "blog", Version: "1.0.0",
		Instructions: "The posts of a blog.",
		Tools:        []mcp.Tool{&Posts{}},
		Resources:    []mcp.Resource{Readme{}},
		Prompts:      []mcp.Prompt{Summarise{}},
	}
}

// Post sends one message to the web transport, through the router that mounts
// it, and returns what came back.
func Post(body string) *httptest.ResponseRecorder {
	sessions := security.NewSessionStore(bytes.Repeat([]byte("k"), 32), time.Hour, false, nil)
	router := fhttp.NewRouter()
	router.Action(http.MethodPost, "/mcp", mcp.Web(Everything(), sessions, "t1"))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body)))
	return rec
}

// ObjectMembers reads a message as a JSON object, and reports whether it was
// one. It is how a test tells a member that is absent from one that is null,
// which is the difference between a notification and a request.
func ObjectMembers(body []byte) (map[string]json.RawMessage, bool) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(body, &members); err != nil || members == nil {
		return nil, false
	}
	return members, true
}

// SameJSON reports whether two encodings carry the same value.
func SameJSON(a, b []byte) bool {
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

// IsNonEmptyString reports whether a raw member is a string with something in
// it, which is what the protocol requires of a method name.
func IsNonEmptyString(raw json.RawMessage) bool {
	var s string
	return json.Unmarshal(raw, &s) == nil && s != ""
}
