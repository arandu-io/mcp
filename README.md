<p align="center">
  <img src=".github/logo.png" alt="Arandu" width="140" height="140">
</p>

<h1 align="center">arandu-io/mcp</h1>

<p align="center">Expose an Arandu application to an AI client, through the policies it already has.</p>

<p align="center">
<a href="https://github.com/arandu-io/mcp/actions/workflows/ci.yml"><img src="https://github.com/arandu-io/mcp/actions/workflows/ci.yml/badge.svg" alt="Build Status"></a>
<a href="https://pkg.go.dev/github.com/arandu-io/mcp"><img src="https://pkg.go.dev/badge/github.com/arandu-io/mcp.svg" alt="Go Reference"></a>
<a href="https://github.com/arandu-io/mcp/tags"><img src="https://img.shields.io/github/v/tag/arandu-io/mcp?label=version" alt="Latest Version"></a>
<a href="LICENSE.md"><img src="https://img.shields.io/github/license/arandu-io/mcp" alt="License"></a>
</p>

## What this is

The Model Context Protocol is how an assistant reaches a program: the program
declares tools it can call, resources it can read and prompts it can use, and
the client picks. This package is the Arandu side of that, speaking protocol
revision `2024-11-05`, over HTTP or over stdio.

## Install

```sh
go get github.com/arandu-io/mcp
```

## A tool

```go
type ListPosts struct{ svc *services.PostService }

func (ListPosts) Name() string        { return "list_posts" }
func (ListPosts) Description() string { return "Lists the posts of this blog, newest first." }

func (ListPosts) Schema() mcp.Schema {
	return mcp.Object(
		mcp.String("status", "Which posts to list").Enum("published", "draft"),
		mcp.Int("limit", "How many to return"),
	)
}

func (t ListPosts) Handle(ctx context.Context, r mcp.Request) (mcp.Response, error) {
	limit, _ := r.Int("limit")

	// The subject the request carried, through the service, through the policy.
	found, err := t.svc.List(ctx, r.Subject(), data.Query{Limit: limit})
	if err != nil {
		return mcp.Response{}, err
	}
	return mcp.JSON(found), nil
}
```

## A server, and where it is reachable

```go
// routes/ai.go
server := &mcp.Server{
	Name:         "blog",
	Version:      "1.0.0",
	Instructions: "The posts and comments of this blog. Drafts are not public.",
	Tools:        []mcp.Tool{ListPosts{svc}, PublishPost{svc}},
}

// Over HTTP, for a remote client. The subject comes from the session.
r.Action("POST", "/mcp", mcp.Web(server, sessions, cfg.Auth.Tenant)).Name("mcp")

// Over stdio, for an assistant on this machine. `aru mcp:start`.
mcp.Start(ctx, server, assistant)
```

---

## The part that is not a port

The shape above is the one this ecosystem's users already know, deliberately.
One thing is different, and it is the reason this package exists rather than a
generic Go MCP library.

**A tool reaches data, and every path to data in Arandu carries a
`security.Grant`.** The `Subject` is on the `Request`, and a tool has no other
way to call a service. A policy that refuses a tool refuses it for the same
reason it refuses a controller — there is no second enforcement point, and no
way to write one by accident.

That is not tidiness. An MCP server hands a language model the keys to an
application. The version of this package where a tool queries the database
directly would be the largest hole this project could ship, and it would ship
quietly, because the answers would look right.

**Where the subject comes from is the transport's answer, and the two are
different on purpose:**

| | |
|---|---|
| `mcp.Web` | from the session, exactly like an HTTP request. No session is a guest, and what a guest may do is the policy's answer |
| `mcp.Local` | from configuration, over a pipe. There is no session on stdio, so the identity is **declared** where the server is registered and is visible in `routes/ai.go` |

The local one takes a `Subject` rather than defaulting to one, so an application
that lets an assistant act as an administrator has written that down somewhere a
reviewer reads.

## Three smaller decisions

**A refusal is an error, not an empty result.** A model handed an empty list
concludes there is nothing there and tells somebody. A model told it may not,
stops. It is one boolean and it is the difference between "you have no invoices"
and "you cannot see them".

**An argument nobody declared is refused.** A model that invents a parameter and
is not told keeps inventing it — and a tool that reads only what it declared
acts on a call it half understood.

**A tool with no description does not boot.** The description is what the model
reads to decide whether to call it: it is the highest-leverage string in this
package, and a tool without one is called at random. It is a mistake in a
declaration, so it belongs at boot rather than at the first call.

## What is deliberately absent

- **No attributes**, because Go has none. A tool's name and description are
  methods: more characters, one less mechanism.
- **No facade**, and no dynamic registration. A server declares its tools,
  resources and prompts in three slices, so one that exists and is not reachable
  is visible in one file.
- **No sampling and no roots.** They are in the protocol and nothing in an
  Arandu application needs them yet; a capability declared and not served is one
  a client reports as the server being broken.

## Learning Arandu

The API reference is generated from the doc comments and lives on
[pkg.go.dev](https://pkg.go.dev/github.com/arandu-io/mcp). Every exported
symbol carries one, and that is deliberate: it is the documentation that cannot
drift from the code, because it sits in the same file.

The CLI documents itself. `aru help` lists every command, and each one explains
what it writes and what to do with it. `aru doctor` explains what it found and
what breaks, not which rule was violated.

A guide and a website do not exist yet, and that is a decision rather than a
gap: a guide written against an API that still moves is work done twice, and the
second time is worse — there is wrong documentation published. The site is the
next phase, and it will be an Arandu application.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security Vulnerabilities

Please review [our security policy](SECURITY.md) on how to report a
vulnerability. Never open a public issue for one.

## License

Open-sourced software licensed under the [MIT license](LICENSE.md).
