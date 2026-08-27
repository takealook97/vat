package brain_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

func TestMarkMakesADirectoryABrainAndWritesNothingElse(t *testing.T) {
	// Arrange: a repository somebody already keeps notes in. Adoption asserts
	// that this directory is the brain and promises to convert nothing.
	root := t.TempDir()
	existing := filepath.Join(root, "NOTES.md")
	if err := os.WriteFile(existing, []byte("mine\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	marked, err := brain.Mark(root)
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}

	// Assert
	if !marked {
		t.Error("Mark reported it wrote nothing to a directory that was not a brain")
	}
	if !brain.IsBrain(root) {
		t.Error("the directory is still not a brain after being marked")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 2 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("marking scaffolded more than the marker: %v", names)
	}
	content, err := os.ReadFile(existing)
	if err != nil || string(content) != "mine\n" {
		t.Errorf("the file that was already there was disturbed: %v %q", err, content)
	}
}

func TestMarkIsANoOpOnADirectoryThatIsAlreadyABrain(t *testing.T) {
	// Arrange: adoption can be run twice, and the second run must not rewrite a
	// marker whose contents record when the layout was created.
	root := t.TempDir()
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("Init: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(root, brain.MarkerFile))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}

	// Act
	marked, err := brain.Mark(root)
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}

	// Assert
	if marked {
		t.Error("Mark claimed to write a marker that was already there")
	}
	after, err := os.ReadFile(filepath.Join(root, brain.MarkerFile))
	if err != nil || string(after) != string(before) {
		t.Errorf("the existing marker was rewritten: %v", err)
	}
}
