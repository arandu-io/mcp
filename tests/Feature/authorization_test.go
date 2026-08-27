// What happens to the statement when the policy says no.
//
// Every other test in this tree stops at the answer the client receives. These
// go one step further and read what reached the database handle, because a
// refusal that produces the right message and the wrong number of statements is
// the failure nobody notices: the model is told it may not, and the row was
// read anyway.

package feature_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/arandu-io/framework/observability"
	"github.com/arandu-io/framework/security"

	"github.com/arandu-io/mcp"
	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// editor is a subject the policy allows.
func editor() security.Subject {
	return security.Subject{ID: "u1", Tenant: "t1", Roles: []string{helpers.EditorRole}}
}

// collected returns a context carrying a collector, and the collector.
func collected(t *testing.T) (context.Context, *observability.Collector) {
	t.Helper()
	collector := observability.NewCollector("test")
	return observability.WithCollector(context.Background(), collector), collector
}

func TestARefusedToolReachesTheHandleWithNoStatementAtAll(t *testing.T) {
	db, statements := helpers.CountingHandle(t)
	server := helpers.SectionsServer(db)
	ctx, collector := collected(t)

	answer := server.Call(ctx, security.Guest("t1"), "list_sections", nil)

	if !answer.IsError {
		t.Fatalf("a refused tool answered %q without isError", answer.Text)
	}
	if statements.Count() != 0 {
		last, _ := statements.Last()
		t.Fatalf("the refusal ran %d statements, and the last was %q", statements.Count(), last)
	}
	if collector.QueryCount() != 0 {
		t.Fatalf("the collector recorded %d queries for a refused tool", collector.QueryCount())
	}
}

// The half that keeps the one above honest. A boundary nothing can cross is
// indistinguishable from a boundary asked the wrong question: if the allowed
// subject also ran no statement, the count above would be proving that the
// handle is unreachable rather than that the policy refused.
func TestTheAllowedSubjectReachesTheHandleAndCarriesTheTenantFromTheGrant(t *testing.T) {
	db, statements := helpers.CountingHandle(t)
	server := helpers.SectionsServer(db)
	ctx, collector := collected(t)

	answer := server.Call(ctx, editor(), "list_sections", nil)

	if answer.IsError {
		t.Fatalf("the allowed subject was refused: %s", answer.Text)
	}
	if statements.Count() != 1 {
		t.Fatalf("the allowed subject ran %d statements, want 1", statements.Count())
	}
	if collector.QueryCount() != 1 {
		t.Fatalf("the collector recorded %d queries, want 1", collector.QueryCount())
	}

	statement, args := statements.Last()
	if !strings.Contains(statement, "tenant_id") {
		t.Fatalf("the statement does not name the tenant column: %q", statement)
	}
	if len(args) != 1 || args[0].Value != "t1" {
		t.Fatalf("the statement was bound with %v, want the tenant from the Grant", args)
	}

	var names []string
	if err := json.Unmarshal([]byte(answer.Text), &names); err != nil {
		t.Fatalf("the answer is not the list the tool encoded: %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("the tool answered %d sections, want 1", len(names))
	}
}

// A subject nobody filled in is not a guest -- it is a session that was not
// loaded -- and it is refused before the policy is asked. The statement count
// is what says the refusal happened above the handle rather than inside it.
func TestASubjectNobodyLoadedRunsNoStatementEither(t *testing.T) {
	db, statements := helpers.CountingHandle(t)
	server := helpers.SectionsServer(db)
	ctx, collector := collected(t)

	answer := server.Call(ctx, security.Subject{}, "list_sections", nil)

	if !answer.IsError {
		t.Fatalf("an empty subject answered %q without isError", answer.Text)
	}
	if statements.Count() != 0 || collector.QueryCount() != 0 {
		t.Fatalf("an empty subject ran %d statements and recorded %d queries",
			statements.Count(), collector.QueryCount())
	}
}

// The same measurement one layer out, over the wire format both transports
// speak, so that nothing between the message and the handle can add a path of
// its own. The refusal arrives as content with isError set, never as an empty
// list -- a model handed an empty list concludes there is nothing there.
func TestAToolRefusedOverTheWireProtocolRunsNoStatement(t *testing.T) {
	db, statements := helpers.CountingHandle(t)
	server := helpers.SectionsServer(db)
	ctx, collector := collected(t)

	body := server.Handle(ctx, security.Guest("t1"), []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_sections"}}`))

	var answer helpers.AnswerShape
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("the answer is not a response: %v", err)
	}
	if answer.Error != nil {
		t.Fatalf("a refusal became a transport error: %v", answer.Error)
	}

	var result struct {
		Content []struct{ Text string } `json:"content"`
		IsError bool                    `json:"isError"`
	}
	if err := json.Unmarshal(answer.Result, &result); err != nil {
		t.Fatalf("the result is not the shape tools/call carries: %v", err)
	}
	if !result.IsError {
		t.Fatalf("the refusal was not marked as one: %s", answer.Result)
	}
	if statements.Count() != 0 || collector.QueryCount() != 0 {
		t.Fatalf("a refusal over the protocol ran %d statements and recorded %d queries",
			statements.Count(), collector.QueryCount())
	}
}

// The local transport, whose subject is declared where the server is
// registered rather than read from a session. A message that names a tool the
// declared subject may not call reaches the handle with nothing.
func TestTheLocalTransportRefusesWithoutReachingTheHandle(t *testing.T) {
	db, statements := helpers.CountingHandle(t)
	server := helpers.SectionsServer(db)
	ctx, collector := collected(t)

	out := new(strings.Builder)
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_sections"}}` + "\n")

	if err := mcp.Local(ctx, server, security.Guest("t1"), in, out); err != nil {
		t.Fatalf("serving over the pipe: %v", err)
	}
	if !strings.Contains(out.String(), `"isError":true`) {
		t.Fatalf("the pipe answered %q, and it is not a refusal", out.String())
	}
	if statements.Count() != 0 || collector.QueryCount() != 0 {
		t.Fatalf("a refusal over the pipe ran %d statements and recorded %d queries",
			statements.Count(), collector.QueryCount())
	}
}
