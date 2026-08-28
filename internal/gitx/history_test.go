package gitx_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/takealook97/vat/internal/gitx"
)

// These two are read by callers that have to tell one failure from another —
// a rewritten upstream from an ordinary divergence, a preserved remote from a
// mismatched one — so the distinction each makes is tested here rather than
// through the packages that consume them.

func TestHaveCommonAncestorSeesSharedHistory(t *testing.T) {
	// Arrange
	dir := newRepo(t)
	run(t, dir, "branch", "later")

	// Act
	shared, err := gitx.HaveCommonAncestor(context.Background(), dir, "HEAD", "later")

	// Assert
	if err != nil {
		t.Fatalf("HaveCommonAncestor returned an error: %v", err)
	}
	if !shared {
		t.Error("two refs on one history were reported as unrelated")
	}
}

func TestHaveCommonAncestorReportsUnrelatedHistoriesWithoutFailing(t *testing.T) {
	// Arrange: an orphan branch is what a force-pushed rewrite looks like to a
	// clone that already had the old history. git answers "no base" by exiting
	// non-zero with nothing on stderr, and reading that as a failure would turn
	// the answer into an error.
	dir := newRepo(t)
	run(t, dir, "checkout", "--quiet", "--orphan", "rewritten")
	if err := os.WriteFile(filepath.Join(dir, "OTHER.md"), []byte("two\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "--quiet", "-m", "rewritten")

	// Act
	shared, err := gitx.HaveCommonAncestor(context.Background(), dir, "main", "rewritten")

	// Assert
	if err != nil {
		t.Fatalf("HaveCommonAncestor returned an error for an answerable question: %v", err)
	}
	if shared {
		t.Error("two histories with no commit in common were reported as related")
	}
}

func TestHaveCommonAncestorFailsOnARevisionThatIsNotThere(t *testing.T) {
	// Arrange: "no such revision" is not "no common ancestor". Collapsing them
	// would report a typo as a rewritten upstream.
	dir := newRepo(t)

	// Act
	_, err := gitx.HaveCommonAncestor(context.Background(), dir, "HEAD", "no-such-ref")

	// Assert
	if err == nil {
		t.Error("an unresolvable revision was answered rather than reported")
	}
}

func TestRemoteNamesListsEveryRemoteAClonKeeps(t *testing.T) {
	// Arrange: a repository renamed on the forge usually keeps the old URL as a
	// second remote so the old route still works.
	dir := newRepo(t)
	run(t, dir, "remote", "add", "origin", "https://example.invalid/acme/payments.git")
	run(t, dir, "remote", "add", "legacy", "https://example.invalid/acme/old-payments.git")

	// Act
	names, err := gitx.RemoteNames(context.Background(), dir)

	// Assert
	if err != nil {
		t.Fatalf("RemoteNames returned an error: %v", err)
	}
	for _, want := range []string{"origin", "legacy"} {
		if !slices.Contains(names, want) {
			t.Errorf("RemoteNames = %v, missing %q", names, want)
		}
	}
}

func TestRemoteNamesReturnsNothingForACloneWithNoRemote(t *testing.T) {
	// Arrange: a repository created locally has none, and an empty list is the
	// answer rather than a failure.
	dir := newRepo(t)

	// Act
	names, err := gitx.RemoteNames(context.Background(), dir)

	// Assert
	if err != nil {
		t.Fatalf("RemoteNames returned an error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("RemoteNames = %v, want none", names)
	}
}
