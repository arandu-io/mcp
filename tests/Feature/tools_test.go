package feature

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/arandu-io/framework/security"

	"github.com/arandu-io/mcp"
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

// limited is a tool that reads a number and records what it read, so a test can
// see the value that reached application code rather than the one that was
// sent.
type limited struct {
	got int
	ok  bool
	ran bool
}

// Name, Description and Schema make it a tool.
func (*limited) Name() string        { return "list_posts" }
func (*limited) Description() string { return "Lists the posts of this blog." }
func (*limited) Schema() mcp.Schema {
	return mcp.Object(mcp.Int("limit", "How many to return"))
}

// Handle records the number the request carried.
func (t *limited) Handle(_ context.Context, r mcp.Request) (mcp.Response, error) {
	t.ran = true
	t.got, t.ok = r.Int("limit")
	return mcp.Text("%d", t.got), nil
}

// TestANumberThatIsNotAnIntegerNeverReachesTheTool.
//
// The measured behaviour was that 1.9 arrived at the handler as 1 and 1e100 as
// the largest int the machine has. Neither is the number the client sent, and
// neither is refused anywhere: the tool runs, answers, and the answer is about
// a different call. The refusal has to come before Handle, because after it
// there is nothing left that knows what was asked for.
func TestANumberThatIsNotAnIntegerNeverReachesTheTool(t *testing.T) {
	for _, bad := range []struct {
		about string
		value any
	}{
		{"a fraction", 1.9},
		{"a value past the range of an int", 1e100},
	} {
		tool := &limited{}
		out := helpers.Blog(tool).Call(context.Background(), security.Subject{ID: "u1"},
			"list_posts", map[string]any{"limit": bad.value})

		if !out.IsError {
			t.Errorf("%s was accepted where an integer was declared: %q", bad.about, out.Text)
		}
		if tool.ran {
			t.Errorf("%s reached the tool, which then read %d", bad.about, tool.got)
		}
	}

	// A whole number still reaches it, and reaches it as itself.
	tool := &limited{}
	if out := helpers.Blog(tool).Call(context.Background(), security.Subject{ID: "u1"},
		"list_posts", map[string]any{"limit": 20.0}); out.IsError {
		t.Fatalf("a whole number was refused: %q", out.Text)
	}
	if !tool.ok || tool.got != 20 {
		t.Errorf("the tool read %d (present: %v), want 20", tool.got, tool.ok)
	}
}

// TestReadingANumberThatIsNotAnIntegerReportsThatItIsNot.
//
// Int is reachable without a schema -- a prompt reads its arguments through the
// same Request -- so the refusal cannot live only in the schema. Answering
// false is the only answer that does not invent a value: a tool told "there is
// no number here" asks again, and a tool handed 1 for 1.9 does not know to.
func TestReadingANumberThatIsNotAnIntegerReportsThatItIsNot(t *testing.T) {
	for _, bad := range []struct {
		about string
		value any
	}{
		{"a fraction", 1.9},
		{"a value past the range of an int", 1e100},
		{"an infinity", math.Inf(1)},
		{"not a number at all", math.NaN()},
	} {
		tool := &limited{}
		helpers.Blog(tool).Call(context.Background(), security.Subject{ID: "u1"}, "list_posts", nil)

		r := mcp.Request{Arguments: map[string]any{"limit": bad.value}}
		if got, ok := r.Int("limit"); ok {
			t.Errorf("%s was read as the integer %d", bad.about, got)
		}
	}

	r := mcp.Request{Arguments: map[string]any{"limit": 20.0}}
	if got, ok := r.Int("limit"); !ok || got != 20 {
		t.Errorf("a whole number was read as %d (present: %v), want 20", got, ok)
	}
	if _, ok := (mcp.Request{}).Int("limit"); ok {
		t.Error("an argument that was never sent was read as a number")
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
