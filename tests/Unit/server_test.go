package unit

import (
	"context"
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

// declared is a prompt whose name and arguments a test chooses, so a
// declaration can be wrong in each of the ways a boot check is for.
type declared struct {
	name string
	args []mcp.Argument
}

// Name, Description, Arguments and Render make it a prompt.
func (p declared) Name() string              { return p.name }
func (p declared) Description() string       { return "A prompt." }
func (p declared) Arguments() []mcp.Argument { return p.args }
func (p declared) Render(context.Context, mcp.Request) ([]mcp.Message, error) {
	return []mcp.Message{mcp.User("hello")}, nil
}

// TestAPromptDeclarationIsCheckedAtBoot.
//
// Every one of these is a mistake in a declaration rather than in a call, so it
// is the same class as two tools with one name: the server starts and then
// answers nonsense about it. Two prompts with one name means which one renders
// is the order of a slice; two arguments with one name means the client is told
// to send a member twice and only one of them can arrive; an argument with no
// name is one no call can ever carry, and it is listed to the model anyway.
func TestAPromptDeclarationIsCheckedAtBoot(t *testing.T) {
	for _, bad := range []struct {
		about   string
		prompts []mcp.Prompt
		says    string
	}{
		{
			"two prompts with one name",
			[]mcp.Prompt{declared{name: "summarise"}, declared{name: "summarise"}},
			"summarise",
		},
		{
			"a prompt with no name",
			[]mcp.Prompt{declared{name: ""}},
			"name",
		},
		{
			"two arguments with one name",
			[]mcp.Prompt{declared{name: "summarise", args: []mcp.Argument{{Name: "slug"}, {Name: "slug"}}}},
			"slug",
		},
		{
			"an argument with no name",
			[]mcp.Prompt{declared{name: "summarise", args: []mcp.Argument{{Description: "The post"}}}},
			"name",
		},
	} {
		err := (&mcp.Server{Name: "blog", Prompts: bad.prompts}).Validate()
		if err == nil {
			t.Errorf("%s was accepted", bad.about)
			continue
		}
		if !strings.Contains(err.Error(), bad.says) {
			t.Errorf("%s was refused without saying what is wrong: %v", bad.about, err)
		}
	}

	// A declaration with nothing wrong with it still boots, so the checks above
	// are about a mistake and not a way to refuse every prompt.
	good := &mcp.Server{Name: "blog", Prompts: []mcp.Prompt{
		declared{name: "summarise", args: []mcp.Argument{{Name: "slug", Required: true}, {Name: "style"}}},
		declared{name: "translate", args: []mcp.Argument{{Name: "slug", Required: true}}},
	}}
	if err := good.Validate(); err != nil {
		t.Errorf("a declaration with nothing wrong with it was refused: %v", err)
	}
}
