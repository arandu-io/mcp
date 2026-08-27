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
	"testing"

	"github.com/arandu-io/framework/security"

	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

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
	ctx, subject := context.Background(), security.Guest("t1")

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

// A URI no resource answers to is refused, and the answer still names a type
// rather than leaving the member out: the refusal is carried as text, and it is
// text whether or not the URI would have been.
func TestAURINobodyAnswersToIsStillReadAsText(t *testing.T) {
	server := helpers.Files()

	read := server.Handle(context.Background(), security.Guest("t1"), []byte(
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"blog://nothing"}}`))

	if got := mimeOfRead(t, read); got != "text/plain" {
		t.Fatalf("an unknown resource read as %q, want text/plain", got)
	}
}
