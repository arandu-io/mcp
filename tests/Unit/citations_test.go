package unit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A citation in the documentation has to resolve.
//
// Two documents warned about a phrase in a doc comment and quoted the line it
// was on. The phrase had been removed, the line had moved on, and the warning
// stayed -- so a reader followed a precise citation to a line that says
// something else, and either hunted for text that no longer exists or
// "corrected" a comment that was already right.
//
// The guard that was meant to catch this read two files, and both were the ones
// already clean. It passed for as long as the false claim lived in the two it
// did not read, which were the two an assistant is told to read first.
//
// So the rule is not "do not name that command" -- naming it to say it does not
// exist is useful. The rule is that a citation points at what it says it does.

// citation matches a reference to a line of a Go file: transport.go:85.
var citation = regexp.MustCompile(`([A-Za-z0-9_./-]+\.go):(\d+)`)

// quoted matches a phrase in double quotes, which is what a citation carries
// when it is asserting what the line says.
var quoted = regexp.MustCompile(`"([^"\n]{4,})"`)

// span matches a code span, and literal a string literal inside one.
var (
	span    = regexp.MustCompile("`([^`\n]+)`")
	literal = regexp.MustCompile(`"[^"]*"`)
)

// commentMarker matches what opens a line of a Go comment.
var commentMarker = regexp.MustCompile(`(?m)^[ \t]*//[ \t]?`)

// listItem matches what opens a Markdown list item.
var listItem = regexp.MustCompile(`^([-*+]|\d+\.)\s`)

// TestEveryCitationPointsAtAFileThatHasThatLine keeps a reference from
// surviving a rename or a file getting shorter.
func TestEveryCitationPointsAtAFileThatHasThatLine(t *testing.T) {
	root := moduleRoot(t)

	checked := 0
	for _, doc := range documents(t, root) {
		body := read(t, doc)

		for _, found := range citation.FindAllStringSubmatch(body, -1) {
			named, line := found[1], found[2]

			path := filepath.Join(root, named)
			source, err := os.ReadFile(path)
			if err != nil {
				// A bare filename is resolved against the module root, which is
				// where this module keeps its Go. A name that does not resolve
				// there is a name that does not resolve.
				t.Errorf("%s cites %s, and there is no such file", rel(root, doc), named)
				continue
			}

			checked++
			if lines := strings.Count(string(source), "\n") + 1; atoi(line) > lines {
				t.Errorf("%s cites %s:%s, and that file has %d lines", rel(root, doc), named, line, lines)
			}
		}
	}

	if checked == 0 {
		t.Error("no citation was checked, so this test cannot fail")
	}
}

// TestEveryQuotedCitationQuotesWhatIsThere is the half that matters.
//
// A sentence that cites a file and quotes something is asserting what the file
// says. The assertion is checkable, and the one that was wrong here would have
// failed.
func TestEveryQuotedCitationQuotesWhatIsThere(t *testing.T) {
	root := moduleRoot(t)
	source := func(name string) (string, bool) {
		body, err := os.ReadFile(filepath.Join(root, name))
		return string(body), err == nil
	}

	checked := 0
	for _, doc := range documents(t, root) {
		for _, claim := range sentences(read(t, doc)) {
			quotes, problems := misquotes(claim, source)
			checked += quotes
			for _, problem := range problems {
				t.Errorf("%s %s", rel(root, doc), problem)
			}
		}
	}

	if checked == 0 {
		t.Error("no quotation beside a citation was checked, so this test cannot fail")
	}
}

