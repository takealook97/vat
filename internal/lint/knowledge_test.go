package lint_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

// The rules covered here are the ones that reach past the manifest into the
// records a workspace accumulates. They are the rules most likely to be quietly
// skipped, because every one of them returns nothing at all when the layer they
// inspect has not been adopted — which is correct, and also indistinguishable
// from a rule that never runs.

// withBrain adds a brain repository to a fixture and returns its path.
func withBrain(t *testing.T, repos ...manifest.Repo) (*workspace.Workspace, string) {
	t.Helper()
	repos = append(repos, manifest.Repo{
		Name: "knowledge", Origin: "https://example.invalid/acme/knowledge.git",
		Role: manifest.RoleBrain,
	})
	ws := fixture(t, repos...)
	path, ok := ws.BrainPath()
	if !ok {
		t.Fatal("the fixture declared a brain repository and the workspace reports none")
	}
	return ws, path
}

func TestABrainRepositoryWithNoRecordsIsAWarningNotAFailure(t *testing.T) {
	// Arrange: a repository declared as the brain but never initialised has
	// nothing to check yet. Greeting a new workspace with hard failures it
	// cannot act on is how a rule set gets ignored wholesale.
	ws, _ := withBrain(t)

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["brain/not-initialised"]
	if !found {
		t.Fatalf("an uninitialised brain repository went unreported: %+v", report.Findings)
	}
	if finding.Severity != lint.SeverityWarn {
		t.Errorf("severity = %s, want warn; there is nothing here a user did wrong", finding.Severity)
	}
}

func TestAnInitialisedBrainWithNoDriftReportsNothing(t *testing.T) {
	// Arrange: the projections must agree with the records immediately after a
	// build, or the rule fires on every clean workspace and teaches nothing.
	ws, root := withBrain(t)
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	store, err := brain.Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := brain.Build(store, reference); err != nil {
		t.Fatalf("build: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	for _, rule := range []string{"brain/not-initialised", "brain/generated-drift"} {
		if _, found := rules(report)[rule]; found {
			t.Errorf("%s fired on a freshly built knowledge repository: %+v", rule, report.Findings)
		}
	}
}

func TestAGeneratedProjectionThatNoLongerMatchesItsRecordsIsAnError(t *testing.T) {
	// Arrange: the projection is what people read. One that disagrees with the
	// records is a summary presenting itself as the canon.
	ws, root := withBrain(t)
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	if _, err := brain.Create(root, brain.NewRecordInput{
		Kind: brain.KindDecision, ID: "D-0001", Title: "Pricing is per seat",
		Status: brain.StatusActive, Now: reference,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Act: records exist that no build has ever been projected over.
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["brain/generated-drift"]
	if !found {
		t.Fatalf("a projection that predates its records went unreported: %+v", report.Findings)
	}
	if !finding.Fixable {
		t.Error("regenerating a projection is mechanically safe and should be marked fixable")
	}
}

func TestAClaimNamingARepositoryTheWorkspaceDoesNotGovernIsReported(t *testing.T) {
	// Arrange: provenance pointing at nothing reads as verified, which is worse
	// than provenance that is absent.
	ws, root := withBrain(t, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct,
	})
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	if _, err := brain.Create(root, brain.NewRecordInput{
		Kind: brain.KindGap, ID: "G-0001", Title: "Ordering is not retry-safe",
		Status: brain.StatusActive, ClaimKind: brain.ClaimCurrentState,
		OwnedBy: "never-enrolled", SourceRef: "never-enrolled@3f9a1c2e",
		Now: reference,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Act: this rule reads git, so it only runs when lint is allowed the disk.
	report, err := lint.Run(t.Context(), ws, lint.Options{Now: reference})
	if err != nil {
		t.Fatalf("lint.Run: %v", err)
	}

	// Assert
	found := false
	for _, finding := range report.Findings {
		if finding.Rule == "brain/source-repo-unknown" && finding.Subject == "G-0001" {
			found = true
		}
	}
	if !found {
		t.Errorf("a claim owned by an unknown repository went unreported: %+v", report.Findings)
	}
}

func TestAChangesetLeftOpenPastTheLimitIsReported(t *testing.T) {
	// Arrange: repositories mid-contract-change with no closing evidence is the
	// state the whole layer exists to make visible.
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct,
	})
	writeChangeset(t, ws, changeset.Changeset{
		ID: "CS-0001", Objective: "Move cancellation to v2",
		Status: changeset.StatusOpen,
		// Far enough back that no plausible default limit tolerates it.
		OpenedAt: reference.AddDate(-1, 0, 0).Format("2006-01-02"),
		Repositories: []changeset.Participant{
			{Name: "payments", RollbackPoint: "3f9a1c2e8b74a1c93d0f5c1f80ab2e0d19bcdeff"},
		},
	})

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["changeset/open-too-long"]
	if !found {
		t.Fatalf("a changeset open for a year went unreported: %+v", report.Findings)
	}
	if finding.Subject != "CS-0001" {
		t.Errorf("the finding names %q, not the changeset it is about", finding.Subject)
	}
	if finding.Severity != lint.SeverityWarn {
		t.Errorf("severity = %s, want warn; slow work is not a broken workspace", finding.Severity)
	}
}

func TestAClosedChangesetIsNeverReportedAsOpenTooLong(t *testing.T) {
	// Arrange: age only matters while the work is unfinished. Reporting old
	// completed records would make the rule fire more the longer a workspace is
	// used, which is the opposite of useful.
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct,
	})
	writeChangeset(t, ws, changeset.Changeset{
		ID: "CS-0001", Objective: "Move cancellation to v2",
		Status:     changeset.StatusClosed,
		OpenedAt:   reference.AddDate(-1, 0, 0).Format("2006-01-02"),
		ClosedAt:   reference.AddDate(-1, 0, 7).Format("2006-01-02"),
		Acceptance: "cancel-then-refund passes end to end",
		Repositories: []changeset.Participant{{
			Name:          "payments",
			RollbackPoint: "3f9a1c2e8b74a1c93d0f5c1f80ab2e0d19bcdeff",
			Revision:      "a71c93d05c1f80ab2e0d19bcdeff3f9a1c2e8b74",
		}},
	})

	// Act
	report := run(t, ws)

	// Assert
	if _, found := rules(report)["changeset/open-too-long"]; found {
		t.Errorf("a closed changeset was reported as open too long: %+v", report.Findings)
	}
}

// writeChangeset puts a record on disk the way the changeset commands would.
func writeChangeset(t *testing.T, ws *workspace.Workspace, set changeset.Changeset) {
	t.Helper()
	if err := os.MkdirAll(ws.ChangesetsDir(), 0o755); err != nil {
		t.Fatalf("create changesets directory: %v", err)
	}
	if err := changeset.Save(ws.Root, set); err != nil {
		t.Fatalf("save changeset: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws.ChangesetsDir(), set.ID+".yaml")); err != nil {
		t.Fatalf("the changeset did not land where lint looks for it: %v", err)
	}
}

// Keep the reference clock honest: these tests date records relative to it, and
// a zero value would make every age assertion meaningless.
func TestTheReferenceClockUsedByTheseTestsIsSet(t *testing.T) {
	if reference.IsZero() || reference.Equal(time.Time{}) {
		t.Fatal("the shared reference time is the zero value, so every age in this file is nonsense")
	}
}
