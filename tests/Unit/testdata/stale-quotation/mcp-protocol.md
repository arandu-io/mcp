## One inconsistency already in the file

`resources/read` writes `"mimeType": "text/plain"` unconditionally at
`protocol.go:362`, while `resources/list` reports `mimeOr(r.MimeType())` at
`protocol.go:352`. A resource declaring `application/json` is therefore listed
as JSON and read back as plain text, and `Resource.MimeType`'s own doc comment
says it "is what the content is". Fixing it means calling `mimeOr` on the
resource in the read branch too — which needs the `Resource` in hand rather than
just the URI, so `Server.Read` returns only a `Response` today. It is a small
change with a signature in it, so propose it before writing it.