// TestAStaleQuotationIsCaughtWholeAndAcrossALineBreak pins the two ways the
// check above let one stale warning through twice.
//
// Both skills said resources/read answered `"mimeType": "text/plain"` at
// protocol.go:362. The code then took the type from the resource: the literal
// left the line and both of its words stayed in the file, so a check reading the
// quotation word by word went on passing. The second skill also put the
// quotation on the line above its citation, where a check reading line by line
// never compared the two.
//
// The documents under testdata are the passages the skills carried, byte for
// byte. The sources are the read branch before and after the change.
func TestAStaleQuotationIsCaughtWholeAndAcrossALineBreak(t *testing.T) {
	const before = `
	case "resources/read":
		uri := text(members(req.Params), "uri")

		out := s.Read(ctx, subject, uri)
		return answer(map[string]any{
			"contents": []map[string]any{{"uri": uri, "mimeType": "text/plain", "text": out.Text}},
		})
`
	const after = `
	case "resources/read":
		uri := text(members(req.Params), "uri")

		// The type the resource declares, read through the same default the
		// listing uses.
		mime := "text/plain"
		for _, r := range s.Resources {
			if r.URI() == uri {
				mime = mimeOr(r.MimeType())
				break
			}
		}

		out := s.Read(ctx, subject, uri)
		return answer(map[string]any{
			"contents": []map[string]any{{"uri": uri, "mimeType": mime, "text": out.Text}},
		})
`

	for _, c := range []struct {
		doc, code string
		stale     bool
	}{
		{"mcp-tool.md", before, false},
		{"mcp-tool.md", after, true},
		{"mcp-protocol.md", before, false},
		{"mcp-protocol.md", after, true},
	} {
		source := func(name string) (string, bool) { return c.code, name == "protocol.go" }

		var problems []string
		for _, claim := range sentences(read(t, filepath.Join("testdata", "stale-quotation", c.doc))) {
			_, found := misquotes(claim, source)
			problems = append(problems, found...)
		}

		switch {
		case c.stale && len(problems) != 1:
			t.Errorf("%s against the code after the change: want the one stale quotation reported, got %q", c.doc, problems)
		case c.stale && !strings.Contains(problems[0], `"mimeType": "text/plain"`):
			t.Errorf("%s against the code after the change: want the literal reported whole, got %q", c.doc, problems[0])
		case !c.stale && len(problems) != 0:
			t.Errorf("%s against the code it described: want nothing reported, got %q", c.doc, problems)
		}
	}
}

// TestOnlyWhatASentenceQuotesBesideACitationIsChecked fixes the edges of the
// check, each of which would otherwise be a report about a document that is
// right.
func TestOnlyWhatASentenceQuotesBesideACitationIsChecked(t *testing.T) {
	const code = "// so a client can tell \"nothing to\n// say\" from \"an empty answer\".\nreturn ctx.Status(http.StatusAccepted)\n"
	source := func(name string) (string, bool) { return code, name == "transport.go" }

	for _, c := range []struct {
		name, doc string
		quotes    int
		problems  int
	}{
		{"a phrase that wraps in the document and in the comment", "At `transport.go:3` a client can tell \"nothing to\nsay\" apart.", 1, 0},
		{"a phrase the cited file does not hold", "At `transport.go:3` it says \"something else entirely\".", 1, 1},
		{"a literal the cited file does not hold", "It answers `\"status\": 202` at `transport.go:3`.", 1, 1},
		{"a code span that names rather than quotes", "It answers `202` with `application/json` at `transport.go:3`.", 0, 0},
		{"a code span that is a command with one quote mark", "Run `grep -c '^\tcase \"' protocol.go` against `transport.go:3`.", 0, 0},
		{"a quotation in a sentence that cites nothing", "It says \"something else entirely\".", 0, 0},
		{"a citation does not vouch for the next sentence", "See `transport.go:3`. The next one says \"something else entirely\".", 0, 0},
		{"a table row is one claim", "| a notification | `202`, at `transport.go:3`, so \"nothing to say\" |", 1, 0},
		{"a fenced line is read alone", "```\nx := \"something else entirely\" // transport.go:3\n```", 1, 1},
	} {
		quotes, problems := 0, 0
		for _, claim := range sentences(c.doc) {
			q, p := misquotes(claim, source)
			quotes += q
			problems += len(p)
		}
		if quotes != c.quotes || problems != c.problems {
			t.Errorf("%s: %d quotations and %d problems, want %d and %d", c.name, quotes, problems, c.quotes, c.problems)
		}
	}
}

// sentences splits a Markdown document into the claims a citation can vouch
// for.
//
// The unit is the sentence rather than the line, because prose wraps: a warning
// that puts its quotation on one line and its citation on the next is one
// claim, and reading line by line never compares the two. A heading and a table
// row are a claim each, a list item starts one, a blank line ends one, and a
// fenced block is read a line at a time, since a line of code is not a
// sentence.
func sentences(doc string) []string {
	var out, paragraph []string
	flush := func() {
		if len(paragraph) > 0 {
			out = append(out, split(strings.Join(paragraph, " "))...)
			paragraph = nil
		}
	}

	fenced := false
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			flush()
			fenced = !fenced
		case fenced:
			out = append(out, line)
		case trimmed == "":
			flush()
		case strings.HasPrefix(trimmed, "|"), strings.HasPrefix(trimmed, "#"):
			flush()
			out = append(out, split(trimmed)...)
		case listItem.MatchString(trimmed):
			flush()
			paragraph = append(paragraph, trimmed)
		default:
			paragraph = append(paragraph, trimmed)
		}
	}
	flush()
	return out
}

