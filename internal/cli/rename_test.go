package cli

import (
	"os"
	"strings"
	"testing"
)

// Renaming a repository is a migration, not an edit. The name appears in the
// manifest entry, in the brain policy, in every open changeset, and in the
// generated contracts — and a rename that moves only the first leaves the
// workspace naming something that is not there.

func TestRenamingTheBrainRepositoryMovesTheBrainPolicyWithIt(t *testing.T) {
	// Arrange: adoption is what writes policy.brain.repo, and it is the state a
	// workspace renaming its knowledge repository is actually in.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "adopt", "brain")

	// Act
	output := h.mustRun("repo", "rename", "brain", "knowledge")

	// Assert
	written, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(written), "repo: brain") {
		t.Errorf("policy.brain.repo still names the old repository:\n%s\n%s", output, written)
	}
	if !strings.Contains(string(written), "repo: knowledge") {
		t.Errorf("policy.brain.repo does not name the renamed repository:\n%s", written)
	}
	// The proof that the two agree: a knowledge command has to reach it.
	if code, out := h.run("brain", "check"); code != ExitOK {
		t.Errorf("the knowledge layer is unreachable after the rename (exit %d):\n%s", code, out)
	}
}

func TestRenamingARepositoryEnrolledInAnOpenChangesetIsRefused(t *testing.T) {
	// Arrange: the record claims which revisions were proven together. Renaming
	// a participant out from under it either silently breaks the record or
	// rewrites a claim about the past, and both are worse than refusing.
	h := adoptedFixture(t, "payments", "console")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	code, output := h.run("repo", "rename", "payments", "billing")

	// Assert
	if code == ExitOK {
		t.Errorf("a participant in an open changeset was renamed silently:\n%s", output)
	}
	if !strings.Contains(output, "CS-0001") {
		t.Errorf("the refusal does not name the record that stands in the way:\n%s", output)
	}
}

func TestRenamingIsRefusedBeforeAnythingMovesWhenAChangesetBlocksIt(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	h.run("repo", "rename", "payments", "billing")

	// Assert
	if _, err := os.Stat(h.path("payments")); err != nil {
		t.Errorf("the directory moved despite the refusal: %v", err)
	}
	if _, err := os.Stat(h.path("billing")); err == nil {
		t.Error("a directory was created for a rename that was refused")
	}
}

func TestRenamingCanRecordTheNewOrigin(t *testing.T) {
	// Arrange: a repository renamed on the forge has a new URL, and the old one
	// stops being the identity the manifest should record.
	h := adoptedFixture(t, "payments")

	// Act
	output := h.mustRun("repo", "rename", "payments", "billing",
		"--origin", "https://example.invalid/acme/billing.git")

	// Assert
	written, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(written), "acme/billing.git") {
		t.Errorf("the new origin was not recorded:\n%s\n%s", output, written)
	}
	// vat never rewrites a remote. The clone still points where it did, and
	// doctor is what says so.
	if code, out := h.run("doctor"); code == ExitOK {
		t.Errorf("doctor certified a clone whose remote no longer matches the manifest:\n%s", out)
	}
}

func TestRenamePlanReportsEveryEffectAndWritesNothing(t *testing.T) {
	// Arrange: a rename touches the manifest, the brain policy, a directory,
	// .gitignore, and every generated contract. Finding out which of those
	// moved by running it is not a plan.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "adopt", "brain")
	before, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Act
	output := h.mustRun("repo", "rename", "brain", "knowledge", "--plan")

	// Assert
	for _, effect := range []string{"repos entry", "policy.brain.repo", ".gitignore", "AGENTS.md"} {
		if !strings.Contains(output, effect) {
			t.Errorf("the plan does not mention %s:\n%s", effect, output)
		}
	}
	// vat never rewrites a remote, so the plan has to say who does.
	if !strings.Contains(output, "remote set-url") {
		t.Errorf("the plan does not say how the remote is renamed:\n%s", output)
	}
	after, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != string(before) {
		t.Error("the plan wrote to the manifest")
	}
	if _, err := os.Stat(h.path("brain")); err != nil {
		t.Errorf("the plan moved the directory: %v", err)
	}
}
