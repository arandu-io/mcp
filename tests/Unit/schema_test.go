package unit

import (
	"math"
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

// TestAnIntegerArgumentIsAWholeNumberInRange.
//
// JSON has one numeric type, so 1.9 and 1e100 arrive as perfectly good numbers
// and a check that only asks "is this a number" lets both through. What follows
// is not a rounding: the tool converts, and 1.9 reaches it as 1 while a value
// past the range of an int converts to whatever the machine does with it -- the
// Go specification leaves that to the implementation, so the same message means
// one thing here and another on the deployment host. A tool asked for 1e100
// rows and handed nine quintillion runs a query nobody wrote.
//
// The boundary is checked on both sides because it is where the float stops
// being exact: the largest int64 has no float64 of its own and rounds up to the
// first value past the range, so a check written against it accepts the one
// number it exists to refuse.
func TestAnIntegerArgumentIsAWholeNumberInRange(t *testing.T) {
	schema := mcp.Object(mcp.Int("limit", "How many to return"))

	for _, bad := range []struct {
		about string
		value any
	}{
		{"a fraction", 1.9},
		{"a fraction below one", 0.5},
		{"a negative fraction", -0.5},
		{"a value past the range of an int", 1e100},
		{"a negative value past the range of an int", -1e100},
		{"the first value past the largest int64", 9223372036854775808.0},
		{"not a number at all", math.NaN()},
		{"an infinity", math.Inf(1)},
		{"a negative infinity", math.Inf(-1)},
		{"a string", "10"},
		{"a boolean", true},
	} {
		err := schema.Validate(map[string]any{"limit": bad.value})
		if err == nil {
			t.Errorf("%s was accepted where an integer was declared", bad.about)
			continue
		}
		if !strings.Contains(err.Error(), "limit") {
			t.Errorf("%s was refused without naming the argument: %v", bad.about, err)
		}
	}

	// The numbers that are integers still are, including the two ends of the
	// range and the ones a float64 carries exactly.
	for _, good := range []struct {
		about string
		value any
	}{
		{"zero", 0.0},
		{"a small number", 20.0},
		{"a negative number", -3.0},
		{"a number written with an exponent", 1e15},
		{"the largest float64 that is exactly an integer", 9007199254740992.0},
		{"the smallest int64", -9223372036854775808.0},
		{"an int built in Go rather than decoded", 7},
	} {
		if err := schema.Validate(map[string]any{"limit": good.value}); err != nil {
			t.Errorf("%s was refused where an integer was declared: %v", good.about, err)
		}
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