// split cuts a paragraph after each full stop, question mark or exclamation
// mark that a space follows, outside a code span. Inside one a full stop is
// part of a file name or a call, and cutting there would part a citation from
// its own line number.
func split(paragraph string) []string {
	var out []string
	start, code := 0, false
	for i := 0; i < len(paragraph); i++ {
		switch c := paragraph[i]; {
		case c == '`':
			code = !code
		case code:
		case c == '.' || c == '?' || c == '!':
			end := i + 1
			for end < len(paragraph) && strings.IndexByte(`"')*`, paragraph[end]) >= 0 {
				end++
			}
			if end == len(paragraph) || paragraph[end] == ' ' {
				out = append(out, strings.TrimSpace(paragraph[start:end]))
				start, i = end, end-1
			}
		}
	}
	if rest := strings.TrimSpace(paragraph[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

// misquotes answers how many quotations a sentence carries beside a citation,
// and which of them none of the files it cites contains.
func misquotes(claim string, source func(name string) (string, bool)) (int, []string) {
	var names, bodies []string
	seen := map[string]bool{}
	for _, found := range citation.FindAllStringSubmatch(claim, -1) {
		name := found[1]
		if seen[name] {
			continue
		}
		seen[name] = true

		body, ok := source(name)
		if !ok {
			continue // TestEveryCitationPointsAtAFileThatHasThatLine reports it
		}
		names = append(names, name)
		bodies = append(bodies, flat(body))
	}
	if len(bodies) == 0 {
		return 0, nil
	}

	quotes := quotations(claim)
	var problems []string
	for _, quote := range quotes {
		want, found := flat(quote), false
		for _, body := range bodies {
			if strings.Contains(body, want) {
				found = true
				break
			}
		}
		if !found {
			// Between backticks rather than %q, which would escape the quote
			// marks of a literal and print something nobody can search for.
			problems = append(problems, fmt.Sprintf("says %s contains `%s`, and it does not",
				strings.Join(names, " or "), quote))
		}
	}
	return len(quotes), problems
}

// quotations answers what a sentence quotes.
//
// A code span carrying a string literal is a quotation of code, and it is read
// whole. `"mimeType": "text/plain"` asserts that literal; read as the two words
// "mimeType" and "text/plain" it passes for as long as each of them is
// somewhere in the file, which is how a warning outlived the code it described.
// A code span with no literal in it names something rather than quoting it, and
// is left alone. What remains is prose, where a phrase in double quotes is the
// quotation.
func quotations(claim string) []string {
	var out []string
	for _, found := range span.FindAllStringSubmatch(claim, -1) {
		if literal.MatchString(found[1]) {
			out = append(out, found[1])
		}
	}

	// Each span becomes a lone backtick, so a quote mark inside code cannot
	// pair with one in the prose around it, and a phrase that runs through a
	// span is recognised as markup rather than as a quotation.
	prose := span.ReplaceAllString(claim, "`")
	for _, found := range quoted.FindAllStringSubmatch(prose, -1) {
		text := strings.TrimSpace(found[1])
		if text == "" || strings.Contains(text, "`") {
			continue
		}
		out = append(out, text)
	}
	return out
}

// flat reads text the way a sentence reads: the marker that opens each line of
// a Go comment dropped, and every run of white space one space. A phrase that
// wraps -- in the document, or in the comment it quotes -- is then still the
// phrase.
func flat(text string) string {
	return strings.Join(strings.Fields(commentMarker.ReplaceAllString(text, "")), " ")
}

// documents answers every Markdown file of this module.
func documents(t *testing.T, root string) []string {
	t.Helper()

	var found []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "vendor" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) < 3 {
		t.Fatalf("only %d documents were found, so this test guards almost nothing", len(found))
	}
	return found
}

func read(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func rel(root, path string) string {
	if out, err := filepath.Rel(root, path); err == nil {
		return out
	}
	return path
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}
