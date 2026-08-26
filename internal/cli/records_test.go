package cli

import (
	"os"
	"strings"
	"testing"
)

// A record that can still be edited after it was completed is not a record of
// what happened, and a record whose provenance is a branch name is evidence for
// whatever that branch points at today. These drive the paths that decide both.

func TestAChangesetRefusesMoreRepositoriesOnceItIsClosed(t *testing.T) {
	// Arrange: a closed changeset is a completion record. Enrolling another
	// repository into it afterwards rewrites what was verified together, which
	// is the one claim the record exists to make.
	h := adoptedFixture(t, "payments", "console")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")
	h.mustRun("changeset", "close", "CS-0001",
		"--acceptance", "cancel-then-refund passes end to end", "--force")

	// Act
	code, output := h.run("changeset", "add", "CS-0001", "console")

	// Assert
	if code == ExitOK {
		t.Errorf("a repository was enrolled into a closed changeset:\n%s", output)
	}
}

func TestChangesetAddEnrolsAndSkipsWhatIsAlreadyThere(t *testing.T) {
	// Arrange: re-running the command must not double-enrol, because the second
	// entry would carry a different return point and the plan would be wrong.
	h := adoptedFixture(t, "payments", "console")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	added := h.mustRun("changeset", "add", "CS-0001", "console")
	again := h.mustRun("changeset", "add", "CS-0001", "console")

	// Assert
	if !strings.Contains(added, "console") {
		t.Errorf("console was not enrolled:\n%s", added)
	}
	if !strings.Contains(again, "already enrolled") {
		t.Errorf("a second enrolment was not reported as a skip:\n%s", again)
	}
	var current struct {
		Repositories []struct {
			Name string `json:"name"`
		} `json:"repositories"`
	}
	h.runJSON(&current, "changeset", "show", "CS-0001")
	seen := map[string]int{}
	for _, participant := range current.Repositories {
		seen[participant.Name]++
	}
	if seen["console"] != 1 {
		t.Errorf("console appears %d times in the record, want once", seen["console"])
	}
}

func TestChangesetAddRejectsARepositoryTheWorkspaceDoesNotGovern(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	code, output := h.run("changeset", "add", "CS-0001", "not-a-repository")

	// Assert
	if code == ExitOK {
		t.Errorf("an unknown repository was enrolled:\n%s", output)
	}
}

