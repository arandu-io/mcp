---
name: mcp-transport
description: Mount an mcp.Server so something can reach it — over HTTP for a remote client, or over stdio for an assistant on the same machine. Use when the request mentions "mount the MCP server", "the route for MCP", "expose the server", "stdio server", "connect a desktop assistant", "my MCP server is not reachable", "the client hangs", "nothing comes back", "the client says the server crashed", "aru mcp:start", "where does the subject come from", "session", "guest subject", "MaxMessage", or "message too large". Covers mcp.Web and mcp.Local, why the subject is an argument on one and the session on the other, the 202 a notification gets, the one-megabyte bound both share, why Validate runs on only one of them, and why a single log line on stdout breaks a working server.
license: MIT
---

# Mounting the server

There are two transports and one difference between them: where the subject
comes from. Everything else — validation, authorization, the shape of a failure
— is decided once in `Server.Call`, which both go through, so a third transport
could not arrive with its own idea of any of it.

```
Web    a remote client, over HTTP, identified by the session it carries.
Local  a process on the same machine, over a pipe, identified by the Subject
       the application passed in.
```

## Over HTTP

```go
// routes/ai.go
server := &mcp.Server{
	Name:         "blog",
	Version:      "1.0.0",
	Instructions: "The posts and comments of this blog. Drafts are not public.",
	Tools:        []mcp.Tool{ListPosts{svc}, PublishPost{svc}},
}

r.Action("POST", "/mcp", mcp.Web(server, sessions, cfg.Auth.Tenant)).Name("mcp")
```

`mcp.Web(s, sessions, tenant)` returns `func(*fhttp.Context) error`, which is
what `Router.Action` takes. It reads the body, loads the subject with
`sessions.Load`, and hands both to `Server.Handle`. A request whose session
cannot be loaded becomes `security.Guest(tenant)` — not a refusal, because what
a guest may do is the policy's answer and it is the same answer a browser would
get.

The subject comes from the session and never from the body. A client that could
name its own subject is a client that could name anybody's, and nothing in
`protocol.go` reads a subject, a tenant or a role out of a message.

Three answers it gives that are not a result:

| situation | answer |
| --- | --- |
| the body could not be read | `400` |
| the body is over `MaxMessage` | `413`, and the message is refused whole rather than truncated |
| the message was a notification | `202`, at `transport.go:74`, so a client can tell "nothing to say" from "an empty answer" |

Anything else is `200` with `Content-Type: application/json` and the encoded
answer.

**Mount it behind whatever middleware the application already uses to establish
a session.** `mcp.Web` authenticates nobody; it reads what is there.

## Over stdio

```go
// The identity the assistant acts as, declared where a reviewer reads it.
assistant := security.Subject{ID: "assistant", Tenant: cfg.Auth.Tenant, Roles: []string{"reader"}}

if err := mcp.Start(ctx, server, assistant); err != nil {
	return err
}
```

`mcp.Start` is `mcp.Local` over the process's own stdin and stdout;
`mcp.Local(ctx, s, subject, in, out)` takes the two streams, which is what the
tests drive.

**This is the one to be careful with, and it is careful on purpose.** There is
no session on a pipe, so the identity is a parameter rather than a default. An
application that wants an assistant to act as an administrator has written that
down in a file somebody reviews, instead of inheriting it.

One message per line, one answer per line. A blank line carries nothing and is
answered by nothing — answering it would turn a byte into an answer, and a
stream of newlines into as much output as the other end cares to ask for
(`TestABlankLineIsNotAMessage`).

**Nothing may be written to stdout except an answer.** A log line there is a
parse error at the client, and it is the most common way a working stdio server
appears broken. `mcp.Start` logs through the framework's logger, which writes to
stderr. If you add output of your own, it goes to stderr too.

## The bound both share

`mcp.MaxMessage` is `1 << 20`, and it is the same number on both transports: a
message one accepts and the other refuses is a message whose fate depends on how
it arrived, which is the hardest kind of report to act on.

A message is held whole before it can be parsed, so without a bound the process
holds whatever the other end sends — and sending is the cheap half of that
exchange. On HTTP the reader takes one byte past the limit, so "too large" can
be told from "exactly large enough" rather than reported as "the message is not
JSON". On the pipe, an over-long line is dropped and the rest of it is discarded
through the reader's own buffer, so the stream is left at the start of the next
message: a reader that gave up mid-line would read the remains of one message as
many.

`tests/Feature/transport_test.go` holds six tests and they are the whole of what
guards this — `grep -c '^func Test' tests/Feature/transport_test.go` says 6.
Read them before changing any of it:
`TestALineIsNotReadIntoUnboundedMemory`, `TestABlankLineIsNotAMessage`,
`TestAnOversizedMessageIsRefusedAndTheStreamResyncs`,
`TestABodyOverTheLimitIsRefusedRatherThanTruncated`,
`TestAMessageWithinTheLimitStillArrives` and
`TestALargeMessageOverAPipeStillArrives`.

## Validate runs on one of the two

`Local` calls `Server.Validate` before it serves anything and returns the error
instead of starting. `Web` does not call it at all — `grep -n 'Validate()' *.go`
shows the one call site, `transport.go:94`.

So "a tool with no description does not boot" is true over stdio and false over
HTTP. Measured: a server carrying a tool with an empty description returns a
non-nil `Validate` error, and `Server.Call` on that same server runs the tool
and answers `isError=false`.

If the server is only ever mounted on a route, call `Validate` yourself where
the application boots and fail there. Everything it reports is a mistake in a
declaration — a tool with no name, two tools with one name, a tool with no
description, two resources at one URI — and a server that starts and answers
nonsense is worse than one that refuses to start.

## `aru mcp:start` does not exist

`transport.go:85` says `Local` "is `aru mcp:start`" and `transport.go:175` says
`Describe` is "for `aru mcp:list`". The CLI has neither: `aru help` lists its
commands and none is under `mcp:`. Until one is:

- stdio is started by the application calling `mcp.Start`, from a console
  command of its own or from `main`;
- `mcp.Describe(s, out)` writes the name, the version, the instructions, then
  the tools with their properties, and the resources and prompts if there are
  any — to any `io.Writer`. `TestDescribeNamesEverythingAServerOffers` is what
  holds its shape.

Do not put either command in an example, a README or a comment.

## When nothing comes back

Work through it in this order; each step rules out one layer.

1. **A notification gets no answer, and that is correct.** A message with no
   `id` is answered by silence over stdio and by `202` over HTTP. If the client
   is waiting, the client sent no id.
2. **Something else is on stdout.** A print, a panic trace, a dependency's
   logger. One line is enough to make every answer unparseable.
3. **The message is over a megabyte.** `413` on HTTP, and on the pipe a
   `-32600` naming no id, because the id was inside the part that was refused.
4. **The session did not load.** The tool ran as `security.Guest(tenant)` and
   the policy refused it. The answer will be an `isError` result rather than a
   protocol error, so read the content rather than the envelope.
5. **The server is empty.** `initialize` declares only the capabilities the
   server actually has, so a `Server` with three empty slices declares none and
   a client that asks for a tool list gets an empty one, correctly
   (`TestInitializeDeclaresOnlyWhatTheServerHas`).

## The gates

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./... && go vet ./... && go test -race ./...
bash tests/test-layout-guard.sh
```
