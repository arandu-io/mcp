---
name: mcp-tool
description: Write or change what an assistant may call in an Arandu application — a tool, a resource or a prompt declared on an mcp.Server. Use when the request is to "expose this to an assistant", "add an MCP tool", "let the AI list orders", "give the model access to", "write a tool", "declare the arguments", "add a resource", "add a prompt", or when a Tool, Schema, Request, Response, mcp.Object, mcp.JSON or a JSON Resource is involved. Also use when tempted to query the database from a tool, to read the tenant out of an argument, or to answer a refused authorization with an empty list — the first two have no correct form here and the third is what makes a model report there is nothing there. Covers the four methods, the closed set of argument types, r.Subject(), and why the description is the highest-leverage string in the file.
license: MIT
---

# Writing a tool

A tool is four methods and no registration. It is declared in a slice on the
`Server`, so a tool that exists and is not reachable is visible in one file.

```go
// app/Mcp/ListPosts.go
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
	found, err := t.svc.List(ctx, r.Subject(), database.Query{Limit: limit})
	if err != nil {
		return mcp.Response{}, err
	}
	// The fields that may leave, listed once in a JSON Resource -- the same one
	// a controller answers with.
	return mcp.JSON(resources.NewPostList(found)), nil
}
```

`go doc Tool` prints the interface with the reason for each method beside it.

## The procedure

**1. Write `Handle` as a call to a service, never as a query.** A tool reaches
data, and every path to data in this framework carries an authorization
decision. `r.Subject()` is the only identity a tool has and there is no way to
call a service without one. A tool that opens a database handle is the largest
hole this project could ship, and it would ship quietly, because the answers
would look right — and nothing in the server stops it: it runs no policy of
its own, so the service is the boundary
(`TestAToolThatAsksNoPolicyIsDispatchedAndReachesTheHandle` measures the
dispatch).

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
`TestAServerWithoutDescriptionsIsRefusedAtBoot` — but only on a server that
runs it: `Local` does before serving, and `Web` never calls it. The
`mcp-transport` skill has the measurement and what to do about it.

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

Two tools with one name is refused by `Validate` rather than resolved by order —
`TestTwoToolsWithOneNameAreRefused`.

## The schema, and the edge it has

Three kinds of argument, and nothing else:

| builder | JSON type | what `Validate` accepts |
| --- | --- | --- |
| `mcp.String(name, description)` | `string` | a string, and one of the enum if there is one |
| `mcp.Int(name, description)` | `integer` | a whole number in the range of an `int`: no fraction, no infinity, no NaN |
| `mcp.Bool(name, description)` | `boolean` | `true` or `false` |

`.Required()` on any of them; `.Enum(...)` to close a set. Reach for the enum:
a model given a closed list picks from it, and a model given "the status"
invents one.

Every problem in a call is reported at once rather than one per attempt —
`TestAWrongTypeAndAWrongEnumAreBothReported`. What is required is in the
schema the client is shown, so the model knows before it calls —
`TestTheModelIsToldWhichArgumentsAreRequired`,
`TestARequiredArgumentThatWasNotSentIsRefused`.

One edge, measured rather than read:

**`.Enum` on an `Int` is advertised and never enforced.** The schema goes out
carrying it and `Validate` accepts any whole number — `problem` compares against
the enum only for a string field. Measured: a schema of
`mcp.Int("limit", "how many").Enum("1", "2")` renders
`map[description:how many enum:[1 2] type:integer]`, and
`Validate(map[string]any{"limit": 99})` returns `<nil>`. If the set matters,
declare it as a `String` and convert in `Handle`.

**`integer` means a whole number this server can carry.** `1.9`, `0.5` and
`1e100` are refused before `Handle` runs, and `r.Int` answers `false` for them
rather than converting: 1.9 used to reach the tool as 1, and a value past the
range of an `int` as whatever the machine did with it —
`TestAnIntegerArgumentIsAWholeNumberInRange`,
`TestANumberThatIsNotAnIntegerNeverReachesTheTool`,
`TestReadingANumberThatIsNotAnIntegerReportsThatItIsNot`.

## Reading the arguments

`r.String`, `r.Int` and `r.Bool` each return the value and whether it was there
at all. Use the second return rather than indexing `r.Arguments`: a missing key
is a zero value, and a tool that cannot tell `0` from absent is a tool acting on
an argument nobody passed. Validation has already run, so the only reason for a
`false` is that the argument was optional and omitted.

