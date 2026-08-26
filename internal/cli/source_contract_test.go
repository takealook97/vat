package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
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
