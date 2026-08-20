package unit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/arandu-io/mcp"
)

// TestAStructuredAnswerIsEncodedForTheTool.
//
// Encoding happens here rather than in each tool, so every tool answers the same
// shape. What the model receives is text either way, and text that does not
// parse is a model reading a value out of a broken document.
func TestAStructuredAnswerIsEncodedForTheTool(t *testing.T) {
	out := mcp.JSON(map[string]any{"slug": "hello", "views": 3})

	if out.IsError {
		t.Fatalf("a value that encodes was answered as a failure: %s", out.Text)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(out.Text), &back); err != nil {
		t.Fatalf("the answer is not JSON: %v, %s", err, out.Text)
	}
	if back["slug"] != "hello" {
		t.Errorf("the answer does not carry what it was given: %s", out.Text)
	}
}

// TestAValueThatCannotBeEncodedIsAnAnswerAndNotAPanic.
//
// A tool that hands over something no encoder can write is a mistake in one
// tool. Taking the session down with it makes it a mistake in every one, and
// the client sees a server that stopped rather than a call that failed.
func TestAValueThatCannotBeEncodedIsAnAnswerAndNotAPanic(t *testing.T) {
	out := mcp.JSON(make(chan int))

	if !out.IsError {
		t.Fatalf("a value that cannot be encoded was answered as a result: %s", out.Text)
	}
	if !strings.Contains(out.Text, "encoding") {
		t.Errorf("the failure does not say what went wrong: %q", out.Text)
	}
}

// TestBothRolesAreSpeltTheWayTheProtocolCarriesThem.
//
// A prompt is a conversation, and a conversation has two sides. A role the
// client does not recognise is a turn it drops, which turns a worked example
// into half of one.
func TestBothRolesAreSpeltTheWayTheProtocolCarriesThem(t *testing.T) {
	if got := mcp.User("Summarise this"); got.Role != "user" || got.Text != "Summarise this" {
		t.Errorf("a user turn came out as %+v", got)
	}
	if got := mcp.Assistant("Here is a summary"); got.Role != "assistant" {
		t.Errorf("an assistant turn came out as %+v", got)
	}
}
