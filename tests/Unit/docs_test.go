package unit

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"

	helpers "github.com/arandu-io/mcp/tests/Helpers"
)

// namespacedCommand matches a CLI command under an mcp: namespace -- the
// aru mcp:start and aru mcp:list the doc comments once promised, and that no
// version of the CLI ever registered.
//
// It does not match aru mcp on its own. That is a different command: the
// server the CLI gives a developer's own assistant, which serves the CLI's
// tools through this library and is not a way to start an application's
// server. A document saying what it is, or that it does not exist yet, is
// right to name it, and the check used to refuse that sentence along with the
// promise it was written for.
var namespacedCommand = regexp.MustCompile(`aru mcp:[a-z]`)

// TestNoDocumentPromisesACommandThatStartsOrDescribesAServer reads every
// document and every Go file this module ships.
//
// The check read two files once, and the false claim lived in the two it did
// not read. So it reads the tree, less the suite, which has to spell the
// pattern to test it.
func TestNoDocumentPromisesACommandThatStartsOrDescribesAServer(t *testing.T) {
	root := moduleRoot(t)

	read := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "tests" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		read++
		if found := namespacedCommand.FindString(string(body)); found != "" {
			t.Errorf("%s names %q, a command no version of the CLI registered", rel(root, path), found)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}
	if read == 0 {
		t.Fatal("no document was read, so this test cannot fail")
	}
}

// TestTheCheckRefusesThePromiseAndNotTheCommand pins both edges of the pattern
// against the sentences it exists to tell apart.
func TestTheCheckRefusesThePromiseAndNotTheCommand(t *testing.T) {
	for sentence, refused := range map[string]bool{
		"Local is `aru mcp:start`.":                          true,
		"Describe is for aru mcp:list.":                      true,
		"`aru mcp` serves the CLI's tools to the developer.": false,
		"aru mcp: the developer's server, not yours.":        false,
		"There is no aru mcp command in this release.":       false,
	} {
		if got := namespacedCommand.MatchString(sentence); got != refused {
			t.Errorf("%q: refused %v, want %v", sentence, got, refused)
		}
	}
}

// toolMethods, resourceMethods and promptMethods are the method sets of the
// three interfaces an application implements.
var (
	toolMethods     = []string{"Name", "Description", "Schema", "Handle"}
	resourceMethods = []string{"URI", "Name", "Description", "MimeType", "Read"}
	promptMethods   = []string{"Name", "Description", "Arguments", "Render"}
)

// TestThisPackageShipsNoTool.
//
// A tool here would be a tool with an opinion about somebody else's domain, and
// the same goes for an MCP resource or a prompt: this module only knows how to
// call one as the subject that asked. The check reads the methods every type in
// the package declares, so a type that grows the method set of one of the three
// interfaces fails here, whatever it is called.
func TestThisPackageShipsNoTool(t *testing.T) {
	root := moduleRoot(t)

	files, err := filepath.Glob(filepath.Join(root, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("the package has no Go file to read: %v", err)
	}

	methods := map[string]map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", rel(root, path), err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			receiver := receiverType(fn.Recv.List[0].Type)
			if methods[receiver] == nil {
				methods[receiver] = map[string]bool{}
			}
			methods[receiver][fn.Name.Name] = true
		}
	}
	if len(methods) == 0 {
		t.Fatal("no method was read, so this test cannot fail")
	}

	for receiver, set := range methods {
		for kind, want := range map[string][]string{
			"Tool": toolMethods, "Resource": resourceMethods, "Prompt": promptMethods,
		} {
			if hasAll(set, want) {
				t.Errorf("%s implements %s: this module ships none, and a %s belongs to the application whose domain it is about",
					receiver, kind, kind)
			}
		}
	}
}

// receiverType names the type a method is declared on, pointer or not.
func receiverType(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverType(e.X)
	case *ast.IndexExpr:
		return receiverType(e.X)
	case *ast.Ident:
		return e.Name
	}
	return ""
}

// hasAll reports whether set holds every name in want.
func hasAll(set map[string]bool, want []string) bool {
	for _, name := range want {
		if !set[name] {
			return false
		}
	}
	return true
}

// The three strings the package doc is held to, all of them about the same
// sentence: what the Server does when a tool asks no policy.
//
// promisedGuarantee is what the published doc used to open with, and it is
// blocked by its exact words because those words shipped. disclosure is the
// clause that replaced it, and requiredHeading is the section it lives under.
// A rewrite that keeps the doc honest keeps both; one that puts a guarantee
// back has to delete the disclosure to read coherently, and deleting it fails
// here. That is what this can check by reading -- it cannot rule out a new
// promise worded some third way, which is why it measures the server first and
// only then reads.
const (
	promisedGuarantee = "Every tool carries a Grant"
	requiredHeading   = "# What this package checks, and what it does not"
	disclosure        = "runs no policy of its own"
)

// TestThePackageDocDescribesTheAuthorizationTheServerPerforms.
//
// The package doc is the first thing a reader of the published reference sees,
// so a guarantee written there reaches a consumer before any code does. This
// dispatches a tool with no policy in its path, reads what the server did about
// it, and holds the doc to the answer -- in both directions, so a doc that
// understates a server which does refuse fails here as well.
func TestThePackageDocDescribesTheAuthorizationTheServerPerforms(t *testing.T) {
	doc := packageDoc(t)

	server, tool := helpers.UnpolicedServer(nil)
	answer := server.Call(context.Background(), auth.Subject{}, "list_everything", nil)
	refused := answer.IsError && !tool.Ran

	if refused {
		if strings.Contains(doc, disclosure) {
			t.Errorf("the server refused a tool that asks no policy, and the package doc still says it %q", disclosure)
		}
		return
	}

	if strings.Contains(doc, promisedGuarantee) {
		t.Errorf("the package doc promises %q, and a tool that asks no policy was dispatched and answered %q",
			promisedGuarantee, answer.Text)
	}
	if !strings.Contains(doc, requiredHeading) {
		t.Errorf("the package doc has no %q section, so it does not say where the boundary is", requiredHeading)
	}
	if !strings.Contains(doc, disclosure) {
		t.Errorf("the package doc does not say the server %q, which is what the dispatch above measured", disclosure)
	}
}

// packageDoc is the comment above the package clause of mcp.go, which is the
// text the published reference carries.
func packageDoc(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(moduleRoot(t), "mcp.go"))
	if err != nil {
		t.Fatalf("reading mcp.go: %v", err)
	}

	clause := strings.Index(string(body), "\npackage mcp\n")
	if clause < 0 {
		t.Fatal("mcp.go has no package clause, so there is no doc comment above one")
	}
	return string(body)[:clause]
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime did not report the test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
}
