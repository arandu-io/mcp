package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/arandu-io/framework/security"
)

// The wire format: JSON-RPC 2.0, which is what the protocol carries.
//
// It is in one file because both transports speak it and neither should have an
// opinion about it. stdio frames it with newlines and HTTP frames it with a
// request body, and everything between the frame and the Server is here.

// rpcRequest is one message, already taken apart. It carries no field tags
// because nothing decodes into it: parse reads the members by name.
type rpcRequest struct {
	JSONRPC string
	ID      json.RawMessage
	Method  string
	Params  json.RawMessage
}

// The id is not omitted when it is empty, unlike the request's. A client keys
// the calls it is waiting on by id, and an answer without one is an answer it
// has nowhere to put: the protocol asks for the member to be there and null when
// the request's could not be read, and a nil RawMessage encodes as exactly that.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// The codes the protocol defines. Only these three are ever sent: everything
// else an application can go wrong with is a Response with IsError set, which
// the model reads, rather than a transport error, which it does not.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
)

// content is one piece of a result, in the shape the protocol carries.
type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// parse takes a message apart, and reports whether it was a JSON object at all.
//
// The members are read by name from a map rather than decoded into the struct,
// and the difference is case: encoding/json fills a field from a member whose
// name differs only in case, and the protocol's names do not differ only in
// case. A server that reads "METHOD" as "method" answers messages that nothing
// else along the path recognises as calls -- which is how the thing in front of
// it records one call and the thing behind it runs another. Reading by name also
// tells a member that is absent from one that is null, and that difference is
// the whole of what makes a message a notification.
func parse(body []byte) (rpcRequest, bool) {
	fields := members(body)
	if fields == nil {
		return rpcRequest{}, false
	}

	return rpcRequest{
		JSONRPC: text(fields, "jsonrpc"),
		ID:      fields["id"],
		Method:  text(fields, "method"),
		Params:  fields["params"],
	}, true
}

// members reads a JSON object, and answers nil for anything that is not one.
func members(raw json.RawMessage) map[string]json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	return fields
}

// text reads one member as a string. A member that is absent, null or of any
// other type reads as empty, which every caller here treats as absent.
func text(fields map[string]json.RawMessage, name string) string {
	var s string
	_ = json.Unmarshal(fields[name], &s)
	return s
}

// isID reports whether a member can serve as a request id, which the protocol
// allows to be a string, a number or null and nothing else.
//
// An absent member is allowed and is not a bad id: a message without one is a
// notification, which is a different thing from a request that named an id this
// server cannot carry.
//
// The number is read as a json.Number rather than into a float, because one
// larger than a float holds is still a number. Reading it into a float fails,
// and refusing it would answer a well-formed message with a complaint about the
// one member that was fine.
func isID(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return true
	}
	var n json.Number
	return json.Unmarshal(raw, &n) == nil
}

// isNotification reports whether a method name belongs to a message the sender
// is not waiting for an answer to.
//
// The protocol names every notification it defines under one prefix and no
// request under it, so the prefix is the test. A list of the nine would be a
// second place to add the tenth to, and the copy that was not updated is a
// server that answers a message nobody is listening for.
func isNotification(method string) bool {
	return strings.HasPrefix(method, "notifications/")
}

// failure builds an answer that carries no result, for the given id or for none.
func failure(id json.RawMessage, code int, message string) []byte {
	return encode(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{code, message}})
}

// tooLong is the answer to a message longer than a transport reads.
//
// It names no id because the id is somewhere inside the part that was refused,
// and reading far enough to find it is the thing being refused.
func tooLong() []byte {
	return failure(nil, codeInvalidRequest, "the message is longer than this server reads")
}

