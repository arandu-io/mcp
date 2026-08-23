# Working in this repository

This is mcp, the Model Context Protocol server for Arandu. It exposes an
application to an assistant through the policies the application already has.

It is a library and nothing else — there is no `cmd/` and no binary. An
application declares a `Server`, fills three slices with what it wants
reachable, and mounts it on one of two transports.

**It ships zero tools, and that is the design rather than a gap.** `Tools`,
`Resources` and `Prompts` are empty slices, and nothing in this module
implements the `Tool` interface:

```sh
grep -rn 'func .*Name() string' *.go   # no output, exit 1
```

A tool here would be a tool with an opinion about somebody else's domain. The
only thing this module knows how to do with a tool is call it as the subject
that asked.

Read `.agents/skills/` before writing code. Each skill is a procedure, and the
one you need is named by the situation you are in.

## The four gates

Nothing is finished until all four exit zero.

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./...
go vet ./...
go test -race ./...
```

Both filters on `gofmt` are the project's rather than this repository's, and
neither matches anything here today — `find . -name '*.kyse.go'` and
`find . -path '*/testdata/*' -name '*.go'` are both empty. The command is kept
identical across repositories so that the one a contributor copies is the one
CI runs, and so that the first file of either kind does not also need CI
changed for it.

One more before a pull request:

```sh
bash tests/test-layout-guard.sh
```

It refuses a test file `go test` would not run — `ServerTest.go` compiles into
the package as ordinary code and every test inside it is skipped, with a green
build. It also refuses a test outside `tests/` that is not
`*_internal_test.go`, a capitalised package clause, and a shipping package that
reaches the tests tree. This module has no `*_internal_test.go` at all.

CI adds three it does not: one dependency, no Node, and govulncheck.

## The tree

Five files at the root, and five packages in the module.

| path | what it owns |
| --- | --- |
| `mcp.go` | the three interfaces — `Tool`, `Resource`, `Prompt` — and the values they speak in: `Request`, `Response`, `Argument`, `Message` |
| `server.go` | `Server` and the one door. `Call` is where validation, authorization and the shape of a failure are decided once |
| `protocol.go` | JSON-RPC 2.0, and the nine methods the switch answers. Knows no transport |
| `schema.go` | the typed argument builder and the validator that runs before `Handle` |
| `transport.go` | `Web`, `Local`, `Start`, `Describe`, and `MaxMessage` |

`protocol.go` is where the wire format lives because both transports speak it
and neither should have an opinion about it: stdio frames a message with a
newline, HTTP frames it with a request body, and everything between the frame
and the `Server` is one file.

## The nine methods

Count them in the switch rather than believing this list:

```sh
grep -c '^	case "' protocol.go   # 9
grep -n '^	case "' protocol.go
```

```
initialize                 protocolVersion, serverInfo, instructions, capabilities
ping                       an empty result
notifications/initialized  answered by silence
tools/list                 name, description, inputSchema
tools/call                 content and isError
resources/list             uri, name, description, mimeType
resources/read             contents
prompts/list               name, description, arguments
prompts/get                description and messages
```

Eight of the nine are requests and one is a notification. Anything else is
answered `-32601`, and a notification naming anything else is answered by
nothing at all.

## What does not exist here

Reaching for one of these is the fastest way to be wrong. None of them is
missing by accident.

| A model reaches for | What is here instead |
| --- | --- |
| a struct tag or an attribute declaring a tool | four methods on an interface. Go has no attributes, so this is more characters and one less mechanism |
| a registry a tool registers itself into at boot | three slices on `Server`. A tool that exists and is not reachable is visible in one file |
| a subject, a tenant or a role read out of the message | the `subject` parameter of `Handle`, put there by the transport. Nothing in `protocol.go` reads either from JSON |
| a schema written as a JSON string | `mcp.Object(mcp.String(...), mcp.Int(...))`, checked before `Handle` runs |
| a tool that queries the database | a tool that calls a service with `r.Subject()`, through the policy, like a controller |
| an empty result for a refused authorization | `mcp.Error`, with `isError` set. A model handed an empty list concludes there is nothing there |
| a third transport with its own validation | `Server.Call`, which both go through |
| sampling, roots, completion, subscriptions | nothing. A capability declared and not served is one a client reports as the server being broken |
| a second dependency | one direct require, and it is the framework. Three indirect come with it, and CI fails a fourth |

## The one rule everything else follows from

**The subject comes from the transport, never from the message.**

`Server.Handle` takes a `security.Subject` as a parameter. `parse` reads four
members — `jsonrpc`, `id`, `method`, `params` — and no member of any message
reaches a subject, a tenant or a role. A client that could name its own subject
is a client that could name anybody's.

Where the transport gets it from is the difference between the two:

```
Web    from the session, exactly like every other request the application
       answers. No session is security.Guest(tenant), and what a guest may do
       is the policy's answer.
Local  from the argument, because there is no session on a pipe. The identity
       is declared where the server is registered, so an application letting an
       assistant act as an administrator has written that down where a reviewer
       reads it.
```

Everything downstream is the framework's ordinary path: the subject goes to the
service, the service asks the policy, the policy issues the `Grant`. There is no
second enforcement point in this module and no way to write one by accident.

## Two claims in the doc comments that the tooling does not back

Both are worth knowing before you repeat them.

**`aru mcp:start` and `aru mcp:list` do not exist.** `transport.go:85` says
`Local` "is `aru mcp:start`" and `transport.go:175` says `Describe` is "for
`aru mcp:list`"; the CLI has neither. `aru help` lists its commands and none of
them is under `mcp:`. Until one is, `mcp.Start` is called by the application
and `mcp.Describe` writes to whatever `io.Writer` it is handed.

**`Server.Validate` runs on one of the two transports.** `Local` calls it and
refuses to serve when it fails; `Web` does not call it at all. So "a tool with
no description does not boot" is true over stdio and not true over HTTP. Call
it yourself at boot if the server is only ever mounted on a route.

## Writing code

Comments, identifiers, error messages, log lines and test names are in English.
A test name is a sentence about what the code does:
`TestTheToolIsCalledAsTheSubjectThatAsked`,
`TestAMemberNobodyNamedIsCarriedRatherThanRefused`.

A doc comment documents its symbol and nothing beyond it — what the function
does, what it takes, what it returns, what it guarantees, and why a signature is
the shape it is, said in terms of the code.

A test goes under `tests/` in the directory that says what kind it is: `Unit/`
for one unit with no protocol and no transport, `Feature/` for a behaviour
crossing layers, `Fuzz/` for the two targets and their corpora, `Helpers/` for
the fakes the suite drives. The directory names the category, so the file name
does not repeat it. `CONTRIBUTING.md` has the table and the sign-off
requirement.
