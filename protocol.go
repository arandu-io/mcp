package mcp

import (
	"context"
	"encoding/json"

	"github.com/arandu-io/framework/security"
)

// The wire format: JSON-RPC 2.0, which is what the protocol carries.
//
// It is in one file because both transports speak it and neither should have an
// opinion about it. stdio frames it with newlines and HTTP frames it with a
// request body, and everything between the frame and the Server is here.

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
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

// Handle answers one JSON-RPC message.
//
// It returns nil for a notification -- a message with no id, which the protocol
// says gets no answer. Sending one anyway is what makes a client hang up.
func (s *Server) Handle(ctx context.Context, subject security.Subject, body []byte) []byte {
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{codeParse, "the message is not JSON"}})
	}
	if req.JSONRPC != "2.0" {
		return encode(rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{codeInvalidRequest, "this server speaks JSON-RPC 2.0"}})
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

	case "notifications/initialized", "ping":
		return answer(map[string]any{})

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
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)

		out := s.Call(ctx, subject, p.Name, p.Arguments)
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
		var p struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(req.Params, &p)

		out := s.Read(ctx, subject, p.URI)
		return answer(map[string]any{
			"contents": []map[string]any{{"uri": p.URI, "mimeType": "text/plain", "text": out.Text}},
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
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)

		for _, pr := range s.Prompts {
			if pr.Name() != p.Name {
				continue
			}
			messages, err := pr.Render(ctx, Request{Arguments: p.Arguments, subject: subject})
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
		return answer(map[string]any{"messages": []any{}, "description": "no prompt called " + p.Name})

	default:
		if len(req.ID) == 0 {
			return nil
		}
		return encode(rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{codeMethodNotFound, "this server does not implement " + req.Method}})
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
