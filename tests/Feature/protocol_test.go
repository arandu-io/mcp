package feature

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/arandu-io/framework/security"

	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// TestANotificationIsNotAnswered.
//
// The protocol says a message with no id gets no answer. Sending one anyway is
// what makes a client hang up mid-session, and it is invisible until it happens.
func TestANotificationIsNotAnswered(t *testing.T) {
	s := helpers.Blog(&helpers.Posts{})

	if got := s.Handle(context.Background(), security.Subject{ID: "u1"},
		[]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); got != nil {
		t.Errorf("a notification was answered with %s", got)
	}
	if got := s.Handle(context.Background(), security.Subject{ID: "u1"},
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)); got == nil {
		t.Error("a request with an id was not answered")
	}
}

// TestInitializeDeclaresOnlyWhatTheServerHas.
//
// A capability the server cannot serve is one a client asks about once and
// reports as the server being broken.
func TestInitializeDeclaresOnlyWhatTheServerHas(t *testing.T) {
	body := helpers.Blog(&helpers.Posts{}).Handle(context.Background(), security.Subject{ID: "u1"},
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))

	var out struct {
		Result struct {
			Capabilities map[string]any `json:"capabilities"`
			Instructions string         `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("the answer is not JSON: %v", err)
	}

	if _, ok := out.Result.Capabilities["tools"]; !ok {
		t.Error("a server with tools did not declare them")
	}
	if _, ok := out.Result.Capabilities["prompts"]; ok {
		t.Error("a server with no prompts declared them anyway")
	}
	if out.Result.Instructions == "" {
		t.Error("the instructions did not reach the client: the model then guesses what this is for")
	}
}

// TestAMemberIsTheOneItIsNamed.
//
// encoding/json fills a struct field from a member whose name differs only in
// case, and the protocol's members do not differ only in case. A message that
// reads "method":"ping" and runs a tool is a message that whatever stands in
// front of this server records as a ping.
func TestAMemberIsTheOneItIsNamed(t *testing.T) {
	tool := &helpers.Posts{}
	s := helpers.Blog(tool)
	who := security.Subject{ID: "u1", Tenant: "t1"}

	ran := s.Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"ping","METHOD":"tools/call",`+
			`"params":{"name":"list_posts","arguments":{}}}`))
	if strings.Contains(string(ran), "one post") {
		t.Errorf("a message that names ping called a tool: %s", ran)
	}
	if tool.Asked.ID != "" {
		t.Error("a message that names ping reached a tool")
	}

	if got := s.Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","method":"ping","ID":7}`)); got != nil {
		t.Errorf("a notification was answered because a member differed in case: %s", got)
	}

	spelt := s.Handle(context.Background(), who,
		[]byte(`{"jsonrpC":"2.0","id":1,"method":"ping"}`))
	if !strings.Contains(string(spelt), "-32600") {
		t.Errorf("a message that names no protocol was accepted: %s", spelt)
	}
}

// TestAnAnswerAlwaysCarriesAnID.
//
// A client keys the calls it is waiting on by id. An answer without one is an
// answer it has nowhere to put, so the protocol asks for the member to be there
// and null when the request's could not be read.
func TestAnAnswerAlwaysCarriesAnID(t *testing.T) {
	s := helpers.Blog(&helpers.Posts{})

	for _, body := range []string{
		``,
		`{`,
		`[1,2,3]`,
		`{}`,
		`{"jsonrpc":"1.0","method":"ping"}`,
		`{"jsonrpc":"2.0","id":4,"method":"nope"}`,
	} {
		answer := s.Handle(context.Background(), security.Subject{ID: "u1"}, []byte(body))
		if answer == nil {
			t.Errorf("%q got no answer at all", body)
			continue
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Errorf("%q was answered with something that is not JSON: %v", body, err)
			continue
		}
		if _, ok := out["id"]; !ok {
			t.Errorf("%q was answered without an id: %s", body, answer)
		}
	}
}

