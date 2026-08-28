package cli

import (
	"os"
	"strings"
	"testing"
)

// writeScript puts an executable check inside a repository and commits it, so
// the command recorded in the changeset carries none of the text the assertions
// look for.
func writeScript(t *testing.T, h *workspaceFixture, repo, name, body string) {
	t.Helper()
	if err := os.WriteFile(h.path(repo, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	commitAll(t, h, repo)
}

// A completion record's evidence has to survive being read months later. Two
// things it could not do: explain a failure it had proved, and say that a
// repository had nothing to prove.

func TestAFailedCheckKeepsEnoughOutputToExplainItself(t *testing.T) {
	// Arrange: the first line of a test runner is usually a progress bar. A
	// record holding only that can prove the check failed and cannot say which
	// test, which assertion, or where — and the failure was already visible in
	// the exit code.
	h := adoptedFixture(t, "payments")
	writeScript(t, h, "payments", "check.sh",
		"echo ....F...\necho 'FAIL test_cancel_refunds_the_order'\necho 'AssertionError 3 != 4'\nexit 1\n")
	addCheck(t, h, "payments", "sh check.sh")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	code, output := h.run("changeset", "verify", "CS-0001")

	// Assert
	if code == ExitOK {
		t.Fatalf("a failing check was recorded as a pass:\n%s", output)
	}
	record, err := os.ReadFile(h.path("changesets", "CS-0001.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{"test_cancel_refunds_the_order", "AssertionError"} {
		if !strings.Contains(string(record), want) {
			t.Errorf("the record cannot explain the failure; %q is missing:\n%s", want, record)
		}
	}
}

func TestAPassingCheckKeepsNoOutput(t *testing.T) {
	// Arrange: a record is not a log. Output is evidence for a failure; for a
	// pass the exit code already says everything the record claims.
	h := adoptedFixture(t, "payments")
	writeScript(t, h, "payments", "check.sh", "echo chatty-but-successful\nexit 0\n")
	addCheck(t, h, "payments", "sh check.sh")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	h.mustRun("changeset", "verify", "CS-0001")

	// Assert
	record, err := os.ReadFile(h.path("changesets", "CS-0001.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(record), "chatty-but-successful") {
		t.Errorf("a passing check filled the record with its output:\n%s", record)
	}
}

func TestARepositoryWithNothingToProveSaysSoInTheRecord(t *testing.T) {
	// Arrange: an empty check list meant two different things — nobody has
	// verified this yet, and there is nothing here that can be verified. A
	// workspace may accept the second deliberately; the record has to show that
	// it did rather than leave a gap that reads as work in progress.
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	h.run("changeset", "verify", "CS-0001")

	// Assert
	record, err := os.ReadFile(h.path("changesets", "CS-0001.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(record), "unverifiable:") {
		t.Errorf("the record does not say the repository had nothing to prove:\n%s", record)
	}
}

func TestChangesetStatusNamesWhatBlocksVerificationAndCommitsNothing(t *testing.T) {
	// Arrange: adopting the harness dirties every governed repository at once,
	// and verification refuses a dirty tree. The order to fix that in used to be
	// discoverable only by running verify and reading the failures.
	h := adoptedFixture(t, "payments", "console")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments,console")
	if err := os.WriteFile(h.path("payments", "NOTES.md"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	var report struct {
		Participants []struct {
			Name     string `json:"name"`
			State    string `json:"state"`
			Blocking bool   `json:"blocking"`
		} `json:"participants"`
		CommitFirst []string `json:"commit_first"`
	}
	h.runJSON(&report, "changeset", "status", "CS-0001")

	// Assert
	states := map[string]string{}
	for _, participant := range report.Participants {
		states[participant.Name] = participant.State
	}
	if states["payments"] != "uncommitted" {
		t.Errorf("payments = %q, want uncommitted", states["payments"])
	}
	if len(report.CommitFirst) == 0 {
		t.Error("the report does not say what to commit first")
	}
	// It reports; it does not repair. A command that committed to satisfy its
	// own gate would be deciding what a revision means.
	if _, err := os.Stat(h.path("payments", "NOTES.md")); err != nil {
		t.Errorf("status touched the working tree: %v", err)
	}
	code, out := h.run("changeset", "status", "CS-0001")
	if code != ExitOK {
		t.Errorf("a preflight that found work to do exited %d; it is a report:\n%s", code, out)
	}
}

func TestChangesetStatusSeparatesVerifiedFromMovedOn(t *testing.T) {
	// Arrange: checks describe the revision they ran on. When the repository
	// moves afterwards, the checks stay true and the record stops describing
	// what is there — which is not the same as never having been verified.
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")
	if err := os.WriteFile(h.path("payments", "NEXT.md"), []byte("more\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	commitAll(t, h, "payments")

	// Act
	code, output := h.run("changeset", "status", "CS-0001")

	// Assert
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, output)
	}
	if !strings.Contains(output, "moved") {
		t.Errorf("a verified revision that was left behind is not reported as moved:\n%s", output)
	}
}
