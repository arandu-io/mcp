---
name: mcp-transport
description: Mount an mcp.Server so something can reach it — over HTTP for a remote client, or over stdio for an assistant on the same machine. Use when the request mentions "mount the MCP server", "the route for MCP", "expose the server", "stdio server", "connect a desktop assistant", "my MCP server is not reachable", "the client hangs", "nothing comes back", "the client says the server crashed", "how do I start it from the CLI", "where does the subject come from", "session", "bearer token", "401", "guest subject", "MaxMessage", or "message too large". Covers mcp.Web and mcp.Local, why the subject is an argument on one and what the guard in front of the route put on the request on the other, the 202 a notification gets, the one-megabyte bound both share, why Validate runs on only one of them, and why a single log line on stdout breaks a working server.
license: MIT
---

# Mounting the server

There are two transports and one difference between them: where the subject
comes from. Everything else — validation, authorization, the shape of a failure
— is decided once in `Server.Call`, which both go through, so a third transport
could not arrive with its own idea of any of it.

```
Web    a remote client, over HTTP, identified by the subject the guard in
       front of the route put on the request.
Local  a process on the same machine, over a pipe, identified by the Subject
       the application passed in.
```

## Over HTTP

The tools live in `app/Mcp/`, the server is composed in `bootstrap/app.go`, and
the route is in `routes/web.go` with every other route — there is no second
route file for an assistant.

```go
// bootstrap/app.go
server := &mcp.Server{
	Name:         "blog",
	Version:      "1.0.0",
	Instructions: "The posts and comments of this blog. Drafts are not public.",
	Tools:        []mcp.Tool{ListPosts{svc}, PublishPost{svc}},
}

// routes/web.go -- a client holding an API token.
r.Action("POST", "/mcp", mcp.Web(server), middleware.RequireToken(tokens)).Name("mcp")
```

`mcp.Web(s)` returns `func(*hhttp.Context) error`, which is what
`Router.Action` takes. It reads the subject with `ctx.User()` — the one the
guard in front of the route put on the request, exactly as a controller reads
it — then the body, and hands both to `Server.Handle`. It loads no session and
resolves no token, so it takes neither a store nor a resolver: which guard is
in front is the application's choice, made where the route is.

| guard in front | who the tool acts as |
| --- | --- |
| `middleware.RequireToken(tokens)` | the subject the application's `TokenResolver` answers for the bearer token, tenant included |
| `middleware.RequireAuth(sessions)` | the subject of the session, on a route `CSRFProtect` also guards |
| nothing, or `LoadSubject` with no session | nobody: the request is refused with `401` |

A request that reaches `Web` with no subject is refused before its body is
read, with `WWW-Authenticate: Bearer`, and never served as a guest. The refusal
is returned as an error, so the router answers it the way it answers every
status an action returns: a problem document (`application/problem+json`) to a
client that asked for JSON, which every MCP client does, and the status and its
sentence to anything else —
`TestARequestNobodyVouchedForIsRefusedAndNotServedAsAGuest`,
`TestAPublicRouteWithNoSessionIsRefusedRatherThanServed`. An application that
does have an anonymous caller declares it in middleware of its own that puts
`auth.Guest(tenant)` on the request; the transport never invents one.

The subject comes from the request and never from the body. A client that could
name its own subject is a client that could name anybody's, and nothing in
`protocol.go` reads a subject, a tenant or a role out of a message —
`TestASubjectInTheBodyIsNotWhoIsAsking`.

Four answers it gives that are not a result:

| situation | answer |
| --- | --- |
| no subject on the request | `401`, a problem document for a JSON client |
| the body could not be read | `400` |
| the body is over `MaxMessage` | `413`, and the message is refused whole rather than truncated |
| the message was a notification | `202`, at `transport.go:87`, so a client can tell "nothing to say" from "an empty answer" |

Anything else is `200` with `Content-Type: application/json` and the encoded
answer.

**Behind `RequireAuth`, the route stays behind `CSRFProtect`.** The session is
a cookie, which a browser attaches to a request any other site can make it
send, and a cookie-authenticated `/mcp` that a tool can change data through is
exactly the request CSRF protection exists for. It is never exempted, not even
"just for the assistant": a client that signs in with the session sends that
session's token in the `X-CSRF-Token` header, as an HTMX request does. A client
that cannot hold one uses a token instead: `RequireToken` reads only the
`Authorization` header, never a cookie, so CSRF has nothing there to reach.

## Over stdio

