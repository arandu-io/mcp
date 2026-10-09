package feature

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/arandu-io/framework/security"

	"github.com/arandu-io/mcp"
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
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize",`+
			`"params":{"protocolVersion":"2024-11-05","capabilities":{},`+
			`"clientInfo":{"name":"a-client","version":"1.0.0"}}}`))

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

// TestInitializeReadsTheParametersItIsGiven.
//
// initialize is the one message whose parameters decide how the rest of the
// session is read, and nothing checked them: a message with none at all was
// answered as a completed handshake. What the client then has is a session it
// believes was negotiated, agreed with a server that never saw a version --
// and the first message that depends on the agreement is where it surfaces,
// which is far away from the message that was wrong.
func TestInitializeReadsTheParametersItIsGiven(t *testing.T) {
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, bad := range []struct {
		about  string
		params string
	}{
		{"no parameters at all", ``},
		{"parameters that name no version", `,"params":{}`},
		{"a version that is not a string", `,"params":{"protocolVersion":20241105}`},
		{"a version that is empty", `,"params":{"protocolVersion":""}`},
		{
			"capabilities that are not an object",
			`,"params":{"protocolVersion":"2024-11-05","capabilities":7}`,
		},
		{
			"a client that describes itself with something that is not an object",
			`,"params":{"protocolVersion":"2024-11-05","clientInfo":"a-client"}`,
		},
	} {
		answer := helpers.Everything().Handle(context.Background(), who,
			[]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"`+bad.params+`}`))

		var out helpers.AnswerShape
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Errorf("%s was answered with something that is not a response: %v", bad.about, err)
			continue
		}
		if out.Error == nil {
			t.Errorf("%s was answered as a completed handshake: %s", bad.about, answer)
			continue
		}
		if out.Error.Code != helpers.CodeInvalidParams {
			t.Errorf("%s was answered with %d, want %d", bad.about, out.Error.Code, helpers.CodeInvalidParams)
		}
	}
}

