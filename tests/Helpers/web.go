// The router the suite reaches the web transport through.
//
// The transport serves the subject a guard put on the request and loads none
// of its own, so every route here is mounted the way an application mounts it:
// behind the framework's bearer-token guard, or its session guard, or -- for
// the test that the transport refuses a request nobody vouched for -- behind
// nothing at all.

package helpers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth"

	"github.com/arandu-io/mcp"
)

// Token is the one bearer token the suite's resolver knows.
//
// It is random-looking and long because the guard's argument for an unsalted
// digest holds only for a token nobody can guess, and a test that used a short
// one would be the example somebody copies.
const Token = "mcp-suite-7f3c9a1e5b2d4f6a8c0e1b3d5f7a9c2e4b6d8f0a"

// Bearer is the subject Token names: an editor of tenant t1, so a policy that
// asks for the role has somebody to allow.
func Bearer() auth.Subject {
	return auth.Subject{ID: "u1", Tenant: "t1", Roles: []string{EditorRole}}
}

// tokens resolves Token to whom it was given to and every other digest to
// nobody, which is the whole of what an application's token store answers.
type tokens map[middleware.TokenDigest]auth.Subject

// ResolveToken answers the subject a digest was issued to.
func (t tokens) ResolveToken(_ context.Context, digest middleware.TokenDigest) (auth.Subject, error) {
	if subject, ok := t[digest]; ok {
		return subject, nil
	}
	return auth.Subject{}, middleware.ErrUnknownToken
}

// MountBehindToken returns a router serving the server at POST /mcp behind
// RequireToken, where Token names who.
func MountBehindToken(server *mcp.Server, who auth.Subject) *fhttp.Router {
	router := fhttp.NewRouter()
	router.Action(http.MethodPost, "/mcp", mcp.Web(server),
		middleware.RequireToken(tokens{middleware.DigestToken(Token): who}))
	return router
}

// MountBehindSession returns a router serving the server at POST /mcp behind
// RequireAuth, and the store whose sessions it admits.
func MountBehindSession(server *mcp.Server) (*fhttp.Router, *security.SessionStore) {
	sessions := security.NewSessionStore(bytes.Repeat([]byte("k"), 32), time.Hour, false, nil)
	router := fhttp.NewRouter()
	router.Action(http.MethodPost, "/mcp", mcp.Web(server), middleware.RequireAuth(sessions))
	return router, sessions
}

// MountUnguarded returns a router serving the server at POST /mcp with nothing
// in front of it: the mistake the transport has to refuse rather than serve.
func MountUnguarded(server *mcp.Server) *fhttp.Router {
	router := fhttp.NewRouter()
	router.Action(http.MethodPost, "/mcp", mcp.Web(server))
	return router
}

// Send posts one message to the router the way a standard client posts it --
// a JSON body, and an Accept naming JSON and an event stream -- with the
// headers given on top, and returns what came back.
func Send(router http.Handler, body string, headers ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)
	return rec
}

// WithToken is the header pair that presents Token.
func WithToken() []string { return []string{"Authorization", "Bearer " + Token} }

// Post sends one message to Everything over the web transport, presenting the
// token, and returns what came back.
func Post(body string) *httptest.ResponseRecorder {
	return Send(MountBehindToken(Everything(), Bearer()), body, WithToken()...)
}