// TestAnIDIsAStringANumberOrNull.
//
// The protocol carries those three and nothing else there, and the id is the
// member a client matches an answer to the call it is waiting on by. An id it
// cannot key on is an answer it never delivers, and the call it belongs to waits
// until something gives up -- so the message is refused while there is still an
// answer to refuse it with, rather than acted on and answered unmatchably.
func TestAnIDIsAStringANumberOrNull(t *testing.T) {
	tool := &helpers.Posts{}
	s := helpers.Blog(tool)
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, id := range []string{`{"a":1}`, `{}`, `[1,2]`, `[]`, `true`, `false`} {
		body := `{"jsonrpc":"2.0","id":` + id + `,"method":"ping"}`

		answer := s.Handle(context.Background(), who, []byte(body))
		if answer == nil {
			t.Errorf("an id of %s got no answer at all, and a request is not a notification", id)
			continue
		}
		var out helpers.AnswerShape
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Errorf("an id of %s was answered with something that is not JSON: %v", id, err)
			continue
		}
		if out.Error == nil {
			t.Errorf("an id of %s was accepted: %s", id, answer)
			continue
		}
		if out.Error.Code != helpers.CodeInvalidRequest {
			t.Errorf("an id of %s was answered with %d, and the message was JSON: %s",
				id, out.Error.Code, answer)
		}
		// Null rather than the id that arrived: the client is told which call
		// failed by there being no call, because there was no id to name one.
		if string(out.ID) != "null" {
			t.Errorf("an id of %s came back as %s, which a client cannot key on", id, out.ID)
		}
	}

	// A message refused for its id is not a message that ran.
	if got := s.Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":[1],"method":"tools/call",`+
			`"params":{"name":"list_posts","arguments":{}}}`)); strings.Contains(string(got), "one post") {
		t.Errorf("a message with an id nobody can match called a tool: %s", got)
	}
	if tool.Asked.ID != "" {
		t.Error("a message with an id nobody can match reached a tool")
	}

	// The three the protocol does carry still arrive, and come back as they were
	// sent. The large one is why the check reads a number as a number: it does
	// not fit a float, and a check that used one would refuse it.
	for _, id := range []string{`1`, `-3`, `0`, `1.5`, `1e999`, `"abc"`, `""`, `null`} {
		body := `{"jsonrpc":"2.0","id":` + id + `,"method":"ping"}`

		answer := s.Handle(context.Background(), who, []byte(body))
		if answer == nil {
			t.Errorf("an id of %s got no answer at all", id)
			continue
		}
		var out helpers.AnswerShape
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Errorf("an id of %s was answered with something that is not JSON: %v", id, err)
			continue
		}
		if out.Error != nil {
			t.Errorf("an id of %s was refused: %s", id, answer)
			continue
		}
		if string(out.ID) != id {
			t.Errorf("an id of %s came back as %s", id, out.ID)
		}
	}
}

// TestANotificationThatCarriesAnIDIsRefused.
//
// A notification carries no id: that is the whole of what makes it one, and the
// protocol says so of every method under the prefix. A message that names one
// and carries an id is neither, and answering it hands a client an answer to a
// call it never made -- which it has nowhere to put, and which the strict ones
// treat as the connection having gone wrong.
func TestANotificationThatCarriesAnIDIsRefused(t *testing.T) {
	s := helpers.Blog(&helpers.Posts{})
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, method := range []string{
		"notifications/initialized",
		"notifications/cancelled",
		"notifications/tools/list_changed",
	} {
		answer := s.Handle(context.Background(), who,
			[]byte(`{"jsonrpc":"2.0","id":1,"method":"`+method+`"}`))
		if answer == nil {
			t.Errorf("%s with an id got no answer, and a message with an id is not a notification", method)
			continue
		}

		var out helpers.AnswerShape
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Errorf("%s was answered with something that is not JSON: %v", method, err)
			continue
		}
		if out.Error == nil {
			t.Errorf("%s with an id was answered with a result: %s", method, answer)
			continue
		}
		if out.Error.Code != helpers.CodeInvalidRequest {
			t.Errorf("%s with an id was answered with %d: the id is what is wrong with the message, "+
				"not the parameters of a method", method, out.Error.Code)
		}
		// The id arrived readable, so it comes back: the client is told which of
		// its calls this refusal belongs to.
		if string(out.ID) != "1" {
			t.Errorf("%s came back with an id of %s, which a client cannot key on", method, out.ID)
		}
	}

	// Without the id it is a notification again, and a notification is answered
	// by silence.
	if got := s.Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); got != nil {
		t.Errorf("a notification was answered with %s", got)
	}
}