// TestInitializeAnswersWithTheOneRevisionThisServerSpeaks.
//
// A client asking for a revision this server does not speak is answered with
// the one it does, which is what lets the client decide whether to go on. The
// answer is never the version that was asked for: a server that echoes it
// agrees to a revision it cannot hold up, and the disagreement then shows up as
// a member that is missing rather than as a version that was refused.
func TestInitializeAnswersWithTheOneRevisionThisServerSpeaks(t *testing.T) {
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, asked := range []string{"2024-11-05", "2025-06-18", "1999-01-01"} {
		answer := helpers.Everything().Handle(context.Background(), who,
			[]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize",`+
				`"params":{"protocolVersion":"`+asked+`","capabilities":{}}}`))

		var out struct {
			Result struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"result"`
			Error *struct{} `json:"error"`
		}
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Errorf("a client asking for %s was answered with something that is not a response: %v", asked, err)
			continue
		}
		if out.Error != nil {
			t.Errorf("a client asking for %s was refused: %s", asked, answer)
			continue
		}
		if out.Result.ProtocolVersion != mcp.Version {
			t.Errorf("a client asking for %s was answered with %q, want %q",
				asked, out.Result.ProtocolVersion, mcp.Version)
		}
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

// TestAnIDIsAStringOrANumber.
//
// The revision this server speaks carries those two there and nothing else --
// not null, which base JSON-RPC allows and this revision's RequestId leaves
// out. The id is the member a client matches an answer to the call it is
// waiting on by: one it cannot key on is an answer it never delivers, and the
// call it belongs to waits until something gives up. So the message is refused
// while there is still an answer to refuse it with, rather than acted on and
// answered unmatchably.
//
// Null is the one worth naming. A client that sends it is not sending a
// notification -- it is waiting -- and answering it as though it were a request
// hands back an answer keyed on nothing.
func TestAnIDIsAStringOrANumber(t *testing.T) {
	tool := &helpers.Posts{}
	s := helpers.Blog(tool)
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, id := range []string{`{"a":1}`, `{}`, `[1,2]`, `[]`, `true`, `false`, `null`, ` null `} {
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

	// The two the protocol does carry still arrive, and come back as they were
	// sent. The long one is why a number is read as a json.Number and not into a
	// float: it does not fit one, and it is echoed and never counted with, so
	// refusing it would answer a well-formed message with a complaint about the
	// one member that was fine.
	for _, id := range []string{
		`1`, `-3`, `0`, `1.5`, `-2.5`, `1e999`, `2` + strings.Repeat(`0`, 308), `"abc"`, `""`, `"null"`,
	} {
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

// TestAnIDThatIsNullIsNotAMissingOne.
//
// The member being absent and the member being null are two different messages,
// and this is the one place in the protocol where that difference decides
// everything: absent means the sender is not listening, and null means it is
// listening and named the call nothing. Reading them the same way either
// answers a notification, which makes a strict client hang up, or leaves a
// request unanswered forever.
func TestAnIDThatIsNullIsNotAMissingOne(t *testing.T) {
	s := helpers.Blog(&helpers.Posts{})
	who := security.Subject{ID: "u1", Tenant: "t1"}

	answer := s.Handle(context.Background(), who, []byte(`{"jsonrpc":"2.0","id":null,"method":"ping"}`))
	if answer == nil {
		t.Fatal("a message carrying a null id was read as a notification and answered by silence")
	}
	var out helpers.AnswerShape
	if err := json.Unmarshal(answer, &out); err != nil {
		t.Fatalf("a null id was answered with something that is not a response: %v", err)
	}
	if out.Error == nil {
		t.Fatalf("a null id was answered with a result a client cannot key on: %s", answer)
	}

	// And leaving the member out is still a notification, which is answered by
	// nothing at all.
	if got := s.Handle(context.Background(), who, []byte(`{"jsonrpc":"2.0","method":"ping"}`)); got != nil {
		t.Errorf("a message with no id member was answered with %s", got)
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
			t.Errorf("params of %s was answered with %d: a params that is not a structured value "+
				"makes the message not a request, which is a different mistake from parameters "+
				"a method cannot use", params, out.Error.Code)
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
	if out.Error.Code != helpers.CodeInvalidParams {
		t.Errorf("positional params were answered with %d, and the parameters are what could not "+
			"be read", out.Error.Code)
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
			if out.Error.Code != helpers.CodeInvalidParams {
				t.Errorf("arguments of %s were answered with %d, and the parameters of the call are "+
					"what could not be read", arguments, out.Error.Code)
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
		if out.Error.Code != helpers.CodeInvalidParams {
			t.Errorf("arguments of %s were answered with %d, and the parameters of the call are "+
				"what could not be read", arguments, out.Error.Code)
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

// promptMessages reads the messages a prompts/get result carried, and reports
// whether the answer was a result at all.
func promptMessages(t *testing.T, body []byte) ([]json.RawMessage, *helpers.AnswerShape) {
	t.Helper()

	var answer helpers.AnswerShape
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("the answer is not a response: %v, %s", err, body)
	}
	if answer.Error != nil {
		return nil, &answer
	}
	var result struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(answer.Result, &result); err != nil {
		t.Fatalf("the result is not the shape prompts/get carries: %v, %s", err, body)
	}
	return result.Messages, &answer
}

// TestAPromptThatWasNotRenderedIsNotAnsweredWithAnEmptyConversation.
//
// A prompt nobody may have and a prompt that broke both came back as a result
// carrying no messages, which is the same answer a prompt that had nothing to
// say gives. The client cannot tell the three apart, so a refusal reads as a
// conversation with nothing in it -- the empty-list mistake this server refuses
// to make for a tool, made for a prompt instead.
func TestAPromptThatWasNotRenderedIsNotAnsweredWithAnEmptyConversation(t *testing.T) {
	who := security.Subject{ID: "u1", Tenant: "t1"}

	refused := helpers.Conversations().Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"unrenderable"}}`))

	messages, answer := promptMessages(t, refused)
	if answer.Error == nil {
		t.Fatalf("a prompt that refused was answered with %d messages and no failure: %s",
			len(messages), refused)
	}
	if answer.Error.Code != helpers.CodeInternal {
		t.Errorf("a prompt that refused was answered with %d, want %d",
			answer.Error.Code, helpers.CodeInternal)
	}
	if !strings.Contains(answer.Error.Message, "not allowed") {
		t.Errorf("the failure does not say what happened: %q", answer.Error.Message)
	}
}

