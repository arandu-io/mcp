package feature

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/framework/security"

	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// TestTheToolIsCalledAsTheSubjectThatAsked is the one that matters.
//
// An MCP server hands a language model the keys to an application. If the
// subject does not reach the tool, every policy in the project is bypassed by
// something that talks to it over a pipe -- and it is bypassed quietly, because
// the answers look right.
func TestTheToolIsCalledAsTheSubjectThatAsked(t *testing.T) {
	tool := &helpers.Posts{}
	who := security.Subject{ID: "u1", Tenant: "t1", Roles: []string{"author"}}

	out := helpers.Blog(tool).Call(context.Background(), who, "list_posts",
		map[string]any{"status": "published"})
	if out.IsError {
		t.Fatalf("the call failed: %s", out.Text)
	}
	if tool.Asked.ID != "u1" || tool.Asked.Tenant != "t1" {
		t.Fatalf("the tool was called as %+v, want u1 in t1", tool.Asked)
	}
}

// TestARefusalIsAnErrorAndNotAnEmptyResult.
//
// A model handed an empty list concludes there is nothing there and says so to
// somebody. A model told it may not, stops. The difference is one boolean and it
// is the difference between "you have no invoices" and "you cannot see them".
func TestARefusalIsAnErrorAndNotAnEmptyResult(t *testing.T) {
	tool := &helpers.Posts{Refuse: true}

	out := helpers.Blog(tool).Call(context.Background(), security.Subject{ID: "u1", Tenant: "t1"},
		"list_posts", nil)

	if !out.IsError {
		t.Fatal("a refused authorization was answered as a result")
	}
	if !strings.Contains(out.Text, "not allowed") {
		t.Errorf("the refusal does not say what happened: %q", out.Text)
	}
}

// TestAnUndeclaredArgumentIsRefused.
//
// A model that invents a parameter and is not told keeps inventing it. Worse, a
// tool that reads only what it declared acts on a call it half understood.
func TestAnUndeclaredArgumentIsRefused(t *testing.T) {
	tool := &helpers.Posts{}

	out := helpers.Blog(tool).Call(context.Background(), security.Subject{ID: "u1"}, "list_posts",
		map[string]any{"tenant": "somebody-elses"})

	if !out.IsError {
		t.Fatal("an argument nobody declared was accepted")
	}
	if !strings.Contains(out.Text, "tenant") {
		t.Errorf("the refusal does not name the argument: %q", out.Text)
	}
	// Subject carries a slice and is not comparable, so the id is what says
	// whether Handle ran at all.
	if tool.Asked.ID != "" {
		t.Error("the tool ran anyway")
	}
}

// TestAWrongTypeAndAWrongEnumAreBothReported, in one answer.
//
// A model told one mistake per call spends three calls on a form it could have
// fixed on the second.
func TestAWrongTypeAndAWrongEnumAreBothReported(t *testing.T) {
	out := helpers.Blog(&helpers.Posts{}).Call(context.Background(), security.Subject{ID: "u1"},
		"list_posts", map[string]any{"status": "archived", "limit": "ten"})

	if !out.IsError {
		t.Fatal("a call with two bad arguments was accepted")
	}
	for _, want := range []string{"status", "limit"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("%s is not mentioned: %q", want, out.Text)
		}
	}
}

// TestAnUnknownToolListsTheOnesThatExist: a model retrying the same wrong name
// is a model that was told nothing useful.
func TestAnUnknownToolListsTheOnesThatExist(t *testing.T) {
	out := helpers.Blog(&helpers.Posts{}).Call(context.Background(), security.Subject{ID: "u1"},
		"list_post", nil)

	if !out.IsError {
		t.Fatal("an unknown tool was accepted")
	}
	if !strings.Contains(out.Text, "list_posts") {
		t.Errorf("the answer does not list what exists: %q", out.Text)
	}
}