// Handle answers one JSON-RPC message.
//
// It returns nil for a notification -- a message with no id, which the protocol
// says gets no answer. Sending one anyway is what makes a client hang up.
func (s *Server) Handle(ctx context.Context, subject security.Subject, body []byte) []byte {
	req, ok := parse(body)
	if !ok {
		// Bytes that are not JSON and JSON that is not an object are different
		// mistakes, and which one it was decides where the person reading the
		// answer looks: at the encoder, or at what it put in the message.
		if json.Valid(body) {
			return failure(nil, codeInvalidRequest, "the message is JSON but not a request")
		}
		return failure(nil, codeParse, "the message is not JSON")
	}
	if !isID(req.ID) {
		// Before anything else reads it, because every answer below is built
		// around this member and one that cannot be carried has to be refused
		// while there is still an answer to refuse it with.
		//
		// The id is what a client matches an answer to a call by, so echoing a
		// shape the protocol does not carry hands it back something its own
		// matching has nowhere to key on. The answer names no id, which is what
		// the protocol asks for when the request's could not be read.
		return failure(nil, codeInvalidRequest, "the id must be a string, a number or null")
	}
	if req.JSONRPC != "2.0" {
		return failure(req.ID, codeInvalidRequest, "this server speaks JSON-RPC 2.0")
	}
	if req.Method == "" {
		// A message with no method is not a request at all, and the usual one is
		// an answer that arrived where a call was expected. Saying so beats
		// reporting that a method named by the empty string is not implemented.
		return failure(req.ID, codeInvalidRequest, "the message names no method")
	}
	if isNotification(req.Method) && len(req.ID) != 0 {
		// A notification carries no id -- that is what makes it one -- so a
		// message that names a notification and carries an id is neither: not a
		// notification, because of the id, and not a request, because no method
		// under that prefix answers one.
		//
		// It is refused rather than answered because answering is the failure
		// that stays invisible until it is not: a client that was never waiting
		// receives an answer, and what it does with one it cannot match to a
		// call is its own decision -- the strict ones close the connection.
		return failure(req.ID, codeInvalidRequest,
			req.Method+" is a notification, and a notification carries no id")
	}

	answer := func(result any) []byte {
		if len(req.ID) == 0 {
			return nil // a notification
		}
		return encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
	}

	switch req.Method {
	case "initialize":
		return answer(map[string]any{
			"protocolVersion": Version,
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
			"instructions":    s.Instructions,
			// Only what this server has. Declaring a capability it cannot serve
			// is how a client asks once and gets an error it reports as the
			// server being broken.
			"capabilities": s.capabilities(),
		})

	case "ping":
		return answer(map[string]any{})

	case "notifications/initialized":
		// Reached without an id, because one that carried it was refused above.
		// There is nothing to do and nothing to answer.
		return nil

	case "tools/list":
		tools := make([]map[string]any, 0, len(s.Tools))
		for _, t := range s.Tools {
			tools = append(tools, map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"inputSchema": t.Schema().JSON(),
			})
		}
		return answer(map[string]any{"tools": tools})

	case "tools/call":
		p := members(req.Params)
		var arguments map[string]any
		_ = json.Unmarshal(p["arguments"], &arguments)

		out := s.Call(ctx, subject, text(p, "name"), arguments)
		return answer(map[string]any{
			"content": []content{{Type: "text", Text: out.Text}},
			"isError": out.IsError,
		})

	case "resources/list":
		list := make([]map[string]any, 0, len(s.Resources))
		for _, r := range s.Resources {
			list = append(list, map[string]any{
				"uri": r.URI(), "name": r.Name(),
				"description": r.Description(), "mimeType": mimeOr(r.MimeType()),
			})
		}
		return answer(map[string]any{"resources": list})

	case "resources/read":
		uri := text(members(req.Params), "uri")

		out := s.Read(ctx, subject, uri)
		return answer(map[string]any{
			"contents": []map[string]any{{"uri": uri, "mimeType": "text/plain", "text": out.Text}},
		})

	case "prompts/list":
		list := make([]map[string]any, 0, len(s.Prompts))
		for _, pr := range s.Prompts {
			args := make([]map[string]any, 0, len(pr.Arguments()))
			for _, a := range pr.Arguments() {
				args = append(args, map[string]any{
					"name": a.Name, "description": a.Description, "required": a.Required,
				})
			}
			list = append(list, map[string]any{
				"name": pr.Name(), "description": pr.Description(), "arguments": args,
			})
		}
		return answer(map[string]any{"prompts": list})

	case "prompts/get":
		p := members(req.Params)
		var arguments map[string]any
		_ = json.Unmarshal(p["arguments"], &arguments)
		name := text(p, "name")

		for _, pr := range s.Prompts {
			if pr.Name() != name {
				continue
			}
			messages, err := pr.Render(ctx, Request{Arguments: arguments, subject: subject})
			if err != nil {
				return answer(map[string]any{"messages": []any{}, "description": err.Error()})
			}
			out := make([]map[string]any, 0, len(messages))
			for _, m := range messages {
				out = append(out, map[string]any{
					"role": m.Role, "content": content{Type: "text", Text: m.Text},
				})
			}
			return answer(map[string]any{"description": pr.Description(), "messages": out})
		}
		return answer(map[string]any{"messages": []any{}, "description": "no prompt called " + name})

	default:
		if len(req.ID) == 0 {
			return nil
		}
		return failure(req.ID, codeMethodNotFound, "this server does not implement "+req.Method)
	}
}

// capabilities is what this server actually has.
func (s *Server) capabilities() map[string]any {
	out := map[string]any{}
	if len(s.Tools) > 0 {
		out["tools"] = map[string]any{}
	}
	if len(s.Resources) > 0 {
		out["resources"] = map[string]any{}
	}
	if len(s.Prompts) > 0 {
		out["prompts"] = map[string]any{}
	}
	return out
}

func mimeOr(m string) string {
	if m == "" {
		return "text/plain"
	}
	return m
}

func encode(r rpcResponse) []byte {
	body, err := json.Marshal(r)
	if err != nil {
		// Marshalling a response of our own shape cannot fail for a reason the
		// caller can act on, and returning nothing would hang the client.
		return []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"encoding the answer failed"}}`)
	}
	return body
}
