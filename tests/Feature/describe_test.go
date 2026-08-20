package feature

import (
	"bytes"
	"strings"
	"testing"

	"github.com/arandu-io/mcp"
	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// TestDescribeNamesEverythingAServerOffers.
//
// The question it answers is asked far more often than it is answered by
// connecting an assistant, and an answer that leaves a kind out sends whoever
// read it to look for a tool that is there.
func TestDescribeNamesEverythingAServerOffers(t *testing.T) {
	var out bytes.Buffer
	mcp.Describe(helpers.Everything(), &out)

	printed := out.String()
	for _, want := range []string{
		"blog",          // the server
		"list_posts",    // the tool
		"status",        // and the arguments a caller has to fill in
		"blog://readme", // the resource
		"summarise",     // the prompt
		"The posts of a blog.",
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("%q is not in what the server says it offers:\n%s", want, printed)
		}
	}
}