// TestAPromptNobodyDeclaredIsAFailureAndNotAnEmptyConversation.
//
// The name came from the model, so it is the one thing a client can correct.
// Answered as a result with no messages, there is nothing to correct: the model
// reads a prompt that exists and is empty.
func TestAPromptNobodyDeclaredIsAFailureAndNotAnEmptyConversation(t *testing.T) {
	who := security.Subject{ID: "u1", Tenant: "t1"}

	missing := helpers.Conversations().Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"summarize"}}`))

	_, answer := promptMessages(t, missing)
	if answer.Error == nil {
		t.Fatalf("a prompt nobody declared was answered with a result: %s", missing)
	}
	if answer.Error.Code != helpers.CodeInvalidParams {
		t.Errorf("a prompt nobody declared was answered with %d, want %d",
			answer.Error.Code, helpers.CodeInvalidParams)
	}
	if !strings.Contains(answer.Error.Message, "summarize") {
		t.Errorf("the failure does not name the prompt that was asked for: %q", answer.Error.Message)
	}
}

// TestAnEmptyConversationIsWhatAPromptThatMeantItAnswers.
//
// The list is not being taken away, it is being given back its one meaning: a
// handler that returned no messages and no error said there are none, and that
// answer still arrives as a result.
func TestAnEmptyConversationIsWhatAPromptThatMeantItAnswers(t *testing.T) {
	who := security.Subject{ID: "u1", Tenant: "t1"}

	silent := helpers.Conversations().Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"silent"}}`))

	messages, answer := promptMessages(t, silent)
	if answer.Error != nil {
		t.Fatalf("a prompt that meant to answer nothing was refused: %s", silent)
	}
	if len(messages) != 0 {
		t.Errorf("a prompt that answers nothing carried %d messages", len(messages))
	}

	// And one that has something to say still carries it.
	rendered := helpers.Conversations().Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"prompts/get",`+
			`"params":{"name":"summarise","arguments":{"slug":"a-post"}}}`))
	if messages, answer := promptMessages(t, rendered); answer.Error != nil || len(messages) != 1 {
		t.Errorf("a prompt that renders was not answered with its messages: %s", rendered)
	}
}

// TestAPromptIsNotRenderedFromArgumentsItDidNotDeclare.
//
// The tool path refuses a call whose arguments do not match the schema, which
// is what lets a tool read an argument without checking whether it arrived. A
// prompt declares its arguments in the same message the client reads, and
// nothing checked them: a required one that never came rendered as the empty
// string, and one the model invented was handed straight through. The rendered
// messages are what a model acts on next, so a prompt built from arguments
// nobody sent is a conversation started about the wrong thing -- and it looks
// like a result, because it is one.
func TestAPromptIsNotRenderedFromArgumentsItDidNotDeclare(t *testing.T) {
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, bad := range []struct {
		about     string
		arguments string
		says      string
	}{
		{"a required argument that was not sent", `{}`, "slug"},
		{"a required argument left out of a call that sent another", `{"style":"short"}`, "slug"},
		{"an argument nobody declared", `{"slug":"a-post","style":"short"}`, "style"},
	} {
		answer := helpers.Conversations().Handle(context.Background(), who,
			[]byte(`{"jsonrpc":"2.0","id":1,"method":"prompts/get",`+
				`"params":{"name":"summarise","arguments":`+bad.arguments+`}}`))

		var out helpers.AnswerShape
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Errorf("%s was answered with something that is not a response: %v", bad.about, err)
			continue
		}
		if out.Error == nil {
			t.Errorf("%s was rendered: %s", bad.about, answer)
			continue
		}
		if out.Error.Code != helpers.CodeInvalidParams {
			t.Errorf("%s was answered with %d, want %d", bad.about, out.Error.Code, helpers.CodeInvalidParams)
		}
		if !strings.Contains(out.Error.Message, bad.says) {
			t.Errorf("%s was refused without naming the argument: %q", bad.about, out.Error.Message)
		}
	}

	// A prompt declaring no arguments refuses one all the same: the check is
	// about the declaration and not about there being one to compare against.
	answer := helpers.Conversations().Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"prompts/get",`+
			`"params":{"name":"silent","arguments":{"slug":"a-post"}}}`))
	if _, out := promptMessages(t, answer); out.Error == nil {
		t.Errorf("a prompt declaring no arguments was sent one and rendered anyway: %s", answer)
	}

	// And a call that matches the declaration still renders from what it sent.
	rendered := helpers.Conversations().Handle(context.Background(), who,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"prompts/get",`+
			`"params":{"name":"summarise","arguments":{"slug":"a-post"}}}`))
	if !strings.Contains(string(rendered), "Summarise a-post") {
		t.Errorf("a call carrying exactly what the prompt declared was not rendered: %s", rendered)
	}
}

// TestAnOptionalPromptArgumentIsStillOptional, so "required" stays a statement
// about one argument rather than a way to refuse every call.
func TestAnOptionalPromptArgumentIsStillOptional(t *testing.T) {
	rendered := helpers.Everything().Handle(context.Background(), security.Subject{ID: "u1", Tenant: "t1"},
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"prompts/get",`+
			`"params":{"name":"summarise","arguments":{"slug":"a-post"}}}`))

	if messages, out := promptMessages(t, rendered); out.Error != nil || len(messages) != 1 {
		t.Errorf("a call carrying the required argument and no optional one was refused: %s", rendered)
	}
}

