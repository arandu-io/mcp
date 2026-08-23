---
name: mcp-protocol
description: Change protocol.go — this module's JSON-RPC 2.0 layer and the nine methods it answers. Use when adding or altering a protocol method, when a client rejects an answer or hangs up, and when the request mentions "JSON-RPC", "notification", "request id", "-32700", "-32600", "-32601", "initialize", "capabilities", "tools/list", "tools/call", "resources/read", "prompts/get", "params must be an object", "protocol version", "2024-11-05", or "the fuzz corpus". Also use when tempted to decode a message straight into a struct, to answer a notification, to refuse a member nobody named, or to trust anything a message carries about who is asking — each has already been the wrong answer here and the reason is written next to the code. Covers parse, isID, shapeOfParams, the two fuzz targets and the crasher corpus.
license: MIT
---

# Changing the wire format

`protocol.go` is JSON-RPC 2.0 and nothing else. It knows no transport: stdio
frames a message with a newline and HTTP frames it with a request body, and
everything between the frame and the `Server` is this one file. Both transports
call `Server.Handle`, so a rule written once here applies to both.

## The nine methods

Count them in the switch rather than believing a list:

```sh
grep -c '^	case "' protocol.go   # 9
grep -n '^	case "' protocol.go
```

Eight requests and one notification, in the order the switch answers them:

| method | result |
| --- | --- |
| `initialize` | `protocolVersion`, `serverInfo`, `instructions`, `capabilities` |
| `ping` | an empty object |
| `notifications/initialized` | nothing at all |
| `tools/list` | `tools`: `name`, `description`, `inputSchema` |
| `tools/call` | `content` as one text part, and `isError` |
| `resources/list` | `resources`: `uri`, `name`, `description`, `mimeType` |
| `resources/read` | `contents` |
| `prompts/list` | `prompts`: `name`, `description`, `arguments` |
| `prompts/get` | `description` and `messages` |

`Version` is `"2024-11-05"` and is the single place the revision is written.

`capabilities` reports only what the server actually holds: `tools` when the
slice is non-empty, and the same for `resources` and `prompts`. A capability
declared and not served is one a client asks about once and reports as the
server being broken — `TestInitializeDeclaresOnlyWhatTheServerHas`.

## Five rules the parser already follows

Each of these is a mistake that was made or nearly made, and each has its reason
written at the code. Read them before changing how a message is taken apart.

**1. Members are read by name, not decoded into a struct.** `encoding/json`
fills a field from a member whose name differs only in case, and this protocol's
names do not differ only in case. A server that reads `"METHOD"` as `"method"`
answers messages that nothing else along the path recognises as calls — the
thing in front of it records one call and the thing behind it runs another.
`parse` reads a `map[string]json.RawMessage`, which also tells a member that is
absent from one that is null, and that difference is the whole of what makes a
message a notification. `TestAMemberIsTheOneItIsNamed`.

**2. The id is checked before anything else reads it.** A string, a number or
null, and nothing else. The number is read as a `json.Number` rather than into a
float, because one larger than a float holds is still a number and refusing it
would answer a well-formed message with a complaint about the one member that
was fine. A bad id is refused with no id, because echoing a shape the protocol
does not carry hands the client something its own matching has nowhere to key
on. `TestAnIDIsAStringANumberOrNull`, `TestAnAnswerAlwaysCarriesAnID`.

**3. A notification is answered by silence, however wrong it was.** No id means
the sender is not listening, so the only thing left to do about its mistake is
to not perform it. Sending an answer anyway is what makes a strict client hang
up. A message that names a `notifications/` method **and** carries an id is
neither one nor the other, and it is refused —
`TestANotificationIsAnsweredBySilenceHoweverWrongItIs`,
`TestANotificationThatCarriesAnIDIsRefused`, `TestANotificationIsNotAnswered`.

The `notifications/` prefix is the test rather than a list of names. A list
would be a second place to add the next one to, and the copy that was not
updated is a server that answers a message nobody is listening for.

**4. Shape is read before content, at both levels.** `shapeOfParams` looks at
the first byte instead of decoding, because decoding would have to succeed
before the shape could be told — which is the order that makes a number where an
object belongs arrive at a method as no parameters at all. `argumentsOf` asks
the same question one level in, and it matters more there: a tool whose
arguments are all optional would pass its own schema and run, and the client
would receive a result for a message this server could not read. A positional
`params` gets its own message, because nothing here declares an order to read
them in. `TestParamsThatAreNotAnObjectAreRefused`,
`TestPositionalParamsAreRefusedRatherThanIgnored`,
`TestArgumentsThatAreNotAnObjectAreRefused`,
`TestParamsThatAreAbsentAndParamsThatAreNullAreTheSame`,
`TestAPromptWhoseArgumentsCannotBeReadIsNotRendered`.

**5. A member nobody named is carried, not refused.** The protocol closes none
of its message objects, so a server that refused what it did not recognise would
refuse the revision after this one — the failure that cannot be fixed from the
side that sees it. Ignoring is not trusting: the member reaches nothing, and
`TestAMemberNobodyNamedIsCarriedRatherThanRefused` pins both halves.

## The subject is never in the message

`Handle` takes a `security.Subject` as a parameter and nothing in this file
reads an identity out of JSON:

```sh
grep -n 'subject' protocol.go
```

Four lines, and each of them passes the parameter along. If a change here ever
needs to read who is asking, the answer is that the transport already knows and
this file must not learn.

## The error codes, and which failures are not codes

