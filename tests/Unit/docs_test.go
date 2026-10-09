package unit

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"

	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

func TestPublicDocumentationDoesNotPromiseUnregisteredAruMCPCommands(t *testing.T) {
	root := moduleRoot(t)
	for _, name := range []string{"README.md", "transport.go"} {
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if strings.Contains(string(body), "aru mcp:") {
			t.Errorf("%s promises an Aru MCP command that no command registry provides", name)
		}
	}
}

// The three strings the package doc is held to, all of them about the same
// sentence: what the Server does when a tool asks no policy.
//
// promisedGuarantee is what the published doc used to open with, and it is
// blocked by its exact words because those words shipped. disclosure is the
// clause that replaced it, and requiredHeading is the section it lives under.
// A rewrite that keeps the doc honest keeps both; one that puts a guarantee
// back has to delete the disclosure to read coherently, and deleting it fails
// here. That is what this can check by reading -- it cannot rule out a new
// promise worded some third way, which is why it measures the server first and
// only then reads.
const (
	promisedGuarantee = "Every tool carries a Grant"
	requiredHeading   = "# What this package checks, and what it does not"
	disclosure        = "runs no policy of its own"
)

// TestThePackageDocDescribesTheAuthorizationTheServerPerforms.
//
// The package doc is the first thing a reader of the published reference sees,
// so a guarantee written there reaches a consumer before any code does. This
// dispatches a tool with no policy in its path, reads what the server did about
// it, and holds the doc to the answer -- in both directions, so a doc that
// understates a server which does refuse fails here as well.
func TestThePackageDocDescribesTheAuthorizationTheServerPerforms(t *testing.T) {
	doc := packageDoc(t)

	server, tool := helpers.UnpolicedServer(nil)
	answer := server.Call(context.Background(), auth.Subject{}, "list_everything", nil)
	refused := answer.IsError && !tool.Ran

	if refused {
		if strings.Contains(doc, disclosure) {
			t.Errorf("the server refused a tool that asks no policy, and the package doc still says it %q", disclosure)
		}
		return
	}

	if strings.Contains(doc, promisedGuarantee) {
		t.Errorf("the package doc promises %q, and a tool that asks no policy was dispatched and answered %q",
			promisedGuarantee, answer.Text)
	}
	if !strings.Contains(doc, requiredHeading) {
		t.Errorf("the package doc has no %q section, so it does not say where the boundary is", requiredHeading)
	}
	if !strings.Contains(doc, disclosure) {
		t.Errorf("the package doc does not say the server %q, which is what the dispatch above measured", disclosure)
	}
}

// packageDoc is the comment above the package clause of mcp.go, which is the
// text the published reference carries.
func packageDoc(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(moduleRoot(t), "mcp.go"))
	if err != nil {
		t.Fatalf("reading mcp.go: %v", err)
	}

	clause := strings.Index(string(body), "\npackage mcp\n")
	if clause < 0 {
		t.Fatal("mcp.go has no package clause, so there is no doc comment above one")
	}
	return string(body)[:clause]
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime did not report the test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
}
