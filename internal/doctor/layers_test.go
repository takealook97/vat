package doctor_test

import (
	"os"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/doctor"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

// doctor judges layers a workspace may never have adopted. Each check here
// returns nothing at all when its layer is absent, which is correct and also
// indistinguishable from a check that no longer runs — so each is driven from
// both sides.

func findingFor(report doctor.Report, section, subject string) (doctor.Finding, bool) {
	for _, finding := range report.Findings {
		if finding.Section == section && finding.Subject == subject {
			return finding, true
		}
	}
	return doctor.Finding{}, false
}

func judge(t *testing.T, ws *workspace.Workspace) doctor.Report {
	t.Helper()
	return doctor.Run(t.Context(), ws, doctor.Options{Now: reference})
}

func productRepo() manifest.Repo {
	return manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct,
	}
}

func brainFixture(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	ws := fixture(t, productRepo(), manifest.Repo{
		Name: "knowledge", Origin: "https://example.invalid/acme/knowledge.git",
		Role: manifest.RoleBrain,
	})
	root, ok := ws.BrainPath()
	if !ok {
		t.Fatal("the fixture declared a brain repository and the workspace reports none")
	}
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	return ws, root
}

func mustCreateRecord(t *testing.T, root, id, title string, status brain.Status) {
	t.Helper()
	if _, err := brain.Create(root, brain.NewRecordInput{
		Kind: brain.KindDecision, ID: id, Title: title, Status: status, Now: reference,
	}); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
}

func openChangeset(t *testing.T, ws *workspace.Workspace, opened string) {
	t.Helper()
	if err := changeset.Save(ws.Root, changeset.Changeset{
		ID: "CS-0001", Objective: "Move cancellation to v2",
		Status:   changeset.StatusOpen,
		OpenedAt: opened,
		Repositories: []changeset.Participant{
			{Name: "payments", RollbackPoint: "3f9a1c2e8b74a1c93d0f5c1f80ab2e0d19bcdeff"},
		},
	}); err != nil {
		t.Fatalf("save changeset: %v", err)
	}
}

func TestDoctorSaysNothingAboutLayersTheWorkspaceNeverAdopted(t *testing.T) {
	// Arrange: a workspace that only uses the manifest must not be told about
	// knowledge records or changesets it has never written. Advice for a layer
	// you did not adopt is noise, and noise is how a report stops being read.
	ws := fixture(t, productRepo())

	// Act
	report := judge(t, ws)

	// Assert
	for _, finding := range report.Findings {
		if finding.Section == "brain" || finding.Section == "changesets" {
			t.Errorf("a workspace with neither layer was judged on %s: %+v", finding.Section, finding)
		}
	}
}

func TestDoctorCountsCitableRecordsSeparatelyFromAllOfThem(t *testing.T) {
	// Arrange: the number that matters is how much of the knowledge is usable as
	// evidence right now, not how much of it exists.
	ws, root := brainFixture(t)
	mustCreateRecord(t, root, "D-0001", "Reviewed and citable", brain.StatusActive)
	mustCreateRecord(t, root, "D-0002", "Nobody has reviewed this", brain.StatusProvisional)

	// Act
	report := judge(t, ws)

	// Assert
	finding, found := findingFor(report, "brain", "records")
	if !found {
		t.Fatalf("doctor said nothing about an initialised knowledge repository: %+v", report.Findings)
	}
	if !strings.Contains(finding.Detail, "2") || !strings.Contains(finding.Detail, "1") {
		t.Errorf("the record count does not distinguish citable from total: %q", finding.Detail)
	}
}

func TestDoctorReportsOpenCrossRepositoryWorkAndFlagsWhatHasGoneStale(t *testing.T) {
	// Arrange: open work with no closing evidence is the state changesets exist
	// to surface, and its age turns it from normal into a problem.
	ws := fixture(t, productRepo())
	openChangeset(t, ws, reference.AddDate(-1, 0, 0).Format("2006-01-02"))

	// Act
	report := judge(t, ws)

	// Assert
	finding, found := findingFor(report, "changesets", "open work")
	if !found {
		t.Fatalf("doctor said nothing about an open changeset: %+v", report.Findings)
	}
	if finding.Status != doctor.StatusWarn {
		t.Errorf("a changeset open for a year is %q, want a warning: %q", finding.Status, finding.Detail)
	}
}