// TestParamsThatAreNotAnObjectAreRefused.
//
// A member that is not an object has no members to read, so every method below
// reads it as a call that arrived with nothing in it -- and answers by naming
// the parameter that went missing. That sends whoever is reading to look at the
// one part of the message that was fine, while the part that was wrong is not
// mentioned at all.
func TestParamsThatAreNotAnObjectAreRefused(t *testing.T) {
	tool := &helpers.Posts{}
	s := helpers.Blog(tool)
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, params := range []string{`7`, `"list_posts"`, `true`, `""`, `1.5`} {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + params + `}`

		answer := s.Handle(context.Background(), who, []byte(body))
		if answer == nil {
			t.Errorf("params of %s got no answer at all", params)
			continue
		}

		var out helpers.AnswerShape
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Errorf("params of %s was answered with something that is not JSON: %v", params, err)
			continue
		}
		if out.Error == nil {
			t.Errorf("params of %s was accepted: %s", params, answer)
			continue
		}
		if out.Error.Code != helpers.CodeInvalidRequest {
			t.Errorf("params of %s was answered with %d: what is wrong is the shape of the message, "+
				"not a parameter of a method that was reached", params, out.Error.Code)
		}
		if !strings.Contains(out.Error.Message, "params") {
			t.Errorf("params of %s was refused without naming params: %q", params, out.Error.Message)
		}
		// The mistake being reported is the one that was made. Naming a tool
		// here is the answer this test exists to keep from coming back: it
		// reports the name that was never sent instead of the members that were.
		if strings.Contains(out.Error.Message, "tool") {
			t.Errorf("params of %s was reported as a problem with a tool name: %q", params, out.Error.Message)
		}
	}

	// A message whose params are an object still reaches the tool, because the
	// check refuses a shape and not a call.
	ran := s.Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call",`+
			`"params":{"name":"list_posts","arguments":{"status":"draft"}}}`))
	if !strings.Contains(string(ran), "one post") {
		t.Errorf("a call with parameters this server can read was refused: %s", ran)
	}
	if tool.Asked.ID != "u1" {
		t.Error("a call with parameters this server can read did not reach the tool")
	}
}

// TestPositionalParamsAreRefusedRatherThanIgnored.
//
// JSON-RPC carries parameters by position as well as by name, so an array is not
// a malformed message by that standard. It is unreadable by this one: every
// method here names its parameters and none declares an order, so there is
// nothing to match the first element to. Reading it as no parameters at all is
// the one answer that is worse than refusing it -- the call proceeds, missing
// everything it was given.
func TestPositionalParamsAreRefusedRatherThanIgnored(t *testing.T) {
	tool := &helpers.Posts{}
	s := helpers.Blog(tool)
	who := security.Subject{ID: "u1", Tenant: "t1"}

	answer := s.Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":["list_posts",{"status":"draft"}]}`))

	var out helpers.AnswerShape
	if err := json.Unmarshal(answer, &out); err != nil {
		t.Fatalf("positional params were answered with something that is not JSON: %v, %s", err, answer)
	}
	if out.Error == nil {
		t.Fatalf("positional params were accepted: %s", answer)
	}
	if out.Error.Code != helpers.CodeInvalidRequest {
		t.Errorf("positional params were answered with %d, and what arrived was not a request "+
			"this protocol carries", out.Error.Code)
	}
	// A client that sent a well-formed JSON-RPC message is told why it is not a
	// well-formed one here, or it sends the same thing again.
	if !strings.Contains(out.Error.Message, "object") {
		t.Errorf("positional params were refused without saying what is expected instead: %q", out.Error.Message)
	}
	if tool.Asked.ID != "" {
		t.Error("a call whose parameters could not be read reached the tool anyway")
	}
}

