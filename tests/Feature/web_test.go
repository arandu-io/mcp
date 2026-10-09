// Who the web transport serves, and who it refuses.
//
// The transport reads the subject a guard put on the request and nothing else.
// Each test here mounts it the way an application does -- behind the bearer
// token guard, behind the session guard, behind a guard that lets a request
// through without a subject, and behind nothing -- and asks the tool who it was
// told it was acting as.

package feature_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/hesape/auth"

	"github.com/arandu-io/mcp"
	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// aCall asks for the tool every server here carries.
const aCall = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_posts","arguments":{}}}`

// TestTheWebTransportServesTheSubjectTheTokenNamed.
//
// The token is the client's credential and the resolver is the application's
// answer to whom it was issued. What reaches the tool is that answer -- the
// account and the tenant the token was recorded with -- and nothing the
// message says.
func TestTheWebTransportServesTheSubjectTheTokenNamed(t *testing.T) {
	tool := &helpers.Posts{}
	who := auth.Subject{ID: "u7", Tenant: "t9"}
	router := helpers.MountBehindToken(helpers.Blog(tool), who)

	rec := helpers.Send(router, aCall, helpers.WithToken()...)

	if rec.Code != http.StatusOK {
		t.Fatalf("a call carrying a token the resolver knows was answered %d: %s", rec.Code, rec.Body.String())
	}
	if tool.Asked.ID != who.ID || tool.Asked.Tenant != who.Tenant {
		t.Fatalf("the tool ran as %+v, and the token was issued to %+v", tool.Asked, who)
	}
}

// TestTheWebTransportServesTheSubjectTheSessionCarried, so a route behind the
// session guard serves the account signed in and not one the transport chose.
func TestTheWebTransportServesTheSubjectTheSessionCarried(t *testing.T) {
	tool := &helpers.Posts{}
	router, sessions := helpers.MountBehindSession(helpers.Blog(tool))
	who := auth.Subject{ID: "u3", Tenant: "t2"}

	signedIn := httptest.NewRecorder()
	if _, err := sessions.Start(t.Context(), signedIn, who); err != nil {
		t.Fatalf("starting a session: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(aCall))
	request.Header.Set("Accept", "application/json")
	for _, cookie := range signedIn.Result().Cookies() {
		request.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)

	if rec.Code != http.StatusOK {
		t.Fatalf("a call carrying a session was answered %d: %s", rec.Code, rec.Body.String())
	}
	if tool.Asked.ID != who.ID || tool.Asked.Tenant != who.Tenant {
		t.Fatalf("the tool ran as %+v, and the session belongs to %+v", tool.Asked, who)
	}
}

// TestARequestNobodyVouchedForIsRefusedAndNotServedAsAGuest.
//
// The transport used to load the session itself and serve a request that had
// none as a guest of the tenant it was configured with. A route mounted with no
// guard in front of it then answered everybody, as somebody, and nothing said
// so. Now a request that reaches it carrying no subject is refused before its
// body is read, the tool never runs, and the refusal is the router's: a problem
// document for a client that asked for JSON, which every MCP client does.
func TestARequestNobodyVouchedForIsRefusedAndNotServedAsAGuest(t *testing.T) {
	tool := &helpers.Posts{}
	router := helpers.MountUnguarded(helpers.Blog(tool))

	rec := helpers.Send(router, aCall)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a request carrying no subject was answered %d, want %d: %s",
			rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("the refusal challenged with %q, want Bearer", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("a client that asked for JSON was refused as %q, want a problem document", got)
	}
	var problem struct {
		Status int    `json:"status"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("the refusal is not a problem document: %v, %s", err, rec.Body.String())
	}
	if problem.Status != http.StatusUnauthorized {
		t.Errorf("the problem document says %d, want %d", problem.Status, http.StatusUnauthorized)
	}
	if strings.Contains(rec.Body.String(), "jsonrpc") {
		t.Errorf("a request nobody vouched for was answered by the protocol: %s", rec.Body.String())
	}
	if ran(tool) {
		t.Fatalf("a request carrying no subject reached the tool as %+v", tool.Asked)
	}
}

// TestARefusalToAClientThatDidNotAskForJSONIsStillA401, so the status does not
// depend on the Accept header: only the shape of the body does.
func TestARefusalToAClientThatDidNotAskForJSONIsStillA401(t *testing.T) {
	tool := &helpers.Posts{}
	router := helpers.MountUnguarded(helpers.Blog(tool))

	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(aCall))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a request carrying no subject was answered %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("the refusal carries no Bearer challenge: %v", rec.Header())
	}
	if ran(tool) {
		t.Fatalf("a request carrying no subject reached the tool as %+v", tool.Asked)
	}
}

// TestAPublicRouteWithNoSessionIsRefusedRatherThanServed.
//
// LoadSubject lets every request through and puts a subject on the ones that
// have a session. The transport behind it is the case the old guest fallback
// existed for, and it is now the case where a request without a session gets
// a 401 instead of somebody's answers.
func TestAPublicRouteWithNoSessionIsRefusedRatherThanServed(t *testing.T) {
	tool := &helpers.Posts{}
	_, sessions := helpers.MountBehindSession(helpers.Blog(tool))
	router := fhttp.NewRouter()
	router.Action(http.MethodPost, "/mcp", mcp.Web(helpers.Blog(tool)), middleware.LoadSubject(sessions))

	rec := helpers.Send(router, aCall)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a request with no session behind LoadSubject was answered %d, want %d",
			rec.Code, http.StatusUnauthorized)
	}
	if ran(tool) {
		t.Fatalf("a request with no session reached the tool as %+v", tool.Asked)
	}
}

// TestAGuardThatRefusesIsTheAnswerAndTheServerIsNeverReached, so a wrong token
// is the guard's 401 and not a protocol answer about a call that never ran.
func TestAGuardThatRefusesIsTheAnswerAndTheServerIsNeverReached(t *testing.T) {
	tool := &helpers.Posts{}
	router := helpers.MountBehindToken(helpers.Blog(tool), helpers.Bearer())

	rec := helpers.Send(router, aCall, "Authorization", "Bearer not-the-token-anybody-was-given")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a token nobody issued was answered %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if ran(tool) {
		t.Fatalf("a token nobody issued reached the tool as %+v", tool.Asked)
	}
}

// TestASubjectInTheBodyIsNotWhoIsAsking.
//
// The message is the client's to write, so nothing in it can choose the
// subject. A call that names an administrator in its params, in its
// arguments and beside them still runs as the one the token named.
func TestASubjectInTheBodyIsNotWhoIsAsking(t *testing.T) {
	tool := &helpers.Posts{}
	who := auth.Subject{ID: "u1", Tenant: "t1"}
	router := helpers.MountBehindToken(helpers.Blog(tool), who)

	helpers.Send(router, `{"jsonrpc":"2.0","id":1,"method":"tools/call","subject":{"ID":"root","Tenant":"t0"},`+
		`"params":{"name":"list_posts","subject":"root","tenant":"t0","arguments":{}}}`, helpers.WithToken()...)

	if tool.Asked.ID != who.ID || tool.Asked.Tenant != who.Tenant {
		t.Fatalf("the tool ran as %+v, and the token named %+v", tool.Asked, who)
	}
}

// ran reports whether the tool was called at all: Handle records the subject it
// was called as, and every subject a transport ever served -- a guest included
// -- names an account or a tenant.
func ran(tool *helpers.Posts) bool { return tool.Asked.ID != "" || tool.Asked.Tenant != "" }