```go
// The identity the assistant acts as, declared where a reviewer reads it.
assistant := auth.Subject{ID: "assistant", Tenant: cfg.Auth.Tenant, Roles: []string{"reader"}}

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
appears broken.

`mcp.Start` logs to `os.Stderr`, through a logger of its own, and serves with
that logger in the context in place of the one it was handed, so a tool and the
service under it that log through `hlog.For(ctx)` write to stderr as well. It
has to: the framework's root logger, `hlog.New`, writes to stdout, and the
context an application hands `Start` usually carries it. Measured before the
change, with `hlog.Into(ctx, hlog.New("production", slog.LevelInfo))`: the line
"mcp: serving over stdio" went to stdout as a JSON object ahead of the first
answer, and a tool's log line went beside its own answer — two messages a
client cannot parse. `TestNothingButAnswersReachesStdout` serves the process's
real stdin and stdout with that logger on the context and finds only answers on
stdout and both lines on stderr.

What `Start` cannot reach is a package-level logger: a line written with
`slog.Info` after `slog.SetDefault` pointed the default at stdout still lands
there. `mcp.Local` takes the two streams and the context as they are, and logs
nothing itself; when the log has to go somewhere other than stderr, that is
the one to call.

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

Six tests in `tests/Feature/transport_test.go` are the whole of what guards
the bound. Read them before changing any of it:
`TestALineIsNotReadIntoUnboundedMemory`, `TestABlankLineIsNotAMessage`,
`TestAnOversizedMessageIsRefusedAndTheStreamResyncs`,
`TestABodyOverTheLimitIsRefusedRatherThanTruncated`,
`TestAMessageWithinTheLimitStillArrives` and
`TestALargeMessageOverAPipeStillArrives`.

## Cancelling a stdio serve

A stdio server spends its life blocked in a read with nothing coming, so asking
whether to stop between messages is asking at the one moment it never is.
`Local` closes its input when the context is cancelled, if the input is an
`io.Closer` — `os.Stdin` is, and so is a pipe — which is the only way to reach
a blocked read, and it returns the cancellation rather than the closed file it
did to itself. A peer that has hung up, crashed or gone quiet never sends the
EOF the loop would otherwise wait for. An input that is not a Closer still ends
at the next message boundary. The goroutine that does the closing ends with the
serve however the serve ends, and a stream that simply ends returns nil —
`TestCancellingTheContextEndsAServeBlockedInARead`,
`TestACancelledServeLeavesNothingRunning`,
`TestAStreamThatEndsIsNotACancellation`.

## Validate runs on one of the two

`Local` calls `Server.Validate` before it serves anything and returns the error
instead of starting. `Web` does not call it at all — `grep -n 'Validate()' *.go`
prints the declaration, `server.go:110`, and the one call site,
`transport.go:129`.

So "a tool with no description does not boot" is true over stdio and false over
HTTP. Measured: a server carrying a tool with an empty description returns a
non-nil `Validate` error, and `Server.Call` on that same server runs the tool
and answers `isError=false`.

If the server is only ever mounted on a route, call `Validate` yourself where
the application boots and fail there. Everything it reports is a mistake in a
declaration — a server with no name, a tool with no name, two tools with one
name, a tool with no description, a prompt with no name, two prompts with one
name, a prompt argument with no name or two with one, a resource with no URI,
two resources at one URI — and a server that starts and answers nonsense is
worse than one that refuses to start.

## No CLI command starts or describes an application's server

The doc comments named two once, under an `mcp:` namespace, and no longer do;
the CLI never had either. So:

- stdio is started by the application calling `mcp.Start`, from a console
  command of its own or from `main`;
- `mcp.Describe(s, out)` writes the name, the version, the instructions, then
  the tools with their properties, and the resources and prompts if there are
  any — to any `io.Writer`. `TestDescribeNamesEverythingAServerOffers` is what
  holds its shape.

Do not put either command in an example, a README or a comment —
`TestNoDocumentPromisesACommandThatStartsOrDescribesAServer` reads every
document and Go file here for the namespace. `aru mcp` on its own is the
server the CLI is to give a developer's own assistant, for the CLI's tools; it
is not a way to start yours, and aru v0.61.0 does not ship it.

## When nothing comes back

Work through it in this order; each step rules out one layer.

1. **A notification gets no answer, and that is correct.** A message with no
   `id` is answered by silence over stdio and by `202` over HTTP. If the client
   is waiting, the client sent no id.
2. **Something else is on stdout.** A print; a logger that is not the
   context's — the default after `slog.SetDefault` pointed it at stdout, or a
   dependency's own; or, under `mcp.Local`, the root logger on the context it
   was handed (see above). One line is enough to make every answer
   unparseable. A panic trace is not one of these: the runtime writes it to
   stderr, so a server that died leaves stdout empty rather than corrupt.
3. **The message is over a megabyte.** `413` on HTTP, and on the pipe a
   `-32600` naming no id, because the id was inside the part that was refused.
4. **No subject reached the route.** Over HTTP that is a `401` before the
   protocol is reached at all: no guard was mounted in front of `mcp.Web`, or
   it was `LoadSubject` and the request had no session. When the subject did
   arrive and the policy refused it, the answer is an `isError` result rather
   than a protocol error, so read the content rather than the envelope.
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
