package unit

import (
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
// A citation with a phrase beside it is asserting what the file says. The
// assertion is checkable, and the one that was wrong here would have failed.
func TestEveryQuotedCitationQuotesWhatIsThere(t *testing.T) {
	root := moduleRoot(t)

	for _, doc := range documents(t, root) {
		for _, line := range strings.Split(read(t, doc), "\n") {
			named := citation.FindStringSubmatch(line)
			if named == nil {
				continue
			}

			source, err := os.ReadFile(filepath.Join(root, named[1]))
			if err != nil {
				continue // the test above reports this
			}

			for _, phrase := range quoted.FindAllStringSubmatch(line, -1) {
				text := strings.Trim(phrase[1], "` ")
				if text == "" || strings.Contains(text, "`") {
					// A phrase that is itself mostly markup is a description
					// rather than a quotation.
					continue
				}
				if !strings.Contains(string(source), text) {
					t.Errorf("%s says %s contains %q, and it does not", rel(root, doc), named[1], text)
				}
			}
		}
	}
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
