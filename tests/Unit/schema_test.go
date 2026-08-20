package unit

import (
	"strings"
	"testing"

	"github.com/arandu-io/mcp"
)

// TestARequiredArgumentThatWasNotSentIsRefused.
//
// This is the check that lets a tool read an argument without testing whether
// it arrived. A call is validated before Handle runs, so a tool that declared an
// argument required and reads it is reading something that is there -- and if
// this stops refusing, the tool reads a zero value instead and acts on a call
// that named nothing. A missing string is "" and a missing number is 0, neither
// of which a tool can tell from a value somebody sent.
func TestARequiredArgumentThatWasNotSentIsRefused(t *testing.T) {
	schema := mcp.Object(
		mcp.String("slug", "The post to read").Required(),
		mcp.Bool("drafts", "Include drafts"),
	)

	err := schema.Validate(map[string]any{"drafts": true})
	if err == nil {
		t.Fatal("a call that left out a required argument was accepted")
	}
	if !strings.Contains(err.Error(), "slug") {
		t.Errorf("the refusal does not name the argument that is missing: %v", err)
	}

	// And the same call with it is not refused, so "required" is a statement
	// about one argument and not a way to refuse everything.
	if err := schema.Validate(map[string]any{"slug": "hello", "drafts": true}); err != nil {
		t.Errorf("a call carrying every required argument was refused: %v", err)
	}
}

// TestTheModelIsToldWhichArgumentsAreRequired.
//
// Refusing the call is the second half. The first is that the schema the client
// reads says which arguments there is no point calling without -- a model that
// is not told sends the call, is refused, and guesses at the correction.
func TestTheModelIsToldWhichArgumentsAreRequired(t *testing.T) {
	rendered := mcp.Object(
		mcp.String("status", "Which posts to list").Enum("published", "draft"),
		mcp.String("slug", "The post to read").Required(),
	).JSON()

	required, ok := rendered["required"].([]string)
	if !ok {
		t.Fatalf("the schema declares no required arguments: %v", rendered)
	}
	if len(required) != 1 || required[0] != "slug" {
		t.Errorf("the required arguments are %v, want just slug", required)
	}

	properties, ok := rendered["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the schema declares no properties: %v", rendered)
	}
	status, ok := properties["status"].(map[string]any)
	if !ok {
		t.Fatalf("status is not in the schema: %v", properties)
	}
	// The closed list travels with it. A model given one picks from it, and a
	// model given "the status" invents a value.
	if _, ok := status["enum"]; !ok {
		t.Errorf("the closed list did not reach the client: %v", status)
	}
}

// TestABooleanArgumentIsCheckedAsOne.
//
// Every declared type is checked before Handle, and this is the one nothing else
// in the suite reaches. A string where a boolean belongs reaching a tool is a
// tool reading false from "true".
func TestABooleanArgumentIsCheckedAsOne(t *testing.T) {
	schema := mcp.Object(mcp.Bool("drafts", "Include drafts"))

	err := schema.Validate(map[string]any{"drafts": "true"})
	if err == nil {
		t.Fatal("a string was accepted where a boolean was declared")
	}
	if !strings.Contains(err.Error(), "drafts") {
		t.Errorf("the refusal does not name the argument: %v", err)
	}

	if err := schema.Validate(map[string]any{"drafts": true}); err != nil {
		t.Errorf("a boolean was refused where a boolean was declared: %v", err)
	}
}