// TestAPromptThatFailedIsStillSilentWhenItWasANotification.
//
// A failure is still an answer, and a notification gets none. Turning the new
// failures into the one message a sender that is not listening receives is how
// this change would break a client that was working.
func TestAPromptThatFailedIsStillSilentWhenItWasANotification(t *testing.T) {
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, name := range []string{"unrenderable", "summarize", "silent"} {
		if got := helpers.Conversations().Handle(context.Background(), who,
			[]byte(`{"jsonrpc":"2.0","method":"prompts/get","params":{"name":"`+name+`"}}`)); got != nil {
			t.Errorf("a notification asking for %s was answered with %s", name, got)
		}
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

// TestTheCodeSaysWhichHalfOfTheMessageIsWrong.
//
// -32600 and -32602 are read by different people. The first says the envelope
// is not a request this server can carry, and whoever reads it looks at the
// client that built the message; the second says the request arrived and its
// parameters could not be used, and whoever reads it looks at the call. Sending
// -32600 for both leaves every parameter mistake looking like a broken client,
// which is the one place nobody finds a wrong argument.
//
// -32603 is neither: it is a call that was read, accepted and then failed
// inside, and retrying it is the only sensible response.
func TestTheCodeSaysWhichHalfOfTheMessageIsWrong(t *testing.T) {
	who := security.Subject{ID: "u1", Tenant: "t1"}

	for _, message := range []struct {
		about string
		body  string
		code  int
	}{
		{"an id nothing can be keyed on", `{"jsonrpc":"2.0","id":[1],"method":"ping"}`, helpers.CodeInvalidRequest},
		{"another protocol", `{"jsonrpc":"1.0","id":1,"method":"ping"}`, helpers.CodeInvalidRequest},
		{"no method at all", `{"jsonrpc":"2.0","id":1,"result":{}}`, helpers.CodeInvalidRequest},
		{
			"a notification carrying an id",
			`{"jsonrpc":"2.0","id":1,"method":"notifications/initialized"}`,
			helpers.CodeInvalidRequest,
		},
		{"a method nobody implements", `{"jsonrpc":"2.0","id":1,"method":"nope"}`, helpers.CodeMethodNotFound},
		{"params that are not a structured value", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":7}`, helpers.CodeInvalidRequest},
		{
			"params sent by position",
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":["list_posts"]}`,
			helpers.CodeInvalidParams,
		},
		{
			"arguments that are not an object",
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_posts","arguments":7}}`,
			helpers.CodeInvalidParams,
		},
		{
			"a prompt name nobody declared",
			`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"nothing"}}`,
			helpers.CodeInvalidParams,
		},
		{
			"a prompt argument that was not declared",
			`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"summarise","arguments":{"x":"y"}}}`,
			helpers.CodeInvalidParams,
		},
		{
			"a prompt that refused to render",
			`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"unrenderable"}}`,
			helpers.CodeInternal,
		},
	} {
		answer := helpers.Conversations().Handle(context.Background(), who, []byte(message.body))

		var out helpers.AnswerShape
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Errorf("%s was answered with something that is not a response: %v", message.about, err)
			continue
		}
		if out.Error == nil {
			t.Errorf("%s was answered with a result: %s", message.about, answer)
			continue
		}
		if out.Error.Code != message.code {
			t.Errorf("%s was answered with %d, want %d", message.about, out.Error.Code, message.code)
		}
	}

	// Every one of them sent without an id is answered by nothing at all: a
	// code is still an answer, and the sender said it is not listening.
	for _, body := range []string{
		`{"jsonrpc":"2.0","method":"tools/call","params":7}`,
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"list_posts","arguments":7}}`,
		`{"jsonrpc":"2.0","method":"prompts/get","params":{"name":"nothing"}}`,
		`{"jsonrpc":"2.0","method":"prompts/get","params":{"name":"summarise","arguments":{"x":"y"}}}`,
		`{"jsonrpc":"2.0","method":"nope"}`,
	} {
		if got := helpers.Conversations().Handle(context.Background(), who, []byte(body)); got != nil {
			t.Errorf("%s was answered with %s", body, got)
		}
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
