// What a server says a resource is, asked twice.
//
// A client reads the type from the listing and then reads the resource, and the
// two answers describe the same thing. They came from different places -- one
// from the resource, one from a literal -- and a server that contradicts itself
// about a resource is one a client cannot cache, label or hand to a parser.

package feature_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"

	"github.com/arandu-io/mcp"
	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// The two codes a read that did not happen comes back as. They are written here
// rather than imported because the package keeps them unexported, and a test
// that imported them would be asserting that a constant equals itself.
const (
	codeResourceNotFound = -32002
	codeInternal         = -32603
)

// refused is a resource the subject may not read, which is what a policy
// answers with when it refuses. It is the case the protocol has to tell from a
// resource whose text happens to say "not allowed".
type refused struct{}

// URI, Name, Description, MimeType and Read make it a resource.
func (refused) URI() string         { return "blog://drafts" }
func (refused) Name() string        { return "drafts" }
func (refused) Description() string { return "The unpublished posts." }
func (refused) MimeType() string    { return "" }
func (refused) Read(context.Context, auth.Subject) (mcp.Response, error) {
	return mcp.Response{}, errors.New("blog.read is not allowed for this subject")
}

// guarded carries the readable resources and the refused one, so one server
// answers both ways.
func guarded() *mcp.Server {
	return &mcp.Server{
		Name: "blog", Version: "1.0.0",
		Resources: []mcp.Resource{helpers.Readme{}, helpers.Manifest{}, refused{}},
	}
}

// mimeOfListed returns the type the listing gives for a URI.
func mimeOfListed(t *testing.T, body []byte, uri string) string {
	t.Helper()

	var answer helpers.AnswerShape
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("the listing is not a response: %v", err)
	}
	var result struct {
		Resources []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(answer.Result, &result); err != nil {
		t.Fatalf("the listing is not the shape resources/list carries: %v", err)
	}
	for _, r := range result.Resources {
		if r.URI == uri {
			return r.MimeType
		}
	}
	t.Fatalf("%s was not listed", uri)
	return ""
}

// mimeOfRead returns the type the read gives for a URI.
func mimeOfRead(t *testing.T, body []byte) string {
	t.Helper()

	var answer helpers.AnswerShape
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("the read is not a response: %v", err)
	}
	var result struct {
		Contents []struct {
			MimeType string `json:"mimeType"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(answer.Result, &result); err != nil {
		t.Fatalf("the read is not the shape resources/read carries: %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("the read carried %d contents, want 1", len(result.Contents))
	}
	return result.Contents[0].MimeType
}

func TestAResourceIsTheSameTypeListedAndRead(t *testing.T) {
	server := helpers.Files()
	ctx, subject := context.Background(), auth.Guest("t1")

	listed := server.Handle(ctx, subject, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`))

	for _, want := range []struct {
		uri  string
		mime string
	}{
		{"blog://manifest.json", "application/json"},
		{"blog://readme", "text/plain"},
	} {
		if got := mimeOfListed(t, listed, want.uri); got != want.mime {
			t.Fatalf("resources/list says %s is %q, want %q", want.uri, got, want.mime)
		}

		read := server.Handle(ctx, subject, []byte(
			`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"`+want.uri+`"}}`))
		if got := mimeOfRead(t, read); got != want.mime {
			t.Fatalf("resources/read says %s is %q, want %q", want.uri, got, want.mime)
		}
	}
}

// failureOf reads the answer as a response and returns the failure it carries,
// failing the test when it carried a result instead.
func failureOf(t *testing.T, body []byte, about string) (int, string) {
	t.Helper()

	var answer helpers.AnswerShape
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("%s was answered with something that is not a response: %v", about, err)
	}
	if answer.Error == nil {
		t.Fatalf("%s was answered with a result: %s", about, body)
	}
	return answer.Error.Code, answer.Error.Message
}

// TestAReadThatDidNotHappenIsNotAnsweredAsTheResourceItself.
//
// A refused read and a failed one are failures of the call. Carrying the words
// of the failure back in the contents member describes them as the resource:
// the client caches them under the URI, labels them with the type the listing
// promised, and the model reads "not allowed" as the document it asked for --
// which is the same mistake as answering a refusal with an empty list, one
// method further along.
func TestAReadThatDidNotHappenIsNotAnsweredAsTheResourceItself(t *testing.T) {
	ctx, subject := context.Background(), auth.Guest("t1")

	refusal := guarded().Handle(ctx, subject, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"blog://drafts"}}`))

	code, message := failureOf(t, refusal, "a resource that refused the read")
	if code != codeInternal {
		t.Errorf("a resource that refused the read was answered with %d, want %d", code, codeInternal)
	}
	if !strings.Contains(message, "not allowed") {
		t.Errorf("the failure does not say what happened: %q", message)
	}
	if strings.Contains(string(refusal), "contents") {
		t.Errorf("the words of the failure came back as the resource: %s", refusal)
	}
}

// TestAURINobodyAnswersToIsAFailureAndNamesItsOwnCode.
//
// The protocol carries a code for a resource that is not there, and it is not
// the code for a read that broke: a client that asked for the wrong URI can
// correct that, and one whose read failed can retry. Answering both the same
// way, or answering either with a successful document, leaves it with neither
// choice.
func TestAURINobodyAnswersToIsAFailureAndNamesItsOwnCode(t *testing.T) {
	read := guarded().Handle(context.Background(), auth.Guest("t1"), []byte(
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"blog://nothing"}}`))

	code, message := failureOf(t, read, "a URI no resource answers to")
	if code != codeResourceNotFound {
		t.Errorf("a URI no resource answers to was answered with %d, want %d", code, codeResourceNotFound)
	}
	if !strings.Contains(message, "blog://nothing") {
		t.Errorf("the failure does not name the URI that was asked for: %q", message)
	}
}

// TestAResourceThatReadsIsStillAnsweredWithItsContents, so the failures above
// are about failing and not a way to refuse every read.
func TestAResourceThatReadsIsStillAnsweredWithItsContents(t *testing.T) {
	read := guarded().Handle(context.Background(), auth.Guest("t1"), []byte(
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"blog://readme"}}`))

	var answer helpers.AnswerShape
	if err := json.Unmarshal(read, &answer); err != nil {
		t.Fatalf("the read is not a response: %v", err)
	}
	if answer.Error != nil {
		t.Fatalf("a resource that reads was answered with a failure: %s", read)
	}
	if !strings.Contains(string(answer.Result), "a blog") {
		t.Errorf("the contents did not reach the client: %s", read)
	}
}

// TestAReadThatFailedIsStillSilentWhenItWasANotification.
//
// A notification carries no id, so the sender is not listening -- and a failure
// is still an answer. Turning the new failures into the one message a
// notification gets back is the way this change would break a client that was
// working.
func TestAReadThatFailedIsStillSilentWhenItWasANotification(t *testing.T) {
	for _, uri := range []string{"blog://drafts", "blog://nothing", "blog://readme"} {
		if got := guarded().Handle(context.Background(), auth.Guest("t1"), []byte(
			`{"jsonrpc":"2.0","method":"resources/read","params":{"uri":"`+uri+`"}}`)); got != nil {
			t.Errorf("a notification reading %s was answered with %s", uri, got)
		}
	}
}
