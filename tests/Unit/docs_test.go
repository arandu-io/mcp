package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime did not report the test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
}
