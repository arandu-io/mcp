One thing to know before declaring a `MimeType` other than text: `resources/read`
answers `"mimeType": "text/plain"` unconditionally at `protocol.go:362`, while
`resources/list` reports what the resource declares. A resource that says it is
JSON is listed as JSON and read back as plain text. Changing that is a
`mcp-protocol` job.
