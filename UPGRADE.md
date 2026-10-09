# Upgrade guide

What changed in a way that stops your code compiling, and what to write instead.

Additions are not listed. A new symbol breaks nothing, and a file that listed
every one of them would be a changelog nobody reads to find the two lines that
matter. What a client receives is listed too, when it changes: an answer is
part of the contract, and a client that matched on the old one breaks as surely
as code that called the old signature.

## Before v1.0.0

While the version starts with `v0.`, the API can break. That is what `v0.` means
in Go and it is deliberate — the alternative is freezing a shape before anyone
has built on it. What is not deliberate is breaking it quietly, which is what
this file exists to stop.

---

## Unreleased — the HTTP transport serves the subject the guard put on the request, and a tool answers structure through a JSON Resource

This release requires `framework` v0.51.0 and `hesape` v0.50.1, and
`arandu.mod.toml` asks for framework `>= 0.51`. `apidiff` reports two
incompatible changes, `Web` and `JSON`, and nothing else in the package.

### `mcp.Web` takes only the server

```go
// before
r.Action("POST", "/mcp", mcp.Web(server, sessions, cfg.Auth.Tenant)).Name("mcp")

// after: the guard in front of the route puts the subject on the request
r.Action("POST", "/mcp", mcp.Web(server), middleware.RequireToken(tokens)).Name("mcp")
// or, for a client holding a session, on a route CSRFProtect guards
r.Action("POST", "/mcp", mcp.Web(server), middleware.RequireAuth(sessions)).Name("mcp")
```

`Web` reads the subject with `ctx.User()`, exactly as a controller does, and no
longer loads a session. Its parameter is `*hesape/http.Context`, which is the
type `framework/http.Context` already aliased, so `Router.Action` takes it as
before.

What to check:

- **A route with no guard in front of `Web` now answers 401.** It used to serve
  every request as `security.Guest(tenant)`. The refusal carries
  `WWW-Authenticate: Bearer` and is a problem document for a client that asked
  for JSON. Mount `RequireToken` or `RequireAuth` in front of it.
- **`LoadSubject` in front of `Web` answers 401 to a request with no session**,
  where it used to serve a guest. An application that does want an anonymous
  caller puts `auth.Guest(tenant)` on the request in middleware of its own.
- **The route moves to `routes/web.go`**, and the server is composed in
  `bootstrap/app.go`. There is no `routes/ai.go`.

### `mcp.JSON` takes a JSON Resource

```go
// before
return mcp.JSON(found), nil

// after: the fields that may leave, listed once, as for a controller
return mcp.JSON(resources.NewPostList(found)), nil
```

`JSON` takes `hesape/http.JsonResource` — `ToArray` and `With`, the contract
`ctx.JSON` takes — instead of `any`. The text is the document `ctx.JSON` writes
for the same JSON Resource: the fields under `data`, `With` beside them, and a
field whose value reports itself missing left out. A tool that passed an entity,
a map or a slice needs a JSON Resource for it; a model that read the old text
reads the fields one level down, under `data`.

### What a client receives

None of these changes a signature. Each changes an answer.

- **`resources/read` that did not happen is a JSON-RPC error.** A URI no
  resource answers to is `-32002`; a `Read` that returned an error is `-32603`
  with the error's text as the message. Neither is `contents` any more, under
  the declared type or any other.
- **`prompts/get` that did not render is a JSON-RPC error.** A prompt nobody
  declared is `-32602`; a `Render` that returned an error is `-32603`. A
  required argument that was not sent, or one the prompt did not declare, is
  `-32602` and `Render` is not called.
- **Params sent by position, and `arguments` that are not an object, are
  `-32602`**, where they were `-32600`. A `params` that is a number, a string or
  a boolean stays `-32600`.
- **`initialize` without a `protocolVersion` is `-32602`**, as are `capabilities`
  or `clientInfo` that are not objects. The answer to a good one is unchanged,
  and always names `2024-11-05`.
- **A request id of `null` is refused** with `-32600` and no id. Strings and
  numbers are carried as before.
- **An integer argument is a whole number in the range of an `int`.** A
  fraction, an infinity or a value past that range is refused before `Handle`
  runs, and `Request.Int` answers `false` for it instead of converting.
- **`Server.Validate` refuses a prompt with no name, two prompts with one name,
  and a prompt argument with no name or two with one.** `Local` calls it, so a
  stdio server declaring one of those no longer starts.

### What the process does

- **`Start` logs to stderr**, and serves with a stderr logger in the context in
  place of the one it was handed, so a tool logging through its context writes
  to stderr too. stdout carries only answers. `Local` is unchanged.
- **Cancelling the context ends `Local` from inside a blocked read**, by closing
  its input when the input is an `io.Closer` — `os.Stdin` is one. It used to
  wait for the next message or an EOF.
