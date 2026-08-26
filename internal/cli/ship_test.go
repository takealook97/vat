package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A changeset records that a set of revisions passed their checks together. It
// says nothing about whether those revisions ever reached anybody else, and a
// workspace that only asks the first question fills up with changesets closed
// on work still sitting on a branch.
func TestShipReportsARevisionThatNeverReachedTheDefaultBranch(t *testing.T) {
	// Arrange: verified locally, never pushed.
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")

	// Act
	code, output := h.run("ship", "CS-0001", "--offline")

	// Assert
	if code == ExitOK {
		t.Fatalf("ship passed a revision that is not on origin/main:\n%s", output)
	}
	if !strings.Contains(output, "not landed") {
		t.Errorf("ship did not name the state it found:\n%s", output)
	}
	if !strings.Contains(output, "origin/main") {
		t.Errorf("ship did not say which ref it looked on:\n%s", output)
	}
}

// The gate is one git question, so the answer must flip the moment the revision
// is reachable from the branch the repository ships from — with no forge, no
// API, and no pull request involved.
func TestShipPassesOnceTheRevisionIsOnTheDefaultBranch(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")
	landOnUpstream(t, h, "payments")

	// Act
	code, output := h.run("ship", "CS-0001", "--offline")

	// Assert
	if code != ExitOK {
		t.Fatalf("ship exited %d for a landed revision:\n%s", code, output)
	}
	if !strings.Contains(output, "landed on origin/main") {
		t.Errorf("ship did not record where the revision landed:\n%s", output)
	}
}

// Closing on checks alone produced a record saying a change was released while
// its revisions sat on branches nobody else could see. A reader six weeks later
// has no way to doubt that record, which is why the gate belongs here.
func TestCloseRefusesAChangesetThatHasNotLanded(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")

	// Act
	code, output := h.run("changeset", "close", "CS-0001", "--acceptance", "an order survives the round trip")

	// Assert
	if code == ExitOK {
		t.Fatalf("a changeset closed while its revision was still unlanded:\n%s", output)
	}
	if !strings.Contains(output, "vat ship CS-0001") {
		t.Errorf("close did not point at the command that judges landing:\n%s", output)
	}
}

// --force exists because a real workspace sometimes has to close a record the
// tool cannot confirm. What it must not do is make that indistinguishable from
// a change that actually shipped.
func TestAForcedCloseIsStillReportedAsUnlanded(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")
	h.mustRun("changeset", "close", "CS-0001", "--acceptance", "checked by hand", "--force")

	// Act
	_, output := h.run("lint")

	// Assert
	if !strings.Contains(output, "changeset/closed-unlanded") {
		t.Errorf("the waived gate left no trace:\n%s", output)
	}
}

// Landing is a claim about now. A previous run's answer surviving a force-push
// would leave the record asserting something that is no longer true.
func TestShipClearsAPreviousAnswerBeforeJudgingAgain(t *testing.T) {
	// Arrange: landed once, then the branch is rewound behind the revision.
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")
	landOnUpstream(t, h, "payments")
	h.mustRun("ship", "CS-0001", "--offline")
	rewindUpstream(t, h, "payments")

	// Act
	code, output := h.run("ship", "CS-0001", "--offline")

	// Assert
	if code == ExitOK {
		t.Fatalf("ship still reported a landing that had been rewound:\n%s", output)
	}
	if record := readChangeset(t, h, "CS-0001"); strings.Contains(record, "landed_on") {
		t.Errorf("the stale landing survived into the record:\n%s", record)
	}
}

// Shipping something nothing has proven would record that a change reached a
// default branch with no account of what made it safe to.
func TestShipRefusesAChangesetNothingHasVerified(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	code, output := h.run("ship", "CS-0001", "--offline")

	// Assert
	if code == ExitOK {
		t.Fatalf("ship judged an unverified changeset:\n%s", output)
	}
	if !strings.Contains(output, "vat changeset verify CS-0001") {
		t.Errorf("ship did not point at verification:\n%s", output)
	}
}

// landOnUpstream puts the repository's current revision on the upstream's main,
// which is what makes it an ancestor of origin/main locally.
func landOnUpstream(t *testing.T, h *workspaceFixture, name string) {
	t.Helper()
	// The upstream in these fixtures is a working clone rather than a bare
	// repository, so it refuses a push to the branch it has checked out. Only
	// the ref matters here; the upstream's worktree going stale affects nothing.
	git(t, originOf(t, h, name), "config", "receive.denyCurrentBranch", "ignore")
	git(t, h.path(name), "push", "--quiet", "origin", "HEAD:refs/heads/main")
}

// rewindUpstream moves the upstream's main behind the verified revision, the
// way a force-push or a revert would.
func rewindUpstream(t *testing.T, h *workspaceFixture, name string) {
	t.Helper()
	git(t, originOf(t, h, name), "update-ref", "refs/heads/main", "HEAD~1")
	git(t, h.path(name), "fetch", "--quiet", "--force", "origin", "+refs/heads/*:refs/remotes/origin/*")
}

