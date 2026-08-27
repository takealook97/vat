package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A doc comment separated from its function by a blank line is not a doc
// comment. go doc shows nothing, an editor shows nothing on hover, and the
// reasoning the comment records — which CONTRIBUTING.md says is the only reason
// a comment earns its place — is invisible to everyone who did not open the
// file at that line.
//
// gofmt does not move it and no linter this project runs reports it, so it kept
// happening: 783fff2 reattached one, and eight more were found afterwards,
// almost certainly left by the refactor that split the large command files.
// Nothing is going to catch it except this.
//
// Only a comment opening with the function's own name is reported. That is the
// Go convention for documentation, and it is what separates a detached doc
// comment from a deliberate free-standing note about the code below it.
func TestNoDocCommentIsSeparatedFromWhatItDocuments(t *testing.T) {
	// Arrange
	const repositoryRoot = "../.."
	fileSet := token.NewFileSet()

	// Act & Assert
	walked := 0
	err := filepath.WalkDir(repositoryRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "bin" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		walked++
		file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			return nil
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Doc != nil {
				continue
			}
			opensAt := fileSet.Position(function.Pos()).Line
			for _, group := range file.Comments {
				if fileSet.Position(group.End()).Line != opensAt-2 {
					continue
				}
				text := strings.TrimSpace(strings.TrimPrefix(group.List[0].Text, "//"))
				if first, _, _ := strings.Cut(text, " "); first == function.Name.Name {
					t.Errorf("%s:%d: the doc comment for %s is separated from it by a blank line, which hides it from go doc",
						path, fileSet.Position(group.Pos()).Line, function.Name.Name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}
	if walked == 0 {
		t.Fatal("no Go files were examined; the walk found nothing and this test stopped checking anything")
	}
}

// AGENTS.md states eight rules this code holds itself to and calls breaking one
// a defect regardless of what the change achieves. The tool exists because a
// rule only written down is a hope, and four of those eight are decidable from
// the source. They were hopes.
//
// Test code is exempt: driving a repository into a state worth reporting means
// putting it there.

// productionGoFiles walks every non-test Go file under internal/ and cmd/.
func productionGoFiles(t *testing.T, visit func(path, source string)) {
	t.Helper()
	visited := 0
	for _, root := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			visited++
			visit(path, string(content))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if visited == 0 {
		t.Fatal("no production files were walked; the layout changed and this test stopped checking anything")
	}
}

// Rule 2: never silently modify a working tree. No stash, reset, checkout, or
// automatic merge.
//
// `merge --ff-only` is the one exception and is the documented behaviour of
// `vat sync`: it advances a clean branch to a commit it is already an ancestor
// of, and fails rather than creating one. `stash list` reads.
func TestNothingModifiesAWorkingTreeItWasNotAskedTo(t *testing.T) {
	// Arrange
	forbidden := regexp.MustCompile(`"(reset|checkout|stash|clean|rebase|cherry-pick|revert)"`)
	allowed := regexp.MustCompile(`"stash", "list"|"merge", "--ff-only"`)

	// Act & Assert
	productionGoFiles(t, func(path, source string) {
		for _, line := range strings.Split(source, "\n") {
			if allowed.MatchString(line) || !forbidden.MatchString(line) {
				continue
			}
			// A git subcommand reaches git as an argument to gitx.Run, so a
			// quoted verb outside one is prose about the state, not a call.
			if !strings.Contains(line, "Run(ctx") && !strings.Contains(line, "Command(") {
				continue
			}
			t.Errorf("%s: %s\n  AGENTS.md rule 2: never silently modify a working tree",
				path, strings.TrimSpace(line))
		}
	})
}

// Rule 3: never rewrite a remote. A mismatch is a supply-chain signal.
//
// The guarantee is worth more as a missing capability than as a discipline: a
// helper that rewrote a remote sat in gitx with no caller, one merge away from
// having one.
func TestNothingCanRewriteARemote(t *testing.T) {
	// Arrange
	rewrites := regexp.MustCompile(`"set-url"|func SetRemoteURL`)

	// Act & Assert
	productionGoFiles(t, func(path, source string) {
		if rewrites.MatchString(source) {
			t.Errorf("%s can rewrite a remote\n  AGENTS.md rule 3: a mismatch is a supply-chain signal, never a redirection", path)
		}
	})
}

// Rule 8: every write is atomic. An interrupted run must never leave a
// half-written manifest or record.
func TestEveryWriteGoesThroughTheAtomicHelper(t *testing.T) {
	// Arrange
	direct := regexp.MustCompile(`os\.(WriteFile|Create)\(`)

	// Act & Assert
	productionGoFiles(t, func(path, source string) {
		if strings.HasSuffix(path, filepath.Join("internal", "fsx", "fsx.go")) {
			// The atomic helper is where the real write happens.
			return
		}
		for _, line := range strings.Split(source, "\n") {
			if direct.MatchString(line) {
				t.Errorf("%s: %s\n  AGENTS.md rule 8: every write goes through fsx.WriteFileAtomic",
					path, strings.TrimSpace(line))
			}
		}
	})
}

// The dependency seam AGENTS.md marks in bold: internal/brain imports neither
// manifest nor gitx, so a workspace that never adopts the knowledge layer pays
// nothing for it and the package could be lifted out entirely.
func TestTheBrainImportsNeitherTheManifestNorGit(t *testing.T) {
	// Arrange
	fileSet := token.NewFileSet()
	parsed := 0

	// Act & Assert
	err := filepath.WalkDir("../../internal/brain", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		parsed++
		for _, imported := range file.Imports {
			value := strings.Trim(imported.Path.Value, `"`)
			if strings.HasSuffix(value, "/internal/manifest") || strings.HasSuffix(value, "/internal/gitx") {
				t.Errorf("%s imports %s; AGENTS.md marks that seam as one to keep clean", path, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/brain: %v", err)
	}
	if parsed == 0 {
		t.Fatal("internal/brain parsed to nothing; the layout changed and this test stopped checking anything")
	}
}

// A filesystem error already carries the path it is about. Wrapping it with the
// path again prints it twice, and two layers doing it printed it three times:
//
//	.agents/skills/weird/SKILL.md: read .agents/skills/weird/SKILL.md: read
//	.agents/skills/weird/SKILL.md: is a directory
//
// That is the first thing somebody reads when their workspace is in a state
// they did not expect, and it was in fifteen places. Nothing but this would say
// when it comes back.
func TestNoFilesystemErrorIsWrappedWithThePathItAlreadyCarries(t *testing.T) {
	// Arrange
	doubling := regexp.MustCompile(`fmt\.Errorf\("(read|write|open|create|remove|stat) %s: %w"`)

	// Act & Assert
	productionGoFiles(t, func(path, source string) {
		for number, line := range strings.Split(source, "\n") {
			if doubling.MatchString(line) {
				t.Errorf("%s:%d: %s\n  the error already names the path; wrapping it repeats it",
					path, number+1, strings.TrimSpace(line))
			}
		}
	})
}

// Everything vat prints passes through the printer, which renders a control
// character rather than executing it. A write straight to the process's own
// streams goes round that, and the values are file content somebody else may
// have written.
func TestNothingWritesToTheTerminalAroundThePrinter(t *testing.T) {
	// Arrange
	direct := regexp.MustCompile(`os\.(Stdout|Stderr)|fmt\.Print(f|ln)?\(`)

	// Act & Assert
	productionGoFiles(t, func(path, source string) {
		// The printer is where the rendering happens, and the entry point wires
		// the real streams into it.
		if strings.Contains(path, filepath.Join("internal", "ui")) ||
			strings.Contains(path, filepath.Join("cmd", "vat")) {
			return
		}
		for number, line := range strings.Split(source, "\n") {
			if direct.MatchString(line) {
				t.Errorf("%s:%d: %s\n  everything printed goes through internal/ui, which renders a control character rather than executing it",
					path, number+1, strings.TrimSpace(line))
			}
		}
	})
}
