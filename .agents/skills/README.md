# Skills

Procedures an assistant follows when working with this server.

They live in `.agents/skills/<name>/SKILL.md`, which is the path the coding
assistants read from — Cursor, Codex, Cline, Copilot, Gemini CLI, Amp, OpenCode,
Warp, Zed and the rest all look there. It is one directory rather than a file
per vendor, so a skill written once is read by whatever this project is being
written with.

Each file opens with frontmatter carrying a `name` and a `description`. The
description is what a tool reads to decide whether the skill is relevant, so it
names the situation you are in rather than the topic it covers.

There are two audiences here, and they want different things. Someone exposing
an application writes tools and mounts a transport; someone changing the server
writes the wire format underneath both. The skills are split along that line.

| skill | when it fires |
| --- | --- |
| `mcp-tool` | writing a tool, a resource or a prompt — what a model may call, what it takes, and who it acts as |
| `mcp-transport` | mounting the server: over HTTP for a remote client, over stdio for one on the same machine |
| `mcp-protocol` | changing `protocol.go` — the nine methods, ids, notifications, params, and what a message may not be trusted to carry |

## Why these exist

An MCP server is a shape a model already has an answer for, and the answer is a
generic library where a tool is a closure, a schema is a JSON string, and the
caller's identity is whatever the message said it was. This module is none of
those, and the last one is the reason it exists at all: a tool reaches data, and
every path to data in this framework carries an authorization decision.

The rest of the answer is that this repository is built to be checked rather
than trusted. The gates are four commands, the layout guard is a fifth, and CI
adds the three a laptop is worst at remembering — a second dependency, a Node
file, a known vulnerability. An assistant that runs those is not guessing.

`AGENTS.md` at the root has the tree, the nine methods and the table of what a
model reaches for that is not here. It also records two claims the doc comments
make that the tooling does not back, which is the kind of thing worth reading
before repeating.

## Adding your own

A skill in this directory travels with the repository. Keep it a procedure
rather than a description: a file that says "read the documentation" never
changes what anybody does. Every command in one has to run, and every number in
one has to have been measured.