Three constants, plus a fourth spelt out in bytes in `encode` because it is the
answer to marshalling having failed:

```
-32700  the message is not JSON
-32600  it is JSON but not a request the server can carry
-32601  the method is not implemented
-32603  encoding the answer failed
```

Everything an **application** can go wrong with is a `Response` with `IsError`
set instead, which the model reads, rather than a transport error, which it does
not. A refused authorization, a tool that does not exist, arguments that do not
match a schema — all of those come back as a result with `isError: true` and a
`200`.

The two nearby wrong messages each have a test: "the message is not JSON" is
reserved for bytes that really are not
(`TestOnlySomethingThatIsNotJSONIsCalledThat`), and a message carrying no method
says so rather than reporting that a method named by the empty string is
unimplemented — the usual one is an answer that arrived where a call was
expected (`TestAMessageWithNoMethodSaysSo`).

## Adding a method

1. Add the `case` to the switch in `Handle`, beside the ones it belongs with.
2. Answer through `answer(...)`, which returns nil for a notification, or
   `refuse(code, msg)`, which is silent for one. Never build an `rpcResponse` by
   hand in a branch.
3. If it takes parameters, read them with `members(req.Params)` and `text(...)`.
   The shape was already checked before the switch.
4. If it reaches application data, it goes through `Server.Call` or
   `Server.Read`, both of which take the subject. Do not reach a `Tool` or a
   `Resource` from this file directly.
5. If it is a capability a client negotiates, add it to `capabilities` at the
   same time, and only under the condition that the server actually has it.
6. Write the test as a sentence about the behaviour, under `tests/Feature/`.
7. Run the gates.

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./... && go vet ./... && go test -race ./...
bash tests/test-layout-guard.sh
```

## The fuzz targets

Two, and their names are what `go test -fuzz` takes:

```sh
grep -rhoE 'func Fuzz[A-Za-z0-9_]*' tests/Fuzz/*_test.go | sort -u   # FuzzHandle, FuzzLocal
go test ./tests/Fuzz -run='^$' -fuzz='^FuzzHandle$' -fuzztime=30s
```

`FuzzHandle` drives one message through `Server.Handle`; `FuzzLocal` drives a
stream through the stdio reader. Their seed corpora and the one committed
crasher run as ordinary subtests on every `go test`, so a regression is caught
on every push without anybody asking for it. Measured on this tree: `go test -v`
reports 88 `--- PASS` lines — 37 top-level and 51 subtests, and every one of the
51 is a fuzz input. `FuzzHandle` carries 37 seeds and the crasher; `FuzzLocal`
carries 13 seeds.

A new crasher belongs under `tests/Fuzz/testdata/fuzz/<target>/`, committed by a
person who has read it. What a long run buys over the corpus is the paths a
minute does not reach.

**Expect `FuzzHandle` to fail inside a minute, and read the failure before
believing it.** A 30-second run on a clean tree found this input:

```
{"id":2000000000000000000000000000000000000000000000000000000000000000…}
```

309 digits, and no `jsonrpc` member. The server answers `-32600` and echoes the
id byte for byte, which is correct: `isID` accepts a number that large on
purpose, reading it as a `json.Number` because one larger than a float holds is
still a number.

The harness cannot follow it there. `helpers.SameJSON` and the round-trip check
below it both `json.Unmarshal` into `any`, which decodes a number as a `float64`
and refuses this one — measured:
`json.Unmarshal([]byte("2"+strings.Repeat("0",308)), &x)` into `any` returns
`cannot unmarshal number … into Go value of type float64`, and the same bytes
into a `json.Number` return `<nil>`. So `SameJSON` answers false about two
identical ids and the target reports "the answer carries an id the request did
not send" about an answer that carried exactly the one it was sent.

The gap is in the assertion, not in `Handle`. A crasher written out for this
reason is a false positive: do not commit it — it would fail `go test` for
everyone afterwards, since the corpus runs as ordinary subtests. Fixing the
harness means comparing the two ids as bytes, or decoding with a
`json.Decoder` that has `UseNumber` set, so that what the test can carry is what
`isID` can carry.

## What must not enter this file

- **A second dependency.** `go.mod` names one direct require and it is the
  framework; the three indirect entries arrive through it. This package is imported by applications that expose themselves to
  an assistant, so a second require is a download for every one of them. The
  check, which is the pass when it prints nothing:

  ```sh
  export GOWORK=off
  go list -deps -f '{{if .Module}}{{.Module.Path}}{{end}}' ./... \
    | sort -u | grep -vE '^github\.com/arandu-io/|^golang\.org/x/'
  ```

- **A struct with JSON tags for an incoming message.** See rule 1.
- **Knowledge of a transport.** No `http`, no `bufio`, no `os` — those live in
  `transport.go`, and the reason both files exist is that neither should have an
  opinion about the other.
- **Anything read from a message that decides who is asking.** See above.

## One inconsistency already in the file

`resources/read` writes `"mimeType": "text/plain"` unconditionally at
`protocol.go:362`, while `resources/list` reports `mimeOr(r.MimeType())` at
`protocol.go:352`. A resource declaring `application/json` is therefore listed
as JSON and read back as plain text, and `Resource.MimeType`'s own doc comment
says it "is what the content is". Fixing it means calling `mimeOr` on the
resource in the read branch too — which needs the `Resource` in hand rather than
just the URI, so `Server.Read` returns only a `Response` today. It is a small
change with a signature in it, so propose it before writing it.