// TestArgumentsThatAreNotAnObjectAreRefused: the same mistake as params, one
// level down, and worse there.
//
// A params that is not an object is refused before any method reads it. An
// arguments that is not an object was decoded into nothing and the call went on,
// so a tool whose arguments are all optional passed its own schema and ran --
// answering a result to a message this server could not read. A call the client
// did not make was performed, as the subject the client carried.
//
// The milder half is a tool that declares a required argument: it is told the
// argument is missing, which sends whoever reads that looking for the member
// they did send.
func TestArgumentsThatAreNotAnObjectAreRefused(t *testing.T) {
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, arguments := range []string{`[1,2]`, `["status","draft"]`, `"draft"`, `7`, `true`} {
		tool := &helpers.Posts{}
		s := helpers.Blog(tool)

		answer := s.Handle(context.Background(), who,
			[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call",`+
				`"params":{"name":"list_posts","arguments":`+arguments+`}}`))

		var out helpers.AnswerShape
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Errorf("arguments of %s were answered with something that is not JSON: %v", arguments, err)
			continue
		}
		if out.Error == nil {
			t.Errorf("arguments of %s were accepted: %s", arguments, answer)
		} else {
			if out.Error.Code != helpers.CodeInvalidRequest {
				t.Errorf("arguments of %s were answered with %d: what is wrong is the shape of the "+
					"message, not a parameter the tool declared", arguments, out.Error.Code)
			}
			if !strings.Contains(out.Error.Message, "arguments") {
				t.Errorf("arguments of %s were refused without naming arguments: %q",
					arguments, out.Error.Message)
			}
		}
		if tool.Asked.ID != "" {
			t.Errorf("arguments of %s reached the tool, which ran on a message this server "+
				"could not read", arguments)
		}
	}

	// The check refuses a shape and not a call: arguments this server can read
	// still reach the tool, and a member that is absent or null still says the
	// call carries none -- which the tool's schema, declaring nothing required,
	// accepts.
	for _, params := range []string{
		`{"name":"list_posts","arguments":{"status":"draft"}}`,
		`{"name":"list_posts","arguments":null}`,
		`{"name":"list_posts"}`,
	} {
		tool := &helpers.Posts{}
		ran := helpers.Blog(tool).Handle(context.Background(), who,
			[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+params+`}`))

		if !strings.Contains(string(ran), "one post") {
			t.Errorf("params of %s were refused, and this server can read them: %s", params, ran)
		}
		if tool.Asked.ID != "u1" {
			t.Errorf("params of %s did not reach the tool", params)
		}
	}
}

// TestAPromptWhoseArgumentsCannotBeReadIsNotRendered.
//
// The same member, on the other method that carries it. A prompt is rendered
// into messages a model is about to act on, so one built from arguments that
// were dropped is a conversation started about the wrong thing -- and it looks
// like an answer, because it is one.
func TestAPromptWhoseArgumentsCannotBeReadIsNotRendered(t *testing.T) {
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, arguments := range []string{`[1,2]`, `"x"`, `7`, `true`} {
		answer := helpers.Everything().Handle(context.Background(), who,
			[]byte(`{"jsonrpc":"2.0","id":1,"method":"prompts/get",`+
				`"params":{"name":"summarise","arguments":`+arguments+`}}`))

		var out helpers.AnswerShape
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Errorf("arguments of %s were answered with something that is not JSON: %v", arguments, err)
			continue
		}
		// A rendered prompt comes back as a result. An error is the only answer
		// that means the messages were not built.
		if out.Error == nil {
			t.Errorf("a prompt was rendered from arguments of %s: %s", arguments, answer)
			continue
		}
		if out.Error.Code != helpers.CodeInvalidRequest {
			t.Errorf("arguments of %s were answered with %d, and what arrived was not a request "+
				"this protocol carries", arguments, out.Error.Code)
		}
	}

	// Arguments this server can read still render, and the prompt still receives
	// them rather than an empty map.
	ran := helpers.Everything().Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"prompts/get",`+
			`"params":{"name":"summarise","arguments":{"slug":"a-post"}}}`))
	if !strings.Contains(string(ran), "Summarise a-post") {
		t.Errorf("a prompt with arguments this server can read was not rendered from them: %s", ran)
	}
}

// TestANotificationIsAnsweredBySilenceHoweverWrongItIs.
//
// A refusal is still an answer, and a notification gets none. The sender said it
// is not listening, so the only thing left to do about its mistake is to not
// perform it -- and a server that makes an exception for the mistakes it finds
// interesting is a server that writes to a client counting bytes it never asked
// for.
func TestANotificationIsAnsweredBySilenceHoweverWrongItIs(t *testing.T) {
	tool := &helpers.Posts{}
	s := helpers.Blog(tool)
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, body := range []string{
		`{"jsonrpc":"2.0","method":"tools/call","params":7}`,
		`{"jsonrpc":"2.0","method":"tools/call","params":["list_posts",{}]}`,
	} {
		if got := s.Handle(context.Background(), who, []byte(body)); got != nil {
			t.Errorf("%s was answered with %s", body, got)
		}
	}
	if tool.Asked.ID != "" {
		t.Error("a notification whose parameters could not be read reached the tool")
	}
}

// TestParamsThatAreAbsentAndParamsThatAreNullAreTheSame.
//
// The protocol allows the member to be left out, and null is how a client whose
// encoder always writes it says exactly that. Unlike the id, where absent and
// null are two different messages, nothing here can act differently on the two:
// both say the call carries no parameters, and refusing one of them would refuse
// a call that is missing nothing.
func TestParamsThatAreAbsentAndParamsThatAreNullAreTheSame(t *testing.T) {
	s := helpers.Blog(&helpers.Posts{})
	who := security.Subject{ID: "u1", Tenant: "t1"}

	absent := s.Handle(context.Background(), who, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	null := s.Handle(context.Background(), who, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":null}`))

	if string(absent) != string(null) {
		t.Errorf("no params answered %s and null params answered %s", absent, null)
	}
	if strings.Contains(string(absent), "error") {
		t.Errorf("a call that names no parameters was refused: %s", absent)
	}
}

