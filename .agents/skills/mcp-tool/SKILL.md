---
name: mcp-tool
description: Write or change what an assistant may call in an Arandu application — a tool, a resource or a prompt declared on an mcp.Server. Use when the request is to "expose this to an assistant", "add an MCP tool", "let the AI list orders", "give the model access to", "write a tool", "declare the arguments", "add a resource", "add a prompt", or when a Tool, Schema, Request, Response, mcp.Object or mcp.JSON is involved. Also use when tempted to query the database from a tool, to read the tenant out of an argument, or to answer a refused authorization with an empty list — the first two have no correct form here and the third is what makes a model report there is nothing there. Covers the four methods, the closed set of argument types, r.Subject(), and why the description is the highest-leverage string in the file.
license: MIT
---

# Writing a tool

A tool is four methods and no registration. It is declared in a slice on the
`Server`, so a tool that exists and is not reachable is visible in one file.

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

`go doc Tool` prints the interface with the reason for each method beside it.

## The procedure

**1. Write `Handle` as a call to a service, never as a query.** A tool reaches
data, and every path to data in this framework carries an authorization
decision. `r.Subject()` is the only identity a tool has and there is no way to
call a service without one. A tool that opens a database handle is the largest
hole this project could ship, and it would ship quietly, because the answers
would look right.

**2. Return the error rather than swallowing it.** `Server.Call` turns a
non-nil error into `mcp.Error`, which sets `isError` on the wire. That boolean
is the difference between "you have no invoices" and "you cannot see them" — a
model handed an empty list concludes there is nothing there and tells somebody;
a model told it may not, stops.
`TestARefusalIsAnErrorAndNotAnEmptyResult` is where that is pinned.

**3. Spend real effort on `Description`.** It is what the model reads to decide
whether to call this rather than something else, so a model that called the
wrong tool was told the wrong thing here. Say what it lists, in what order, and
what it will not show. `Server.Validate` refuses a tool without one —
`TestAServerWithoutDescriptionsIsRefusedAtBoot`.

**4. Declare every argument.** One the schema does not name is refused before
`Handle` runs, and the refusal names it: a model that invents a parameter and is
not told keeps inventing it — `TestAnUndeclaredArgumentIsRefused`.

**5. Run the gates.**

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./... && go vet ./... && go test -race ./...
```

## The name, and what a client does with it

Lower case with underscores. That is what every client displays without quoting,
and it is what the model types back. `Server.Call` looks it up by exact string;
a miss answers with the sorted list of what does exist, because a model retrying
the same wrong name is a model that was told nothing useful —
`TestAnUnknownToolListsTheOnesThatExist`.

Two tools with one name is refused at boot rather than resolved by order —
`TestTwoToolsWithOneNameAreRefused`.

## The schema, and the two edges it has

Three kinds of argument, and nothing else:

| builder | JSON type | what `Validate` accepts |
| --- | --- | --- |
| `mcp.String(name, description)` | `string` | a string, and one of the enum if there is one |
| `mcp.Int(name, description)` | `integer` | any JSON number |
| `mcp.Bool(name, description)` | `boolean` | `true` or `false` |

`.Required()` on any of them; `.Enum(...)` to close a set. Reach for the enum:
a model given a closed list picks from it, and a model given "the status"
invents one.

Every problem in a call is reported at once rather than one per attempt —
`TestAWrongTypeAndAWrongEnumAreBothReported`. What is required is in the
schema the client is shown, so the model knows before it calls —
`TestTheModelIsToldWhichArgumentsAreRequired`,
`TestARequiredArgumentThatWasNotSentIsRefused`.

Two edges, both measured rather than read:

**`.Enum` on an `Int` is advertised and never enforced.** The schema goes out
carrying it and `Validate` accepts anything numeric — `problem` compares against
the enum only for a string field. Measured: a schema of
`mcp.Int("limit", "how many").Enum("1", "2")` renders
`map[description:how many enum:[1 2] type:integer]`, and
`Validate(map[string]any{"limit": 99})` returns `<nil>`. If the set matters,
declare it as a `String` and convert in `Handle`.

**`integer` means "a number".** `1.5` passes validation, and `r.Int` truncates
it to `1` without saying so. Measured through `Server.Call` with
`{"limit": 1.5}`: no error, and the tool read `1`. Check the bound in `Handle`
when a fractional value would be wrong.

## Reading the arguments

`r.String`, `r.Int` and `r.Bool` each return the value and whether it was there
at all. Use the second return rather than indexing `r.Arguments`: a missing key
is a zero value, and a tool that cannot tell `0` from absent is a tool acting on
an argument nobody passed. Validation has already run, so the only reason for a
`false` is that the argument was optional and omitted.

## Answering

`mcp.Text(format, args...)` for prose, `mcp.Error(format, args...)` for a
failure the model should react to, `mcp.JSON(v)` for structure. `JSON` encodes
here rather than in the tool so that every tool answers the same shape and a
marshalling failure is one error message instead of one per tool — and it
answers rather than panics on a value that cannot be encoded
(`TestAValueThatCannotBeEncodedIsAnAnswerAndNotAPanic`).

## Resources and prompts

A `Resource` is something the client reads by URI: `URI`, `Name`,
`Description`, `MimeType` and `Read(ctx, subject)`. It takes the subject
directly, so the same rule applies — it goes to the service, not to a query.
Two resources at one URI are refused at boot.

One thing to know before declaring a `MimeType` other than text: `resources/read`
answers `"mimeType": "text/plain"` unconditionally at `protocol.go:362`, while
`resources/list` reports what the resource declares. A resource that says it is
JSON is listed as JSON and read back as plain text. Changing that is a
`mcp-protocol` job.

A `Prompt` is a conversation the application knows how to start: `Arguments()`
declares what the client fills in, and `Render` builds `[]mcp.Message` with
`mcp.User` and `mcp.Assistant`. The roles are the two strings the protocol
carries and nothing else — `TestBothRolesAreSpeltTheWayTheProtocolCarriesThem`.
A `Render` that returns an error answers with no messages and the error text as
the description, so the client sees why rather than an empty prompt.

## What has no correct form here

- **A tool that reads a tenant, a role or a user id out of its arguments.** The
  subject is on the `Request` and it came from the transport. An argument
  carrying an identity is the client naming its own permissions.
- **A tool constructed at run time from configuration.** The slices are read as
  they are; there is no registry and nothing appends at boot.
- **A tool in this module.** Nothing here implements `Tool`, deliberately —
  `grep -rn 'func .*Name() string' *.go` prints nothing. A tool belongs to the
  application whose domain it is about.
