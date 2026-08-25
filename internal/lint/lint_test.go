package lint_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

var reference = time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

// git runs a git command and fails the test if it errors.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, output)
	}
}

// fixture builds a workspace whose repositories are real git repositories
// pointing at the origin the manifest declares.
func fixture(t *testing.T, repos ...manifest.Repo) *workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "--quiet", "--initial-branch", "main", ".")
	built := manifest.Default("acme")
	for _, repo := range repos {
		built = manifest.WithRepo(built, repo)
		dir := filepath.Join(root, repo.Dir())
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create: %v", err)
		}
		git(t, dir, "init", "--quiet", "--initial-branch", "main", ".")
		git(t, dir, "remote", "add", "origin", repo.Origin)
	}
	if err := manifest.Save(filepath.Join(root, manifest.FileName), built); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	return ws
}

func run(t *testing.T, ws *workspace.Workspace) lint.Report {
	t.Helper()
	report, err := lint.Run(context.Background(), ws, lint.Options{Now: reference, Offline: true})
	if err != nil {
		t.Fatalf("lint.Run returned an error: %v", err)
	}
	return report
}

func rules(report lint.Report) map[string]lint.Finding {
	found := map[string]lint.Finding{}
	for _, finding := range report.Findings {
		found[finding.Rule] = finding
	}
	return found
}

func TestAGovernedRepositoryMissingFromGitignoreIsAnError(t *testing.T) {
	// Arrange: the next commit at the workspace root would absorb the clone.
	ws := fixture(t, manifest.Repo{Name: "payments", Origin: "u", Role: manifest.RoleProduct})

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["workspace/gitignore-drift"]
	if !found {
		t.Fatalf("the drift went unreported: %+v", report.Findings)
	}
	if finding.Severity != lint.SeverityError {
		t.Errorf("severity = %s, want error", finding.Severity)
	}
	if !finding.Fixable {
		t.Error("this is mechanically repairable and should be marked fixable")
	}
}

func TestFixRepairsWhatIsGeneratedAndTheRunThenPasses(t *testing.T) {
	// Arrange
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "u", Role: manifest.RoleProduct, Checks: []string{"make check"},
	})

	// Act
	if _, err := lint.Fix(ws, reference); err != nil {
		t.Fatalf("Fix returned an error: %v", err)
	}
	reloaded, err := workspace.OpenAt(ws.Root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	report := run(t, reloaded)

	// Assert
	if report.Errors() != 0 {
		t.Errorf("errors survived --fix: %+v", report.Findings)
	}
}

func TestHarnessDriftIsReportedAfterTheManifestChanges(t *testing.T) {
	// Arrange
	ws := fixture(t, manifest.Repo{Name: "payments", Origin: "u", Role: manifest.RoleProduct})
	if _, err := lint.Fix(ws, reference); err != nil {
		t.Fatalf("Fix returned an error: %v", err)
	}
	extended := manifest.WithRepo(ws.Manifest,
		manifest.Repo{Name: "console", Origin: "u", Role: manifest.RoleProduct})
	if err := ws.SaveManifest(extended); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	reloaded, err := workspace.OpenAt(ws.Root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}

	// Act
	report := run(t, reloaded)

	// Assert
	if _, found := rules(report)["harness/workspace-drift"]; !found {
		t.Errorf("the workspace contract no longer matches the manifest and was not reported: %+v",
			report.Findings)
	}
}

func TestAnOversizedWorkspaceContractIsReported(t *testing.T) {
	// Arrange: past the budget, a runtime may stop loading the per-repository
	// contracts below it.
	ws := fixture(t, manifest.Repo{Name: "payments", Origin: "u", Role: manifest.RoleProduct})
	if _, err := lint.Fix(ws, reference); err != nil {
		t.Fatalf("Fix returned an error: %v", err)
	}
	path := ws.Path("AGENTS.md")
	existing, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	padded := make([]byte, 0, len(existing)+20000)
	padded = append(padded, existing...)
	padded = append(padded, strings.Repeat("padding padding padding\n", 800)...)
	if err := os.WriteFile(path, padded, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	if _, found := rules(report)["harness/workspace-oversized"]; !found {
		t.Errorf("an oversized contract was not reported: %+v", report.Findings)
	}
}

func TestAWorkspaceWithNoDeclaredUntrustedSourcesIsReported(t *testing.T) {
	// Arrange
	ws := fixture(t)
	stripped := ws.Manifest
	stripped.Policy.Trust.Untrusted = nil
	ws.Manifest = stripped

	// Act
	report := run(t, ws)

	// Assert
	if _, found := rules(report)["policy/trust-undeclared"]; !found {
		t.Errorf("an undeclared trust boundary was not reported: %+v", report.Findings)
	}
}

func TestAProductRepositoryWithNoCanonicalChecksIsReported(t *testing.T) {
	// Arrange: without them a changeset has nothing to verify.
	ws := fixture(t, manifest.Repo{Name: "payments", Origin: "u", Role: manifest.RoleProduct})

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["repo/checks-missing"]
	if !found {
		t.Fatalf("the missing checks went unreported: %+v", report.Findings)
	}
	if finding.Severity != lint.SeverityWarn {
		t.Errorf("severity = %s, want warn", finding.Severity)
	}
}

func TestARequiredRepositoryThatIsNotClonedIsAnError(t *testing.T) {
	// Arrange
	ws := fixture(t)
	built := manifest.WithRepo(ws.Manifest, manifest.Repo{
		Name: "ghost", Origin: "u", Role: manifest.RoleProduct, Required: true,
	})
	ws.Manifest = built

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["repo/missing"]
	if !found {
		t.Fatalf("a missing required repository went unreported: %+v", report.Findings)
	}
	if finding.Severity != lint.SeverityError {
		t.Errorf("severity = %s, want error for a required repository", finding.Severity)
	}
}

func TestOnlyRestrictsTheRunToMatchingRules(t *testing.T) {
	// Arrange
	ws := fixture(t, manifest.Repo{Name: "payments", Origin: "u", Role: manifest.RoleProduct})

	// Act
	report, err := lint.Run(context.Background(), ws,
		lint.Options{Now: reference, Offline: true, Only: []string{"harness"}})

	// Assert
	if err != nil {
		t.Fatalf("lint.Run returned an error: %v", err)
	}
	for _, finding := range report.Findings {
		if !strings.Contains(finding.Rule, "harness") {
			t.Errorf("--only harness returned %q", finding.Rule)
		}
	}
}

func TestEveryReportedRuleIsListedInRuleNames(t *testing.T) {
	// Arrange: an unlisted rule cannot be selected with --only or documented.
	ws := fixture(t,
		manifest.Repo{Name: "payments", Origin: "u", Role: manifest.RoleProduct},
		manifest.Repo{Name: "brain", Origin: "u", Role: manifest.RoleBrain},
	)
	declared := map[string]bool{}
	for _, name := range lint.RuleNames() {
		declared[name] = true
	}

	// Act
	report := run(t, ws)

	// Assert
	for _, finding := range report.Findings {
		if !declared[finding.Rule] {
			t.Errorf("rule %q is reported but not listed by RuleNames", finding.Rule)
		}
	}
}