func TestDoctorTreatsRecentlyOpenedWorkAsNormal(t *testing.T) {
	// Arrange: a changeset opened this morning is work in progress. Reporting it
	// as a problem would make the check fire on correct behaviour.
	ws := fixture(t, productRepo())
	openChangeset(t, ws, reference.Format("2006-01-02"))

	// Act
	report := judge(t, ws)

	// Assert
	finding, found := findingFor(report, "changesets", "open work")
	if !found {
		t.Fatalf("doctor said nothing about an open changeset: %+v", report.Findings)
	}
	if finding.Status != doctor.StatusOK {
		t.Errorf("work opened today is %q: %q", finding.Status, finding.Detail)
	}
}

func TestEveryFindingCarriesEnoughToActOn(t *testing.T) {
	// Arrange: a finding with no section or subject cannot be looked up, and one
	// with no status cannot be triaged.
	ws, root := brainFixture(t)
	mustCreateRecord(t, root, "D-0001", "Something", brain.StatusActive)

	// Act
	report := judge(t, ws)

	// Assert
	if len(report.Findings) == 0 {
		t.Fatal("doctor judged an entire workspace and reported nothing")
	}
	for _, finding := range report.Findings {
		if finding.Section == "" || finding.Subject == "" || finding.Status == "" {
			t.Errorf("finding %+v cannot be acted on", finding)
		}
	}
}

func TestNoFindingEverQuotesCredentialMaterial(t *testing.T) {
	// Arrange: findings about credentials are limited to existence, permissions,
	// and age. A report that quoted one would turn a diagnosis into a leak.
	ws, root := brainFixture(t)
	mustCreateRecord(t, root, "D-0001", "Something", brain.StatusActive)

	// Act
	report := judge(t, ws)

	// Assert
	for _, finding := range report.Findings {
		for _, marker := range []string{"PRIVATE KEY", "ghp_", "AKIA", "-----BEGIN"} {
			if strings.Contains(finding.Detail, marker) {
				t.Errorf("a finding quoted credential material: %+v", finding)
			}
		}
	}
}

// A repository declared as the brain and never initialised is the state every
// workspace starts in when `vat init` guesses the role from a directory name.
// doctor reported it as a brain with an empty review queue and its generated
// files "out of date", and pointed at `vat brain build` — which wrote an index
// into a directory that is not a brain and left doctor reporting the whole
// section healthy. A diagnostic that can be made to certify a state by
// following its own advice is worse than one that says nothing.
//
// `vat lint` had the guard and the reason for it in a comment; this one never
// asked.
func TestDoctorDoesNotJudgeABrainThatWasNeverInitialised(t *testing.T) {
	// Arrange
	ws := fixture(t, productRepo(), manifest.Repo{
		Name: "knowledge", Origin: "https://example.invalid/acme/knowledge.git",
		Role: manifest.RoleBrain,
	})
	root, ok := ws.BrainPath()
	if !ok {
		t.Fatal("the fixture declared a brain repository and the workspace reports none")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Act
	report := judge(t, ws)

	// Assert
	finding, found := findingFor(report, "brain", "knowledge")
	if !found {
		t.Fatalf("an uninitialised brain was not reported at all: %+v", report.Findings)
	}
	if finding.Status != doctor.StatusWarn {
		t.Errorf("expected a warning, got %v", finding.Status)
	}
	if finding.Fix != "vat brain init" {
		t.Errorf("doctor advises %q; the repository is not a brain yet", finding.Fix)
	}
	for _, subject := range []string{"records", "review queue", "generated files"} {
		if _, reported := findingFor(report, "brain", subject); reported {
			t.Errorf("doctor judged %q of a brain that does not exist", subject)
		}
	}
}

// The guard must not have silenced the checks it stands in front of.
func TestDoctorStillJudgesABrainThatWasInitialised(t *testing.T) {
	// Arrange
	ws, _ := brainFixture(t)

	// Act
	report := judge(t, ws)

	// Assert
	if _, found := findingFor(report, "brain", "records"); !found {
		t.Errorf("an initialised brain went unjudged: %+v", report.Findings)
	}
}
