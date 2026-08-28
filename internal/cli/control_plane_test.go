package cli

import (
	"os"
	"strings"
	"testing"
)

// A changeset records which revisions were proven together. The workspace root
// is a repository like any other — it holds vat.yaml, the roles, the skills,
// and the generated contracts every governed repository reads — and a change
// that spans it and two products used to be recordable only in part. The
// control plane's own revision was the one nothing could roll back to.

// withWorkspaceChecks gives the workspace root a canonical check of its own.
func withWorkspaceChecks(t *testing.T, h *workspaceFixture, command string) {
	t.Helper()
	path := h.path("vat.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	updated := strings.Replace(string(content), "workspace:\n",
		"workspace:\n    checks:\n        - "+command+"\n", 1)
	if updated == string(content) {
		t.Fatalf("the fixture manifest has no workspace block:\n%s", content)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// commitWorkspace commits the control plane, which every real workspace has
// done: vat.yaml is committed, and the changesets live beside it.
func commitWorkspace(t *testing.T, h *workspaceFixture) {
	t.Helper()
	git(t, h.root, "add", "-A")
	git(t, h.root, "commit", "--quiet", "-m", "workspace")
}

func TestEnrollingARepositoryWithNoCommitsSaysSo(t *testing.T) {
	// Arrange: the rollback point is the one thing enrolment exists to capture,
	// and a repository with no commits has none. Git words this as "ambiguous
	// argument 'HEAD'", which reads as a defect in vat.
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Change the contract", "--repos", "payments")

	// Act
	code, output := h.run("changeset", "add", "CS-0001", ".")

	// Assert
	if code == ExitOK {
		t.Errorf("a repository with no commits was enrolled:\n%s", output)
	}
	if !strings.Contains(output, "no commits yet") {
		t.Errorf("output does not say why the workspace could not be enrolled:\n%s", output)
	}
}

func TestAChangesetCanEnrolTheWorkspaceItself(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	commitWorkspace(t, h)
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	output := h.mustRun("changeset", "add", "CS-0001", ".")

	// Assert
	if !strings.Contains(output, "return point") {
		t.Errorf("the workspace was enrolled without a return point:\n%s", output)
	}
	var current struct {
		Repositories []struct {
			Name          string `json:"name"`
			RollbackPoint string `json:"rollback_point"`
		} `json:"repositories"`
	}
	h.runJSON(&current, "changeset", "show", "CS-0001")
	var found bool
	for _, participant := range current.Repositories {
		if participant.Name != "." {
			continue
		}
		found = true
		if participant.RollbackPoint == "" {
			t.Error("the workspace was enrolled with no revision to return to")
		}
	}
	if !found {
		t.Errorf("the workspace is not in the record: %+v", current.Repositories)
	}
}

func TestTheWorkspaceIsVerifiedByItsOwnDeclaredChecks(t *testing.T) {
	// Arrange: the control plane's checks belong to the workspace block,
	// because it is not in `repos:` and cannot be.
	h := adoptedFixture(t, "payments")
	withWorkspaceChecks(t, h, "git --version")
	commitWorkspace(t, h)
	h.mustRun("changeset", "new", "Change the contract", "--repos", ".")
	// The record lives in the workspace root, so writing it dirties the very
	// tree the checks are about to describe. Committing it is the same
	// discipline every other participant is held to.
	commitWorkspace(t, h)

	// Act
	code, output := h.run("changeset", "verify", "CS-0001")

	// Assert
	if code != ExitOK {
		t.Fatalf("verifying the workspace failed:\n%s", output)
	}
	if !strings.Contains(output, "git --version") {
		t.Errorf("the workspace's own check did not run:\n%s", output)
	}
}

func TestAWorkspaceWithNoChecksIsUnverifiableRatherThanVerified(t *testing.T) {
	// Arrange: silence here would record the control plane as proven by an
	// empty set of evidence, which is the claim this record exists to prevent.
	h := adoptedFixture(t, "payments")
	commitWorkspace(t, h)
	h.mustRun("changeset", "new", "Change the contract", "--repos", ".")
	commitWorkspace(t, h)

	// Act
	code, output := h.run("changeset", "verify", "CS-0001")

	// Assert
	if code == ExitOK {
		t.Errorf("a workspace with no declared checks was verified:\n%s", output)
	}
	if !strings.Contains(output, "no canonical checks") {
		t.Errorf("output does not say why nothing could be verified:\n%s", output)
	}
}

func TestARepositoryNamedInTheManifestIsStillRequiredToExist(t *testing.T) {
	// Arrange: "." is the one name that resolves outside `repos:`. Every other
	// unknown name must stay an error, or a typo enrols something silently.
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	code, output := h.run("changeset", "add", "CS-0001", "..")

	// Assert
	if code == ExitOK {
		t.Errorf("a name that is not a repository was enrolled:\n%s", output)
	}
}
