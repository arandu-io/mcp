package unit

import (
	"strings"
	"testing"

	"github.com/arandu-io/mcp"
	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// TestAServerWithoutDescriptionsIsRefusedAtBoot.
//
// A description is what the model reads to decide whether to call a tool, and a
// tool without one is called at random. It is a mistake in a declaration, so it
// belongs at boot rather than at the first call.
func TestAServerWithoutDescriptionsIsRefusedAtBoot(t *testing.T) {
	s := &mcp.Server{Name: "blog", Tools: []mcp.Tool{helpers.Undescribed{}}}

	err := s.Validate()
	if err == nil {
		t.Fatal("a tool with no description was accepted")
	}
	if !strings.Contains(err.Error(), "description") {
		t.Errorf("the error does not say what is missing: %v", err)
	}
}

// TestTwoToolsWithOneNameAreRefused: the second shadows the first, and which one
// runs is the order of a slice.
func TestTwoToolsWithOneNameAreRefused(t *testing.T) {
	s := &mcp.Server{Name: "blog", Tools: []mcp.Tool{&helpers.Posts{}, &helpers.Posts{}}}
	if err := s.Validate(); err == nil {
		t.Fatal("two tools with one name were accepted")
	}
}
