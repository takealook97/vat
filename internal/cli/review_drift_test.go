package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The drift rule's fix line used to say `vat brain review`, and the queue it
// named holds provisional, stale, and quarantined records — never an active
// claim whose evidence moved. One workspace carried 46 drifted claims and a
// review queue of 11, with no overlap at all. Either the hint was wrong or the
// command was incomplete; this makes the command complete, without demoting
// anything, because a moved revision is not a claim becoming false.

// driftedClaim pins a claim to a file, then changes that file, so the claim's
// own evidence has demonstrably moved.
func driftedClaim(t *testing.T, h *workspaceFixture, repo string) {
	t.Helper()
	h.mustRun("brain", "new", "gap", "--title", "Ordering is not retry-safe",
		"--claim", "current-state", "--owner", repo, "--source-path", "README.md")
	h.mustRun("brain", "promote", "G-0001", "--reviewer", "alex")
	path := filepath.Join(h.path(repo), "README.md")
	if err := os.WriteFile(path, []byte("# rewritten\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, h.path(repo), "add", "-A")
	git(t, h.path(repo), "commit", "--quiet", "-m", "rewrite the ordering notes")
}

func TestReviewListsAClaimWhoseEvidenceMoved(t *testing.T) {
	// Arrange
	h := brainFixture(t, "payments")
	driftedClaim(t, h, "payments")

	// Act
	output := h.mustRun("brain", "review")

	// Assert
	if !strings.Contains(output, "G-0001") {
		t.Errorf("the queue does not list the claim whose evidence moved:\n%s", output)
	}
}

func TestReviewLeavesADriftedClaimActiveAndCitable(t *testing.T) {
	// Arrange: a revision moving is not a claim becoming false. Listing it for
	// re-check must never be the same act as demoting it, or a typo commit in
	// the owning repository would silently remove an answer.
	h := brainFixture(t, "payments")
	driftedClaim(t, h, "payments")

	// Act
	h.mustRun("brain", "review")

	// Assert
	record, err := os.ReadFile(findRecord(t, h, "G-0001"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(record), "status: active") {
		t.Errorf("listing a drifted claim changed its status:\n%s", record)
	}
}

func TestReviewCanNarrowToWhatDrifted(t *testing.T) {
	// Arrange: one claim awaiting a first review, one active claim that drifted.
	h := brainFixture(t, "payments")
	driftedClaim(t, h, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Orders own their idempotency keys")

	// Act
	drifted := h.mustRun("brain", "review", "--drifted")

	// Assert
	if !strings.Contains(drifted, "G-0001") {
		t.Errorf("--drifted dropped the claim whose evidence moved:\n%s", drifted)
	}
	if strings.Contains(drifted, "D-0001") {
		t.Errorf("--drifted kept a record that is merely unreviewed:\n%s", drifted)
	}
}

func TestReviewJSONSaysWhyEachRowIsThere(t *testing.T) {
	// Arrange: a consumer has to tell a record awaiting its first review from an
	// active claim whose evidence moved. They need different work done to them.
	h := brainFixture(t, "payments")
	driftedClaim(t, h, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Orders own their idempotency keys")

	// Act
	var items []struct {
		ID     string `json:"id"`
		Source string `json:"source"`
	}
	if code := h.runJSON(&items, "brain", "review"); code != ExitOK {
		t.Fatalf("vat brain review --json exited %d", code)
	}

	// Assert
	sources := map[string]string{}
	for _, item := range items {
		sources[item.ID] = item.Source
	}
	if sources["G-0001"] != "drift" {
		t.Errorf("G-0001 source = %q; want \"drift\"", sources["G-0001"])
	}
	if sources["D-0001"] != "queue" {
		t.Errorf("D-0001 source = %q; want \"queue\"", sources["D-0001"])
	}
}
