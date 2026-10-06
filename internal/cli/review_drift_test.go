package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewJSONCarriesEvidenceForReverification(t *testing.T) {
	for _, sourcePath := range []string{"README.md", ""} {
		t.Run("path="+sourcePath, func(t *testing.T) {
			h := brainFixture(t, "payments")
			pinned := gitOutput(t, h.path("payments"), "rev-parse", "HEAD")
			args := []string{"brain", "new", "gap", "--title", "Ordering is not retry-safe",
				"--claim", "current-state", "--owner", "payments"}
			if sourcePath != "" {
				args = append(args, "--source-path", sourcePath)
			}
			h.mustRun(args...)
			h.mustRun("brain", "promote", "G-0001", "--reviewer", "alex")
			if err := os.WriteFile(filepath.Join(h.path("payments"), "README.md"), []byte("# rewritten\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			git(t, h.path("payments"), "add", "-A")
			git(t, h.path("payments"), "commit", "--quiet", "-m", "rewrite notes")
			head := gitOutput(t, h.path("payments"), "rev-parse", "HEAD")

			var items []struct {
				ID       string            `json:"id"`
				Why      string            `json:"why"`
				Evidence map[string]string `json:"evidence"`
			}
			if code := h.runJSON(&items, "brain", "review", "--drifted"); code != ExitOK {
				t.Fatalf("review exited %d", code)
			}
			if len(items) != 1 || items[0].ID != "G-0001" {
				t.Fatalf("review items = %+v", items)
			}
			evidence := items[0].Evidence
			if evidence["repo"] != "payments" || evidence["pinned_revision"] != pinned || evidence["head_revision"] != head || head == pinned {
				t.Errorf("evidence = %+v; want payments pinned %s, head %s", evidence, pinned, head)
			}
			path, present := evidence["source_path"]
			if path != sourcePath || present != (sourcePath != "") {
				t.Errorf("source_path = %q (present %v); want %q", path, present, sourcePath)
			}
			if items[0].Why == "" {
				t.Error("drift explanation was lost")
			}
		})
	}
}

func TestReviewJSONOmitsEvidenceForQueueItems(t *testing.T) {
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Orders own idempotency keys")
	var items []map[string]json.RawMessage
	if code := h.runJSON(&items, "brain", "review"); code != ExitOK {
		t.Fatalf("review exited %d", code)
	}
	if len(items) != 1 {
		t.Fatalf("review items = %+v", items)
	}
	if _, present := items[0]["evidence"]; present {
		t.Errorf("queue item has evidence: %s", items[0]["evidence"])
	}
}

func TestReviewJSONKeepsHeadWhenThePinNoLongerResolves(t *testing.T) {
	h := brainFixture(t, "payments")
	driftedClaim(t, h, "payments")
	path := findRecord(t, h, "G-0001")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const missing = "0123456789abcdef0123456789abcdef01234567"
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "source_ref:") {
			lines[i] = "source_ref: payments@" + missing + ":README.md"
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	var items []struct {
		Why      string `json:"why"`
		Evidence struct {
			Repo            string `json:"repo"`
			PinnedRevision  string `json:"pinned_revision"`
			SourcePath      string `json:"source_path"`
			HeadRevision    string `json:"head_revision"`
			PinUnresolvable bool   `json:"pin_unresolvable"`
		} `json:"evidence"`
	}
	if code := h.runJSON(&items, "brain", "review", "--drifted"); code != ExitOK {
		t.Fatalf("review exited %d", code)
	}
	if len(items) != 1 {
		t.Fatalf("review items = %+v", items)
	}
	evidence := items[0].Evidence
	if !evidence.PinUnresolvable || evidence.PinnedRevision != missing || evidence.HeadRevision != headOf(t, h, "payments") || evidence.Repo != "payments" || evidence.SourcePath != "README.md" {
		t.Errorf("unresolvable evidence = %+v", evidence)
	}
	if !strings.Contains(items[0].Why, "no longer resolves") {
		t.Errorf("why = %q", items[0].Why)
	}
}

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