## Answering

`mcp.Text(format, args...)` for prose, `mcp.Error(format, args...)` for a
failure the model should react to, and `mcp.JSON(resource)` for structure.

`mcp.JSON` takes a JSON Resource — `hhttp.JsonResource` from
`github.com/arandu-io/hesape/http`, the contract `ctx.JSON` takes — and never a
bare value. An encoder handed an entity answers with whatever fields the entity
has, including the ones added later without anybody reading the tool; a JSON
Resource answers with the fields somebody listed. A model is a reader like any
other client, and it repeats what it reads.

```go
// app/Http/Resources/PostList.go
type PostList struct{ posts []models.Post }

func NewPostList(posts []models.Post) PostList { return PostList{posts} }

func (l PostList) ToArray() map[string]any {
	items := make([]map[string]any, 0, len(l.posts))
	for _, p := range l.posts {
		items = append(items, map[string]any{"slug": p.Slug, "title": p.Title})
	}
	return map[string]any{"posts": items}
}

func (PostList) With() map[string]any { return nil }
```

The text is the document `ctx.JSON` writes for the same JSON Resource: the
fields under `data`, what `With` returns beside them, and a field whose value
reports itself missing — `resources.When(false, …)` — left out. A tool and a
controller over one service answer the same document
(`TestAStructuredAnswerIsTheDocumentAControllerAnswersWith`,
`TestAFieldThatIsMissingIsLeftOut`). `JSON` encodes here rather than in the
tool so that every tool answers the same shape and a marshalling failure is one
error message instead of one per tool — and it answers rather than panics on a
JSON Resource that cannot be encoded, or a nil one
(`TestAValueThatCannotBeEncodedIsAnAnswerAndNotAPanic`).

An MCP resource is a different thing with the same word in it: the `Resource`
interface in the next section, which a client reads by URI. The two are always
named in full.

## MCP resources and prompts

An MCP resource — the `Resource` interface — is something the client reads by
URI: `URI`, `Name`, `Description`, `MimeType` and `Read(ctx, subject)`. It
takes the subject directly, so the same rule applies — it goes to the service,
not to a query. Two MCP resources at one URI are refused by `Validate`.

An MCP resource is the same type listed and read. `resources/read` looks the
resource up by URI and answers with the type it declares, taken through the
same default the listing uses, so an empty `MimeType` is `text/plain` in both —
`TestAResourceIsTheSameTypeListedAndRead`.

A read that did not happen is not answered as the resource. A URI no resource
answers to is the JSON-RPC error `-32002`, and a `Read` that returned an error
is `-32603` with the error's text as the message — the two codes the
`2024-11-05` resources page names for a read. Either way there is no `contents`
member, so a client never caches a refusal under the URI or hands it to a
parser that trusted the declared type —
`TestAURINobodyAnswersToIsAFailureAndNamesItsOwnCode`,
`TestAReadThatDidNotHappenIsNotAnsweredAsTheResourceItself`. Return the
service's error from `Read` rather than describing it in the text: text is
answered as the resource.

A `Prompt` is a conversation the application knows how to start: `Arguments()`
declares what the client fills in, and a call is checked against it before
`Render` runs — a required argument that was not sent, or one nobody declared,
is refused with `-32602` (`TestAPromptIsNotRenderedFromArgumentsItDidNotDeclare`).
`Render` builds `[]mcp.Message` with `mcp.User` and `mcp.Assistant`. The roles
are the two strings the protocol carries and nothing else —
`TestBothRolesAreSpeltTheWayTheProtocolCarriesThem`.
A `Render` that returns an error is answered as the JSON-RPC error `-32603` with
the error text as the message, and a prompt name nobody declared as `-32602`:
neither is an empty conversation, which the model would read as there being
nothing to say. Return the service's error from `Render` for the same reason a
tool returns it from `Handle`.

## What has no correct form here

- **A tool that reads a tenant, a role or a user id out of its arguments.** The
  subject is on the `Request` and it came from the transport. An argument
  carrying an identity is the client naming its own permissions.
- **A tool constructed at run time from configuration.** The slices are read as
  they are; there is no registry and nothing appends at boot.
- **A tool in this module.** Nothing here implements `Tool`, deliberately —
  `grep -rn 'func .*Name() string' *.go` prints nothing, and
  `TestThisPackageShipsNoTool` fails on a type that grows the method set of a
  tool, an MCP resource or a prompt. A tool belongs to the application whose
  domain it is about.