// TestAMemberNobodyNamedIsCarriedRatherThanRefused.
//
// The protocol closes none of its message objects: params is declared to carry
// members beyond the ones named, and nothing anywhere forbids the message itself
// from doing the same. A server that refused what it did not recognise would
// refuse the revision after this one, which is the failure that cannot be fixed
// from the side that sees it.
//
// Ignoring the member is not the same as trusting it: it reaches nothing, which
// the second half of this test is what pins.
func TestAMemberNobodyNamedIsCarriedRatherThanRefused(t *testing.T) {
	tool := &helpers.Posts{}
	s := helpers.Blog(tool)
	who := security.Subject{ID: "u1", Tenant: "t1"}

	answer := s.Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"ping","nonsense":{"a":1},"_meta":{"progressToken":9}}`))

	var out helpers.AnswerShape
	if err := json.Unmarshal(answer, &out); err != nil {
		t.Fatalf("a message with a member nobody named was answered with something that is not JSON: %v", err)
	}
	if out.Error != nil {
		t.Fatalf("a member nobody named was refused, and the next revision of the protocol adds one: %s", answer)
	}

	// What is unknown is ignored, and ignored means it reaches nothing. A member
	// that is not read cannot name a tool, whatever it is spelt like.
	if tool.Asked.ID != "" {
		t.Error("a member nobody named reached a tool")
	}
}

// TestAMessageWithNoMethodSaysSo.
//
// The usual message with no method is an answer that arrived where a call was
// expected, and reporting that the method named by the empty string is not
// implemented sends the reader looking for a method.
func TestAMessageWithNoMethodSaysSo(t *testing.T) {
	answer := helpers.Blog(&helpers.Posts{}).Handle(context.Background(), security.Subject{ID: "u1"},
		[]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))

	if !strings.Contains(string(answer), "no method") {
		t.Errorf("a message with no method was answered with %s", answer)
	}
}

// TestOnlySomethingThatIsNotJSONIsCalledThat.
//
// "the message is not JSON" about a message that is JSON is a person reading
// their encoder instead of their fields.
func TestOnlySomethingThatIsNotJSONIsCalledThat(t *testing.T) {
	s := helpers.Blog(&helpers.Posts{})

	if got := s.Handle(context.Background(), security.Subject{ID: "u1"},
		[]byte(`{"jsonrpc":"2.0","id":1,"method":5}`)); strings.Contains(string(got), "not JSON") {
		t.Errorf("a message that is JSON was reported as not being JSON: %s", got)
	}
	if got := s.Handle(context.Background(), security.Subject{ID: "u1"},
		[]byte(`{oops`)); !strings.Contains(string(got), "not JSON") {
		t.Errorf("a message that is not JSON was reported as something else: %s", got)
	}
}
