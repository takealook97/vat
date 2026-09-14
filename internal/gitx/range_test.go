package gitx_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/takealook97/vat/internal/gitx"
)

// A claim in the knowledge layer pins the revision it was read from. Asking
// whether that evidence still holds is two questions, and they are not the same
// one: how far the repository has travelled, and whether the file the claim was
// read from was touched at all. The first is what the drift rule reported for
// years, and it is why a typo commit in an unrelated directory produced a
// warning. These two functions are what let the second question be asked.

// commitFileAt writes a file and commits it, returning the new revision.
func commitFileAt(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "--quiet", "-m", "change "+name)
	revision, err := gitx.HeadRevision(context.Background(), dir)
	if err != nil {
		t.Fatalf("head revision: %v", err)
	}
	return revision
}

func TestCommitsBetweenCountsOnlyOneDirection(t *testing.T) {
	// Arrange
	dir := newRepo(t)
	base, err := gitx.HeadRevision(context.Background(), dir)
	if err != nil {
		t.Fatalf("head revision: %v", err)
	}
	commitFileAt(t, dir, "a.md", "a\n")
	commitFileAt(t, dir, "b.md", "b\n")

	// Act
	forward, err := gitx.CommitsBetween(context.Background(), dir, base, "HEAD")
	if err != nil {
		t.Fatalf("CommitsBetween returned an error: %v", err)
	}
	backward, err := gitx.CommitsBetween(context.Background(), dir, "HEAD", base)
	if err != nil {
		t.Fatalf("CommitsBetween returned an error: %v", err)
	}

	// Assert
	if forward != 2 {
		t.Errorf("forward count = %d; want 2", forward)
	}
	if backward != 0 {
		t.Errorf("backward count = %d; the range is not symmetrical and must not be counted as one", backward)
	}
}

func TestCommitsBetweenIsZeroWhenNothingMoved(t *testing.T) {
	// Arrange
	dir := newRepo(t)

	// Act
	count, err := gitx.CommitsBetween(context.Background(), dir, "HEAD", "HEAD")

	// Assert
	if err != nil {
		t.Fatalf("CommitsBetween returned an error: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d; want 0", count)
	}
}

func TestCommitsBetweenFailsOnARevisionThatIsNotThere(t *testing.T) {
	// Arrange: a revision that was rewritten away is not an answer of zero. A
	// caller reading zero would conclude the evidence still holds.
	dir := newRepo(t)

	// Act
	_, err := gitx.CommitsBetween(context.Background(), dir, "0123456789abcdef0123456789abcdef01234567", "HEAD")

	// Assert
	if err == nil {
		t.Error("counting from a revision the repository does not have was reported as a clean zero")
	}
}

func TestChangedPathsSeesOnlyWhatTheRangeTouched(t *testing.T) {
	// Arrange
	dir := newRepo(t)
	base := commitFileAt(t, dir, "docs/ordering.md", "one\n")
	commitFileAt(t, dir, "docs/ordering.md", "two\n")
	commitFileAt(t, dir, "internal/unrelated.go", "package unrelated\n")

	// Act
	all, err := gitx.ChangedPaths(context.Background(), dir, base, "HEAD")

	// Assert
	if err != nil {
		t.Fatalf("ChangedPaths returned an error: %v", err)
	}
	if !slices.Contains(all, "docs/ordering.md") || !slices.Contains(all, "internal/unrelated.go") {
		t.Errorf("ChangedPaths = %v; want both changed files", all)
	}
}

func TestChangedPathsNarrowsToThePathItWasAskedAbout(t *testing.T) {
	// Arrange: this is the case the whole change exists for. The repository
	// moved, but the file a claim was read from did not.
	dir := newRepo(t)
	base := commitFileAt(t, dir, "docs/ordering.md", "one\n")
	commitFileAt(t, dir, "internal/unrelated.go", "package unrelated\n")
	commitFileAt(t, dir, "internal/other.go", "package other\n")

	// Act
	touched, err := gitx.ChangedPaths(context.Background(), dir, base, "HEAD", "docs/ordering.md")

	// Assert
	if err != nil {
		t.Fatalf("ChangedPaths returned an error: %v", err)
	}
	if len(touched) != 0 {
		t.Errorf("ChangedPaths = %v; the pinned file was never touched, so the range must report nothing", touched)
	}
}

func TestChangedPathsReportsThePinnedFileWhenItReallyChanged(t *testing.T) {
	// Arrange
	dir := newRepo(t)
	base := commitFileAt(t, dir, "docs/ordering.md", "one\n")
	commitFileAt(t, dir, "internal/unrelated.go", "package unrelated\n")
	commitFileAt(t, dir, "docs/ordering.md", "two\n")

	// Act
	touched, err := gitx.ChangedPaths(context.Background(), dir, base, "HEAD", "docs/ordering.md")

	// Assert
	if err != nil {
		t.Fatalf("ChangedPaths returned an error: %v", err)
	}
	if !slices.Equal(touched, []string{"docs/ordering.md"}) {
		t.Errorf("ChangedPaths = %v; want only the pinned file", touched)
	}
}

func TestChangedPathsTreatsAPathspecAsDataRatherThanAnOption(t *testing.T) {
	// Arrange: a path reaching this from a record an agent wrote must never be
	// read by git as one of its own flags. The `--` separator is what enforces
	// that, and its absence would be a way to run git with arbitrary options.
	dir := newRepo(t)
	escape := filepath.Join(t.TempDir(), "escaped")
	base := commitFileAt(t, dir, "docs/ordering.md", "one\n")
	commitFileAt(t, dir, "docs/ordering.md", "two\n")

	// Act
	touched, err := gitx.ChangedPaths(context.Background(), dir, base, "HEAD", "--output="+escape)

	// Assert
	if err == nil && len(touched) != 0 {
		t.Errorf("ChangedPaths = %v; a pathspec beginning with a dash must not match anything", touched)
	}
	if _, statErr := os.Stat(escape); statErr == nil {
		t.Fatal("a pathspec was interpreted as a git option and wrote a file")
	}
}
