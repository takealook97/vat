package lint_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

// brain/source-revision-drift is the rule the knowledge layer exists for, and
// until now nothing tested it. These pin both of its original branches before
// the path-aware behaviour is added on top, because a rule nobody has a test
// for is a rule that can be changed into something else by accident.

// driftFixture builds a workspace with a brain repository and one governed
// repository that has real history.
func driftFixture(t *testing.T) (*workspace.Workspace, string, string) {
	t.Helper()
	ws, root := withBrain(t, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct,
	})
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	return ws, root, ws.RepoPath(manifest.Repo{Name: "payments"})
}

// commitAt writes a file, commits it, and returns the resulting revision.
func commitAt(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "commit "+name)
	revision, err := gitx.HeadRevision(context.Background(), dir)
	if err != nil {
		t.Fatalf("head revision: %v", err)
	}
	return revision
}

func claimPinnedTo(t *testing.T, root, id, sourceRef string) {
	t.Helper()
	writeBrainRecord(t, root, "gaps/"+id+"-pinned.md", fmt.Sprintf(
		"id: %s\nstatus: active\nclaim_kind: current-state\nowned_by: payments\n"+
			"source_ref: %s\nobserved_at: \"2026-08-25\"", id, sourceRef))
}

func TestAClaimWhosePinnedRevisionIsGoneIsReportedAsDrift(t *testing.T) {
	// Arrange: a rewritten or dropped commit is the one case where the evidence
	// is not merely older than the repository — it is not there at all.
	ws, root, _ := driftFixture(t)
	claimPinnedTo(t, root, "G-0001", "payments@0123456789abcdef0123456789abcdef01234567")

	// Act
	finding, found := rules(runOnline(t, ws))["brain/source-revision-drift"]

	// Assert
	if !found {
		t.Fatal("a claim pinned to a revision the repository does not have was reported as current")
	}
	if !strings.Contains(finding.Message, "no longer resolves") {
		t.Errorf("message = %q; it does not say the revision is gone", finding.Message)
	}
}

func TestAClaimWithNoPinnedFileIsReportedWhenTheRepositoryMovesOn(t *testing.T) {
	// Arrange: without a path there is nothing narrower to ask about, so
	// repository movement remains the only available signal.
	ws, root, repo := driftFixture(t)
	base := commitAt(t, repo, "docs/ordering.md", "one\n")
	commitAt(t, repo, "internal/unrelated.go", "package unrelated\n")
	claimPinnedTo(t, root, "G-0001", "payments@"+base)

	// Act
	finding, found := rules(runOnline(t, ws))["brain/source-revision-drift"]

	// Assert
	if !found {
		t.Fatal("a claim pinned to a repository that has moved was reported as current")
	}
	if !strings.Contains(finding.Message, "has moved 1 commit") {
		t.Errorf("message = %q; it does not count what moved", finding.Message)
	}
}

func TestAPinnedFileNobodyTouchedIsNotDrift(t *testing.T) {
	// Arrange: this is the case the whole rule was too coarse to see. A
	// workspace whose busiest repository takes two hundred unrelated commits
	// was reporting every claim it owned, every run, forever — and a rule that
	// fires on everything is read as firing on nothing.
	ws, root, repo := driftFixture(t)
	base := commitAt(t, repo, "docs/ordering.md", "one\n")
	commitAt(t, repo, "internal/unrelated.go", "package unrelated\n")
	commitAt(t, repo, "internal/other.go", "package other\n")
	claimPinnedTo(t, root, "G-0001", "payments@"+base+":docs/ordering.md")

	// Act
	finding, found := rules(runOnline(t, ws))["brain/source-revision-drift"]

	// Assert
	if found {
		t.Errorf("the pinned file never changed, yet the claim was reported as drifted: %q", finding.Message)
	}
}

func TestAPinnedFileThatChangedIsDriftAndSaysSo(t *testing.T) {
	// Arrange
	ws, root, repo := driftFixture(t)
	base := commitAt(t, repo, "docs/ordering.md", "one\n")
	commitAt(t, repo, "internal/unrelated.go", "package unrelated\n")
	commitAt(t, repo, "docs/ordering.md", "two\n")
	claimPinnedTo(t, root, "G-0001", "payments@"+base+":docs/ordering.md")

	// Act
	finding, found := rules(runOnline(t, ws))["brain/source-revision-drift"]

	// Assert
	if !found {
		t.Fatal("the file a claim was read from changed and the claim was reported as current")
	}
	if !strings.Contains(finding.Message, "docs/ordering.md") {
		t.Errorf("message = %q; it does not name the evidence that moved", finding.Message)
	}
	if strings.Contains(finding.Message, "has moved 2 commits") {
		t.Errorf("message = %q; it counts the repository's movement rather than the file's", finding.Message)
	}
}

func TestTheDriftFixNamesACommandThatActsOnTheRecord(t *testing.T) {
	// Arrange: the fix line used to say `vat brain review`, which lists
	// provisional, stale, and quarantined records — never an active one that
	// drifted. It named a command that could not show the record it was
	// attached to, which is worse than naming none.
	ws, root, repo := driftFixture(t)
	base := commitAt(t, repo, "docs/ordering.md", "one\n")
	commitAt(t, repo, "docs/ordering.md", "two\n")
	claimPinnedTo(t, root, "G-0001", "payments@"+base+":docs/ordering.md")

	// Act
	finding, found := rules(runOnline(t, ws))["brain/source-revision-drift"]

	// Assert
	if !found {
		t.Fatal("no drift was reported")
	}
	if !strings.Contains(finding.Fix, "vat brain promote G-0001") {
		t.Errorf("fix = %q; it does not name the command that re-verifies this record", finding.Fix)
	}
	if !strings.Contains(finding.Fix, "--reverified") {
		t.Errorf("fix = %q; re-pinning evidence that moved needs --reverified", finding.Fix)
	}
}

func TestAnAbbreviatedPinStillNamingHeadIsNotDrift(t *testing.T) {
	// Arrange: lint compared pinned revisions by prefix and promotion compared
	// them exactly, so a claim written with a short hash read as current to one
	// command and as moved to the other. A record cannot be both.
	ws, root, repo := driftFixture(t)
	head := commitAt(t, repo, "docs/ordering.md", "one\n")
	claimPinnedTo(t, root, "G-0001", "payments@"+head[:8])

	// Act
	finding, found := rules(runOnline(t, ws))["brain/source-revision-drift"]

	// Assert
	if found {
		t.Errorf("a claim pinned to HEAD by an abbreviated hash was reported as drifted: %q", finding.Message)
	}
}
