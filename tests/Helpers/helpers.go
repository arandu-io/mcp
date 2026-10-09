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
	"io"
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
	CodeInvalidParams  = -32602
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

// Manifest is a resource that declares a type of its own, which is what tells
// the listing and the read apart: Readme leaves MimeType empty and takes the
// default, and this one does not.
type Manifest struct{}

// URI, Name, Description, MimeType and Read make it a resource.
func (Manifest) URI() string         { return "blog://manifest.json" }
func (Manifest) Name() string        { return "manifest" }
func (Manifest) Description() string { return "What this blog contains." }
func (Manifest) MimeType() string    { return "application/json" }
func (Manifest) Read(context.Context, security.Subject) (mcp.Response, error) {
	return mcp.Text(`{"posts":1}`), nil
}

// Files carries the two resources, so one server answers about a declared type
// and an absent one.
func Files() *mcp.Server {
	return &mcp.Server{
		Name: "blog", Version: "1.0.0",
		Resources: []mcp.Resource{Readme{}, Manifest{}},
	}
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

// Unrenderable is a prompt whose Render refuses, which is what a policy
// answers with when the subject may not have it. It is the case a successful
// answer carrying no messages cannot be told from.
type Unrenderable struct{}

// Name and Description are what a client lists the prompt as.
func (Unrenderable) Name() string        { return "unrenderable" }
func (Unrenderable) Description() string { return "A prompt nobody may have." }

// Arguments declares none: what is being reached is the failure, not a check.
func (Unrenderable) Arguments() []mcp.Argument { return nil }

// Render refuses.
func (Unrenderable) Render(context.Context, mcp.Request) ([]mcp.Message, error) {
	return nil, errors.New("prompt.get is not allowed for this subject")
}

// Silent is a prompt that renders no messages and means it. It is the one
// answer an empty list is reserved for, and it is why a failure has to be
// carried some other way.
type Silent struct{}

// Name and Description are what a client lists the prompt as.
func (Silent) Name() string        { return "silent" }
func (Silent) Description() string { return "A prompt with nothing to say yet." }

// Arguments declares none.
func (Silent) Arguments() []mcp.Argument { return nil }

// Render answers with no messages and no error.
func (Silent) Render(context.Context, mcp.Request) ([]mcp.Message, error) {
	return []mcp.Message{}, nil
}

// Conversations carries the three prompts, so one server answers a prompt that
// renders, one that refuses and one that is deliberately empty.
func Conversations() *mcp.Server {
	return &mcp.Server{
		Name: "blog", Version: "1.0.0",
		Prompts: []mcp.Prompt{Summarise{}, Unrenderable{}, Silent{}},
	}
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
//
// A number is compared as it was written rather than as a float64, through
// Decode, so two identical ids larger than a float64 holds are the same id.
func SameJSON(a, b []byte) bool {
	if a == nil || b == nil {
		return false
	}
	x, err := Decode(a)
	if err != nil {
		return false
	}
	y, err := Decode(b)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// Decode reads one JSON value, keeping every number as the json.Number it was
// written as.
//
// Decoding into an any turns a number into a float64, and a number larger than
// a float64 holds -- which the protocol allows as an id, and which the server
// echoes byte for byte -- then fails to decode at all. A check written that way
// reports the one member the server got right. Anything after the value is an
// error, as it is for json.Unmarshal.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("helpers: data after the JSON value")
	}
	return v, nil
}

// IsNonEmptyString reports whether a raw member is a string with something in
// it, which is what the protocol requires of a method name.
func IsNonEmptyString(raw json.RawMessage) bool {
	var s string
	return json.Unmarshal(raw, &s) == nil && s != ""
}