func originOf(t *testing.T, h *workspaceFixture, name string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", h.path(name), "remote", "get-url", "origin").Output()
	if err != nil {
		t.Fatalf("read origin of %s: %v", name, err)
	}
	return strings.TrimSpace(string(out))
}

func readChangeset(t *testing.T, h *workspaceFixture, id string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(h.root, "changesets", id+".yaml"))
	if err != nil {
		t.Fatalf("read changeset %s: %v", id, err)
	}
	return string(content)
}

// A completion record is not a live view. Re-judging a closed changeset would
// overwrite what it closed with, and a branch rewound since then would erase
// the record of a change that did ship.
func TestShipRefusesAChangesetThatIsAlreadyClosed(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")
	landOnUpstream(t, h, "payments")
	h.mustRun("ship", "CS-0001", "--offline")
	h.mustRun("changeset", "close", "CS-0001", "--acceptance", "it works end to end")
	rewindUpstream(t, h, "payments")

	// Act
	code, output := h.run("ship", "CS-0001", "--offline")

	// Assert
	if code == ExitOK {
		t.Fatalf("ship re-judged a closed changeset:\n%s", output)
	}
	if record := readChangeset(t, h, "CS-0001"); !strings.Contains(record, "landed_on") {
		t.Errorf("the closing evidence was erased by a later run:\n%s", record)
	}
}

// Landing is a claim about now, so an observation that contradicts it must
// clear it — but failing to *look* is not an observation. Clearing up front
// meant a run with no network, or one where a repository was briefly not
// cloned, erased the evidence of a change that really had shipped, and no
// later run could put it back.
func TestShipKeepsLandingEvidenceWhenItCannotObserve(t *testing.T) {
	// Arrange: landed and recorded, then the remote is made unreachable.
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")
	landOnUpstream(t, h, "payments")
	h.mustRun("ship", "CS-0001", "--offline")
	if record := readChangeset(t, h, "CS-0001"); !strings.Contains(record, "landed_on") {
		t.Fatalf("the fixture did not land in the first place:\n%s", record)
	}
	git(t, h.path("payments"), "remote", "set-url", "origin", h.path("payments")+"-gone")

	// Act: online, so the fetch is attempted and fails.
	code, output := h.run("ship", "CS-0001")

	// Assert
	if code == ExitOK {
		t.Fatalf("ship claimed success while unable to reach the remote:\n%s", output)
	}
	if record := readChangeset(t, h, "CS-0001"); !strings.Contains(record, "landed_on") {
		t.Errorf("being unable to look erased evidence of a change that did ship:\n%s", record)
	}
}

// "The ref is not here" and "the commit is not on it" are different facts.
// Rendering the first as the second told the user something false about the
// branch — and `--remote` naming a remote this clone does not have was enough
// to trigger it for every repository at once.
func TestShipSaysTheRefIsAbsentRatherThanClaimingItDidNotLand(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")
	landOnUpstream(t, h, "payments")

	// Act: a remote whose tracking refs do not exist in this clone.
	code, output := h.run("ship", "CS-0001", "--offline", "--remote", "upstream")

	// Assert
	if code == ExitOK {
		t.Fatalf("ship passed against a ref it cannot see:\n%s", output)
	}
	if strings.Contains(output, "not landed") {
		t.Errorf("ship blamed the branch for a ref it never had:\n%s", output)
	}
	if !strings.Contains(output, "not present in this clone") {
		t.Errorf("ship did not say the ref is missing:\n%s", output)
	}
}

// A waived gate has to stay visible. Keying the rule on absent evidence instead
// reported every changeset closed before landing was recorded at all — the
// whole history of every upgrading workspace, with nothing anyone could do.
func TestOnlyAWaivedCloseIsReportedAsUnlanded(t *testing.T) {
	// Arrange: one changeset closed with --force, one closed after landing.
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Waived", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")
	h.mustRun("changeset", "close", "CS-0001", "--acceptance", "checked by hand", "--force")

	// A record from before landing existed: closed, landed, then the evidence
	// stripped the way an older vat's file would look.
	h.mustRun("changeset", "new", "Legacy", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0002")
	landOnUpstream(t, h, "payments")
	h.mustRun("ship", "CS-0002", "--offline")
	h.mustRun("changeset", "close", "CS-0002", "--acceptance", "shipped")
	stripLandingEvidence(t, h, "CS-0002")

	// Act
	_, output := h.run("lint")

	// Assert
	if !strings.Contains(output, "CS-0001") {
		t.Errorf("the waived close was not reported:\n%s", output)
	}
	if strings.Contains(output, "CS-0002") {
		t.Errorf("a changeset closed before landing was recorded was reported as a waiver:\n%s", output)
	}
}

// stripLandingEvidence rewrites a record the way one written by a vat that did
// not yet know about landing would look.
func stripLandingEvidence(t *testing.T, h *workspaceFixture, id string) {
	t.Helper()
	path := filepath.Join(h.root, "changesets", id+".yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	var kept []string
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "landed_on:") || strings.HasPrefix(trimmed, "landed_at:") ||
			strings.HasPrefix(trimmed, "landing_waived:") {
			continue
		}
		kept = append(kept, line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		t.Fatalf("write %s: %v", id, err)
	}
}
