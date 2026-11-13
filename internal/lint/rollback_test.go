package lint_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/manifest"
)

// repoFixture is a governed repository with one commit, so a revision exists to
// record and to lose.
func repoFixture(t *testing.T, name string) manifest.Repo {
	t.Helper()
	return manifest.Repo{
		Name: name, Origin: "https://example.invalid/acme/" + name + ".git",
		Role: manifest.RoleProduct,
	}
}

// saveChangeset writes a record straight to disk, which is what a workspace
// that pulled somebody else's changeset has.
func saveChangeset(t *testing.T, root string, set changeset.Changeset) {
	t.Helper()
	if err := changeset.Save(root, set); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// headRevision reads the commit a repository stands at.
func headRevision(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// A changeset's one irreplaceable field is the way back. Everything else in the
// record can be reconstructed from git; the revision each repository stood at
// before the change began cannot, which is why it is captured at enrolment.
//
// Nothing checked that it still resolves. A rewritten history — a force-push, a
// squashed branch, a pruned object — leaves the record asserting a return point
// that is not there, and asserting it in exactly the voice of one that is.

func TestARollbackPointThatNoLongerResolvesIsReported(t *testing.T) {
	// Arrange: a changeset whose recorded return point is not a revision this
	// repository has ever had.
	ws := fixture(t, repoFixture(t, "payments"))
	saveChangeset(t, ws.Root, changeset.Changeset{
		ID: "CS-0001", Objective: "Move cancellation to v2",
		Status: changeset.StatusOpen, OpenedAt: "2026-08-25",
		Repositories: []changeset.Participant{{
			Name:          "payments",
			RollbackPoint: "0000000000000000000000000000000000000000",
		}},
	})

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["changeset/rollback-point-missing"]
	if !found {
		t.Fatalf("a return point that does not exist went unreported: %+v", report.Findings)
	}
	if finding.Severity != lint.SeverityError {
		t.Errorf("severity = %s, want error; the record promises a way back it does not have",
			finding.Severity)
	}
	if finding.Fixable {
		t.Error("marked fixable, but no command can recover a revision the repository does not hold")
	}
}

func TestARollbackPointThatStillResolvesIsNotReported(t *testing.T) {
	// Arrange
	ws := fixture(t, repoFixture(t, "payments"))
	commitOnce(t, filepath.Join(ws.Root, "payments"))
	head := headRevision(t, filepath.Join(ws.Root, "payments"))
	saveChangeset(t, ws.Root, changeset.Changeset{
		ID: "CS-0001", Objective: "Move cancellation to v2",
		Status: changeset.StatusOpen, OpenedAt: "2026-08-25",
		Repositories: []changeset.Participant{{Name: "payments", RollbackPoint: head}},
	})

	// Act
	report := run(t, ws)

	// Assert
	if _, found := rules(report)["changeset/rollback-point-missing"]; found {
		t.Errorf("a return point that resolves was reported: %+v", report.Findings)
	}
}

func TestAnUnclonedRepositoryIsNotAMissingRollbackPoint(t *testing.T) {
	// Arrange: a repository that is not on this machine says nothing about
	// whether its history still holds the revision. Reporting it would make
	// `vat lint` fail for every changeset naming a repository somebody has not
	// cloned yet.
	ws := fixture(t, repoFixture(t, "payments"))
	if err := os.RemoveAll(filepath.Join(ws.Root, "payments")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	saveChangeset(t, ws.Root, changeset.Changeset{
		ID: "CS-0001", Objective: "Move cancellation to v2",
		Status: changeset.StatusOpen, OpenedAt: "2026-08-25",
		Repositories: []changeset.Participant{{
			Name:          "payments",
			RollbackPoint: "0000000000000000000000000000000000000000",
		}},
	})

	// Act
	report := run(t, ws)

	// Assert
	if _, found := rules(report)["changeset/rollback-point-missing"]; found {
		t.Errorf("an uncloned repository was reported as a lost return point: %+v", report.Findings)
	}
}

// commitOnce gives a fixture repository a revision.
func commitOnce(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "init")
}

// The fix line used to tell the reader to "record why the way back was lost",
// and the record had nowhere to put it. A history rewritten on purpose is a
// fact the workspace can only acknowledge; without a field, the finding stayed
// red for good and taught everybody to read past it.

func lostChangeset(rollbackPoint, reason string) changeset.Changeset {
	return changeset.Changeset{
		ID: "CS-0001", Objective: "Move cancellation to v2",
		Status: changeset.StatusOpen, OpenedAt: "2026-08-25",
		Repositories: []changeset.Participant{{
			Name: "payments", RollbackPoint: rollbackPoint, RollbackLost: reason,
		}},
	}
}

func TestARecordedLossSilencesTheMissingRollbackPoint(t *testing.T) {
	// Arrange
	ws := fixture(t, repoFixture(t, "payments"))
	saveChangeset(t, ws.Root, lostChangeset(
		"0000000000000000000000000000000000000000",
		"history rewritten; the object is gone from the remote and every clone"))

	// Act
	report := run(t, ws)

	// Assert
	found := rules(report)
	if _, ok := found["changeset/rollback-point-missing"]; ok {
		t.Errorf("a loss the record acknowledges was still reported: %+v", report.Findings)
	}
	if _, ok := found["changeset/rollback-lost-but-present"]; ok {
		t.Errorf("a revision that does not resolve was called present: %+v", report.Findings)
	}
}

func TestABlankLossReasonIsNoReasonAtAll(t *testing.T) {
	// Arrange: whitespace is not an explanation, and accepting it would let the
	// field silence the rule without saying anything.
	ws := fixture(t, repoFixture(t, "payments"))
	saveChangeset(t, ws.Root, lostChangeset("0000000000000000000000000000000000000000", "  \t "))

	// Act
	report := run(t, ws)

	// Assert
	if _, ok := rules(report)["changeset/rollback-point-missing"]; !ok {
		t.Errorf("a blank reason silenced the finding: %+v", report.Findings)
	}
}

func TestTheMissingRollbackPointFixNamesTheField(t *testing.T) {
	// Arrange
	ws := fixture(t, repoFixture(t, "payments"))
	saveChangeset(t, ws.Root, lostChangeset("0000000000000000000000000000000000000000", ""))

	// Act
	finding := rules(run(t, ws))["changeset/rollback-point-missing"]

	// Assert
	if !strings.Contains(finding.Fix, "rollback_lost") {
		t.Errorf("fix = %q, want it to name the field that records the loss", finding.Fix)
	}
}

func TestALossClaimedForARevisionThatResolvesIsReported(t *testing.T) {
	// Arrange: the record says the way back is gone, and git says it is not.
	ws := fixture(t, repoFixture(t, "payments"))
	commitOnce(t, filepath.Join(ws.Root, "payments"))
	head := headRevision(t, filepath.Join(ws.Root, "payments"))
	saveChangeset(t, ws.Root, lostChangeset(head, "force-pushed over"))

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["changeset/rollback-lost-but-present"]
	if !found {
		t.Fatalf("a false claim of loss went unreported: %+v", report.Findings)
	}
	if finding.Severity != lint.SeverityWarn {
		t.Errorf("severity = %s, want warn; the way back exists, the record only undersells it",
			finding.Severity)
	}
	if finding.Fixable {
		t.Error("marked fixable, but removing a reason somebody wrote is not a mechanical repair")
	}
	if _, found := rules(report)["changeset/rollback-point-missing"]; found {
		t.Errorf("a resolving revision was reported missing: %+v", report.Findings)
	}
}
