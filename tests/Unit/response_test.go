package unit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/http/resources"

	"github.com/arandu-io/mcp"
)

// post is a resource the way an application writes one: the fields that may
// leave, listed, and one of them only when it is set.
type post struct {
	slug, draftNote string
	views           int
}

// ToArray lists the fields. The note is conditional, which is the field a
// reader may not see being absent rather than present and empty.
func (p post) ToArray() map[string]any {
	return map[string]any{
		"slug":  p.slug,
		"views": p.views,
		"note":  resources.When(p.draftNote != "", p.draftNote),
	}
}

// With puts what is about the answer beside the fields.
func (post) With() map[string]any { return map[string]any{"meta": map[string]any{"version": 1}} }

// TestAStructuredAnswerIsTheDocumentAControllerAnswersWith.
//
// A tool and a controller over one service answer the same resource, and a
// client that reads one has to be able to read the other. So the text is the
// document Context.JSON writes for that resource, compared value for value with
// what Context.JSON wrote -- not with a copy of its rules written here.
func TestAStructuredAnswerIsTheDocumentAControllerAnswersWith(t *testing.T) {
	resource := post{slug: "hello", views: 3}

	out := mcp.JSON(resource)
	if out.IsError {
		t.Fatalf("a resource that encodes was answered as a failure: %s", out.Text)
	}

	rec := httptest.NewRecorder()
	if err := hhttp.NewContext(rec, httptest.NewRequest(http.MethodGet, "/", nil), nil, nil).
		JSON(http.StatusOK, resource); err != nil {
		t.Fatalf("Context.JSON refused the same resource: %v", err)
	}

	var fromTool, fromController any
	if err := json.Unmarshal([]byte(out.Text), &fromTool); err != nil {
		t.Fatalf("the answer is not JSON: %v, %s", err, out.Text)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &fromController); err != nil {
		t.Fatalf("Context.JSON wrote something that is not JSON: %v", err)
	}
	if !reflect.DeepEqual(fromTool, fromController) {
		t.Fatalf("one resource, two documents:\ntool       %s\ncontroller %s", out.Text, rec.Body.String())
	}
}

// TestAFieldThatIsMissingIsLeftOut, so a conditional field nobody may see is
// absent from what the model reads, not present and empty.
func TestAFieldThatIsMissingIsLeftOut(t *testing.T) {
	var hidden, shown struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(mcp.JSON(post{slug: "hello"}).Text), &hidden); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(mcp.JSON(post{slug: "hello", draftNote: "unfinished"}).Text), &shown); err != nil {
		t.Fatal(err)
	}

	if _, present := hidden.Data["note"]; present {
		t.Errorf("a missing field reached the answer: %v", hidden.Data)
	}
	if shown.Data["note"] != "unfinished" {
		t.Errorf("a field that is set did not reach the answer: %v", shown.Data)
	}
}

// unencodable is a resource with a field no encoder can write.
type unencodable struct{}

func (unencodable) ToArray() map[string]any { return map[string]any{"stream": make(chan int)} }
func (unencodable) With() map[string]any    { return nil }

// TestAValueThatCannotBeEncodedIsAnAnswerAndNotAPanic.
//
// A tool that hands over something no encoder can write is a mistake in one
// tool. Taking the session down with it makes it a mistake in every one, and
// the client sees a server that stopped rather than a call that failed.
func TestAValueThatCannotBeEncodedIsAnAnswerAndNotAPanic(t *testing.T) {
	for name, resource := range map[string]hhttp.JsonResource{
		"a field no encoder can write": unencodable{},
		"no resource at all":           nil,
	} {
		out := mcp.JSON(resource)

		if !out.IsError {
			t.Errorf("%s was answered as a result: %s", name, out.Text)
		}
		if !strings.Contains(out.Text, "encoding") {
			t.Errorf("%s: the failure does not say what went wrong: %q", name, out.Text)
		}
	}
}

// TestBothRolesAreSpeltTheWayTheProtocolCarriesThem.
//
// A prompt is a conversation, and a conversation has two sides. A role the
// client does not recognise is a turn it drops, which turns a worked example
// into half of one.
func TestBothRolesAreSpeltTheWayTheProtocolCarriesThem(t *testing.T) {
	if got := mcp.User("Summarise this"); got.Role != "user" || got.Text != "Summarise this" {
		t.Errorf("a user turn came out as %+v", got)
	}
	if got := mcp.Assistant("Here is a summary"); got.Role != "assistant" {
		t.Errorf("an assistant turn came out as %+v", got)
	}
}