func TestACurrentStateClaimIsPinnedToAnExactRevision(t *testing.T) {
	// Arrange: a claim about the present is only evidence for the revision it
	// was read from. A branch name would keep moving and silently change what
	// the claim was evidence for.
	h := brainFixture(t, "payments")

	// Act
	output := h.mustRun("brain", "new", "gap", "--title", "Ordering is not retry-safe",
		"--claim", "current-state", "--owner", "payments")

	// Assert
	record, err := os.ReadFile(findRecord(t, h, "G-0001"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(record)
	if !strings.Contains(body, "source_ref: payments@") {
		t.Errorf("the record carries no pinned source reference:\n%s\n%s", output, body)
	}
	for _, moving := range []string{"payments@main", "payments@HEAD"} {
		if strings.Contains(body, moving) {
			t.Errorf("the source reference is a moving target (%s):\n%s", moving, body)
		}
	}
}

func TestAClaimCannotBeOwnedByARepositoryTheWorkspaceDoesNotGovern(t *testing.T) {
	// Arrange: provenance that names nothing real is worse than none, because
	// it reads as verified.
	h := brainFixture(t, "payments")

	// Act
	code, output := h.run("brain", "new", "gap", "--title", "Something",
		"--claim", "current-state", "--owner", "not-a-repository")

	// Assert
	if code == ExitOK {
		t.Errorf("a claim was pinned to a repository that does not exist:\n%s", output)
	}
}

func TestBrainSupersedeLinksBothRecordsSoTheChainReadsFromEitherEnd(t *testing.T) {
	// Arrange: a one-way link means the superseded record still reads as the
	// current answer when someone opens it directly.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Pricing is per seat", "--owner", "payments")
	h.mustRun("brain", "new", "decision", "--title", "Pricing is per workspace", "--owner", "payments")

	// Act
	h.mustRun("brain", "supersede", "D-0001", "D-0002")

	// Assert
	older, err := os.ReadFile(findRecord(t, h, "D-0001"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	newer, err := os.ReadFile(findRecord(t, h, "D-0002"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(older), "superseded") || !strings.Contains(string(older), "D-0002") {
		t.Errorf("the replaced record does not point at its replacement:\n%s", older)
	}
	if !strings.Contains(string(newer), "D-0001") {
		t.Errorf("the replacement does not say what it replaced:\n%s", newer)
	}
}

func TestBrainSupersedeRejectsARecordThatDoesNotExist(t *testing.T) {
	// Arrange
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Pricing is per seat", "--owner", "payments")

	// Act
	code, output := h.run("brain", "supersede", "D-0001", "D-9999")

	// Assert
	if code == ExitOK {
		t.Errorf("a record was superseded by one that does not exist:\n%s", output)
	}
}

func TestBrainCheckReportsEveryProblemAtOnce(t *testing.T) {
	// Arrange: these commands run in a loop while someone repairs a repository.
	// One problem per run is unusable.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "decision", "--title", "First", "--owner", "payments")
	h.mustRun("brain", "new", "decision", "--title", "Second", "--owner", "payments")
	for _, id := range []string{"D-0001", "D-0002"} {
		path := findRecord(t, h, id)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		broken := strings.Replace(string(content), "status: provisional", "status: nonsense", 1)
		if broken == string(content) {
			t.Fatalf("%s does not carry the status field this test corrupts:\n%s", id, content)
		}
		if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// Act
	code, output := h.run("brain", "check")

	// Assert
	if code == ExitOK {
		t.Errorf("`brain check` passed a repository with two invalid records:\n%s", output)
	}
	if !strings.Contains(output, "D-0001") || !strings.Contains(output, "D-0002") {
		t.Errorf("`brain check` did not report both broken records at once:\n%s", output)
	}
}

func TestBrainAdoptTakesOverADirectoryThatIsAlreadyThere(t *testing.T) {
	// Arrange: most teams have a knowledge repository before they have this
	// tool, so adoption must not require starting again.
	h := adoptedFixture(t, "payments", "notes")

	// Act
	output := h.mustRun("brain", "adopt", "notes")

	// Assert
	written, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(written), "role: brain") {
		t.Errorf("the adopted directory is not recorded as the brain:\n%s\n%s", output, written)
	}
	if code, out := h.run("brain", "new", "decision", "--title", "Anything"); code != ExitOK {
		t.Errorf("the knowledge commands still refuse to run after adoption:\n%s", out)
	}
}

func TestBrainSweepApplyDemotesRatherThanDeletes(t *testing.T) {
	// Arrange: an unverified claim is not a false one. Deleting it destroys the
	// reasoning; demoting it stops it being cited.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "gap", "--title", "Log rotation is weekly",
		"--claim", "current-state", "--owner", "payments")
	h.mustRun("brain", "promote", "G-0001", "--reviewer", "test")
	path := findRecord(t, h, "G-0001")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	aged := strings.Replace(string(content),
		`observed_at: "`+testNow.Format("2006-01-02")+`"`, `observed_at: "2020-01-01"`, 1)
	if aged == string(content) {
		t.Fatalf("the record carries no observed_at to age:\n%s", content)
	}
	if err := os.WriteFile(path, []byte(aged), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	output := h.mustRun("brain", "sweep", "--apply")

	// Assert
	if _, err := os.Stat(path); err != nil {
		t.Errorf("sweep deleted the record instead of demoting it:\n%s", output)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(after), "stale") {
		t.Errorf("an observation years past its window was not demoted:\n%s\n%s", output, after)
	}
}

func TestBrainReviewOverdueNarrowsToWhatIsPastItsWindow(t *testing.T) {
	// Arrange: a stale claim nothing cites can wait; the queue is only useful
	// if it can be narrowed to what has actually run out of time.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Recent enough", "--owner", "payments")

	// Act
	code, output := h.run("brain", "review", "--overdue")

	// Assert
	if code != ExitOK {
		t.Errorf("`brain review --overdue` exited %d:\n%s", code, output)
	}
	if strings.Contains(output, "Recent enough") {
		t.Errorf("a record written today was reported as overdue:\n%s", output)
	}
}

func TestEvidenceShowRendersTheRecordInBothForms(t *testing.T) {
	// Arrange: the packet is read by a person deciding whether to hand it over
	// and by a machine checking it was met.
	h := adoptedFixture(t, "payments")
	h.mustRun("evidence", "new", "EV-0001", "Tidy the ordering docs",
		"--repos", "payments", "--acceptance", "the doc names the current revision")

	// Act
	plain := h.mustRun("evidence", "show", "EV-0001")
	var structured struct {
		ID        string `json:"id"`
		Objective string `json:"objective"`
	}
	h.runJSON(&structured, "evidence", "show", "EV-0001")

	// Assert
	if !strings.Contains(plain, "Tidy the ordering docs") {
		t.Errorf("the plain rendering omits the objective:\n%s", plain)
	}
	if structured.ID != "EV-0001" || structured.Objective == "" {
		t.Errorf("the JSON rendering dropped a field the plain one shows: %+v", structured)
	}
}

func TestChangesetVerifyRecordsTheRevisionEachCheckRanAt(t *testing.T) {
	// Arrange: "the tests passed" is worth nothing six months later without the
	// revision they passed at. That pairing is the whole record.
	h := adoptedFixture(t, "payments", "console")
	addCheck(t, h, "payments", "echo payments-check")
	addCheck(t, h, "console", "echo console-check")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments,console")

	// Act
	output := h.mustRun("changeset", "verify", "CS-0001")

	// Assert
	var current struct {
		Repositories []struct {
			Name     string `json:"name"`
			Revision string `json:"revision"`
		} `json:"repositories"`
	}
	h.runJSON(&current, "changeset", "show", "CS-0001")
	if len(current.Repositories) != 2 {
		t.Fatalf("the record holds %d repositories:\n%s", len(current.Repositories), output)
	}
	for _, participant := range current.Repositories {
		if participant.Revision == "" {
			t.Errorf("%s was verified but the record does not say at which revision", participant.Name)
		}
	}
}

func TestChangesetVerifyFailsWhenARepositoryCheckFails(t *testing.T) {
	// Arrange: a verification that passed while a check failed would put a false
	// claim into a permanent record.
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "false")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	code, output := h.run("changeset", "verify", "CS-0001")

	// Assert
	if code == ExitOK {
		t.Errorf("verification passed while the repository's own check failed:\n%s", output)
	}
	var current struct {
		Status       string `json:"status"`
		Repositories []struct {
			Revision string `json:"revision"`
			Checks   []struct {
				Status   string `json:"status"`
				Revision string `json:"revision"`
			} `json:"checks"`
		} `json:"repositories"`
	}
	h.runJSON(&current, "changeset", "show", "CS-0001")
	if current.Status == "verified" {
		t.Errorf("the changeset is marked verified after a check failed: %+v", current)
	}
	// The failure is kept with the revision it happened on. A result recorded
	// against no revision cannot be re-checked, and one silently dropped makes
	// the next run look like the first.
	recorded := false
	for _, participant := range current.Repositories {
		for _, check := range participant.Checks {
			if check.Status == "fail" {
				recorded = true
				if check.Revision == "" {
					t.Error("a failed check was recorded against no revision")
				}
			}
		}
	}
	if !recorded {
		t.Errorf("the failure left no trace in the record: %+v", current)
	}
}

func TestAVerifiedChangesetClosesAndStopsBeingOpen(t *testing.T) {
	// Arrange: closing is what turns work in progress into a completion record.
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "echo ok")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")

	// Act
	h.mustRun("changeset", "close", "CS-0001",
		"--acceptance", "cancel-then-refund passes end to end", "--approved-by", "test")

	// Assert
	var open []struct {
		ID string `json:"id"`
	}
	h.runJSON(&open, "changeset", "list", "--open")
	if len(open) != 0 {
		t.Errorf("a closed changeset is still listed as open: %+v", open)
	}
	shown := h.mustRun("changeset", "show", "CS-0001")
	if !strings.Contains(shown, "cancel-then-refund") {
		t.Errorf("the acceptance statement was not kept:\n%s", shown)
	}
}

func TestRepoRemoveDeleteChangesNothingWhenNobodyConfirms(t *testing.T) {
	// Arrange: this is the one command that deletes a working tree. --delete
	// prompts even when --yes and --force were both given, so with no one there
	// to answer it must change nothing at all — not the directory, and not the
	// manifest entry that would otherwise be dropped first.
	h := adoptedFixture(t, "payments", "console")

	// Act
	output := h.mustRun("repo", "remove", "payments", "--delete", "--force")

	// Assert
	if _, err := os.Stat(h.path("payments")); err != nil {
		t.Errorf("an unanswered deletion prompt still removed the directory:\n%s", output)
	}
	if _, err := os.Stat(h.path("console")); err != nil {
		t.Error("removing payments also removed console")
	}
	written, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(written), "payments") {
		t.Error("the deletion was declined but the manifest entry was dropped anyway")
	}
}
