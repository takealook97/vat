package lint_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/workspace"
)

// The generated index routes readers to STATUS.md, ROADMAP.md, DECISIONS.md and
// the rest. vat generates neither their content nor their judgements, so no
// schema rule reached them — and a knowledge repository could report zero
// findings while every document its own entry point recommends had gone months
// without being revisited. The records had moved; the synthesis had not.

// gitAt runs a git command with both dates fixed, so a test can place a commit
// in time without waiting for it.
func gitAt(t *testing.T, dir string, when time.Time, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	stamp := when.Format(time.RFC3339)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// brainWithHistory builds a brain whose view and records were committed at
// given times.
func brainWithHistory(t *testing.T, viewAt, recordsAt time.Time) (*workspace.Workspace, string) {
	t.Helper()
	ws, root := withBrain(t)
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	gitAt(t, root, viewAt, "init", "--quiet", "--initial-branch", "main", ".")
	if err := os.WriteFile(filepath.Join(root, "ROADMAP.md"), []byte("# Roadmap\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitAt(t, root, viewAt, "add", "ROADMAP.md")
	gitAt(t, root, viewAt, "commit", "--quiet", "-m", "roadmap")

	writeBrainRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: active")
	gitAt(t, root, recordsAt, "add", "decisions")
	gitAt(t, root, recordsAt, "commit", "--quiet", "-m", "a decision")
	return ws, root
}

func TestAViewTheRecordsLeftBehindIsReported(t *testing.T) {
	// Arrange: the records moved on three months after the view was last
	// revisited, and the index still sends readers to it.
	ws, _ := brainWithHistory(t,
		reference.AddDate(0, -4, 0), reference.AddDate(0, -1, 0))

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["brain/view-stale"]
	if !found {
		t.Fatalf("a view three months behind its records went unreported: %+v", report.Findings)
	}
	if finding.Subject != "ROADMAP.md" {
		t.Errorf("subject = %q, want the view that fell behind", finding.Subject)
	}
	if finding.Fix == "" {
		t.Error("the rule states a problem and no remedy")
	}
}

func TestAViewRevisitedInsideTheReviewWindowIsNotReported(t *testing.T) {
	// Arrange: every record change makes every view technically older, and a
	// rule that fires on every commit is one people silence rather than act on.
	ws, _ := brainWithHistory(t,
		reference.AddDate(0, 0, -10), reference.AddDate(0, 0, -1))

	// Act
	report := run(t, ws)

	// Assert
	if _, found := rules(report)["brain/view-stale"]; found {
		t.Errorf("a view revisited inside the review window was reported: %+v", report.Findings)
	}
}
