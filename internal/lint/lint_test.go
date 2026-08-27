package lint_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/brain"
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
	ws := fixture(t, manifest.Repo{Name: "payments", Origin: "https://example.invalid/acme/payments.git", Role: manifest.RoleProduct})

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
		Name: "payments", Origin: "https://example.invalid/acme/payments.git", Role: manifest.RoleProduct, Checks: []string{"make check"},
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
	ws := fixture(t, manifest.Repo{Name: "payments", Origin: "https://example.invalid/acme/payments.git", Role: manifest.RoleProduct})
	if _, err := lint.Fix(ws, reference); err != nil {
		t.Fatalf("Fix returned an error: %v", err)
	}
	extended := manifest.WithRepo(ws.Manifest,
		manifest.Repo{Name: "console", Origin: "https://example.invalid/acme/console.git", Role: manifest.RoleProduct})
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
	ws := fixture(t, manifest.Repo{Name: "payments", Origin: "https://example.invalid/acme/payments.git", Role: manifest.RoleProduct})
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
	ws := fixture(t, manifest.Repo{Name: "payments", Origin: "https://example.invalid/acme/payments.git", Role: manifest.RoleProduct})

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
		Name: "ghost", Origin: "https://example.invalid/acme/ghost.git", Role: manifest.RoleProduct, Required: true,
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
	ws := fixture(t, manifest.Repo{Name: "payments", Origin: "https://example.invalid/acme/payments.git", Role: manifest.RoleProduct})

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
		manifest.Repo{Name: "payments", Origin: "https://example.invalid/acme/payments.git", Role: manifest.RoleProduct},
		manifest.Repo{Name: "brain", Origin: "https://example.invalid/acme/brain.git", Role: manifest.RoleBrain},
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

func TestFixableCountsOnlyWhatFixCanActuallyRepair(t *testing.T) {
	// Arrange: the number is printed as "N of these can be repaired with
	// vat lint --fix", so counting a finding that --fix will not touch sends
	// the reader to a command that changes nothing.
	ws := fixture(t, manifest.Repo{
		Name:   "payments",
		Origin: "https://example.invalid/acme/payments.git",
		Role:   manifest.RoleProduct,
	})

	// Act
	report := run(t, ws)

	// Assert
	counted := 0
	for _, finding := range report.Findings {
		if finding.Fixable {
			counted++
		}
	}
	if report.Fixable() != counted {
		t.Errorf("Fixable() = %d, but %d findings are marked fixable", report.Fixable(), counted)
	}
	if report.Fixable() == 0 {
		t.Fatal("this fixture used to produce repairable findings; the test no longer proves anything")
	}
	for _, finding := range report.Findings {
		if finding.Fixable && finding.Fix == "" {
			t.Errorf("finding %q is fixable but names no command", finding.Rule)
		}
	}
}

// `vat brain init <directory>` writes wherever it is pointed, so a workspace can
// hold a complete brain layout that no `vat brain` command can reach — they all
// resolve the brain through the manifest. Nothing reported that state before
// this rule: brain/not-initialised fires only for a repository the manifest
// already declares as the brain, which this directory is not.
func TestAScaffoldedBrainTheManifestNeverAdoptedIsReported(t *testing.T) {
	// Arrange: a brain layout on disk, and a manifest that names no brain.
	ws := fixture(t)
	dir := filepath.Join(ws.Root, "cortex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := brain.Init(dir, reference); err != nil {
		t.Fatalf("brain.Init: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["brain/unreferenced"]
	if !found {
		t.Fatalf("a brain no command can reach went unreported: %+v", report.Findings)
	}
	if finding.Subject != "cortex" {
		t.Errorf("subject = %q, want cortex", finding.Subject)
	}
	if !strings.Contains(finding.Fix, "vat repo adopt cortex") {
		t.Errorf("an ungoverned directory must be enrolled first; fix = %q", finding.Fix)
	}
}

// A governed repository needs only the second half of that advice, and telling
// someone to re-adopt what is already in the manifest teaches them to distrust
// the fix line.
func TestAGovernedButUnadoptedBrainIsToldOnlyToAdopt(t *testing.T) {
	// Arrange
	ws := fixture(t, manifest.Repo{
		Name: "cortex", Origin: "https://example.invalid/acme/cortex.git", Role: manifest.RoleProduct,
	})
	if _, err := brain.Init(filepath.Join(ws.Root, "cortex"), reference); err != nil {
		t.Fatalf("brain.Init: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["brain/unreferenced"]
	if !found {
		t.Fatalf("a brain no command can reach went unreported: %+v", report.Findings)
	}
	if strings.Contains(finding.Fix, "repo adopt") {
		t.Errorf("cortex is already governed; fix = %q", finding.Fix)
	}
	if !strings.Contains(finding.Fix, "vat brain adopt cortex") {
		t.Errorf("fix = %q, want the adopt command", finding.Fix)
	}
}

// The rule must stay silent for the repository that IS the brain, or every
// correctly configured workspace carries a permanent warning and the whole
// report stops being read.
func TestTheAdoptedBrainIsNotReportedAsUnreferenced(t *testing.T) {
	// Arrange
	ws := fixture(t, manifest.Repo{
		Name: "brain", Origin: "https://example.invalid/acme/brain.git", Role: manifest.RoleBrain,
	})
	if _, err := brain.Init(filepath.Join(ws.Root, "brain"), reference); err != nil {
		t.Fatalf("brain.Init: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	if finding, found := rules(report)["brain/unreferenced"]; found {
		t.Errorf("the adopted brain was reported as unreachable: %+v", finding)
	}
}

// `runtimes:` decides which adapters exist. An unrecognised value — a typo for
// `claude`, or a runtime vat does not support — silently produces none, and
// every other rule passes: no adapter means no drift, the description is
// present so role-metadata passes, and a bare model binds to nothing so
// model-ambiguous passes too. The definition sits on disk, inert, while the
// whole report says the harness is healthy.
func TestARoleTargetingARuntimeThatGeneratesNothingIsReported(t *testing.T) {
	// Arrange
	ws := fixture(t)
	dir := filepath.Join(ws.Root, ".agents", "roles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	role := "---\nname: planner\ndescription: Plans things.\nruntimes: [claude-code]\nmodel: opus\n---\n\n# Planner\n"
	if err := os.WriteFile(filepath.Join(dir, "planner.md"), []byte(role), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["harness/runtime-unknown"]
	if !found {
		t.Fatalf("a role that generates no adapter went unreported: %+v", report.Findings)
	}
	if !strings.Contains(finding.Message, "claude-code") {
		t.Errorf("the finding does not name the bad value: %q", finding.Message)
	}
}

// The rule was added and documented, and nothing exercised it end to end. A
// finding nobody has seen produced is a finding whose Rule, Severity, or
// Subject may be wrong in a way no reader will notice until they need it.
func TestAnUnreadableDefinitionIsReportedByLint(t *testing.T) {
	// Arrange
	ws := fixture(t)
	dir := filepath.Join(ws.Root, ".agents", "roles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.md"),
		[]byte("---\nname: [not valid\n  bad: :\n---\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["harness/definition-malformed"]
	if !found {
		t.Fatalf("an unreadable role went unreported: %+v", report.Findings)
	}
	if finding.Severity != lint.SeverityError {
		t.Errorf("severity = %s, want error: a definition nobody can read renders no adapter", finding.Severity)
	}
	if want := ".agents/roles/broken.md"; finding.Subject != want {
		t.Errorf("subject = %q, want %q", finding.Subject, want)
	}
	if finding.Message == "" {
		t.Error("the finding does not say what is wrong with the file")
	}
}

// The sound definitions beside it must still be checked, or one bad file
// silences every other harness rule in the workspace.
func TestAnUnreadableDefinitionDoesNotSilenceTheOtherHarnessRules(t *testing.T) {
	// Arrange
	ws := fixture(t)
	dir := filepath.Join(ws.Root, ".agents", "roles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	for name, body := range map[string]string{
		"broken.md":   "---\nname: [not valid\n  bad: :\n---\n",
		"nameless.md": "---\nname: nameless\n---\n\n# Nameless\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Act
	report := run(t, ws)

	// Assert
	found := rules(report)
	if _, ok := found["harness/definition-malformed"]; !ok {
		t.Errorf("the unreadable file went unreported: %+v", report.Findings)
	}
	if _, ok := found["harness/role-metadata"]; !ok {
		t.Errorf("a sound role's missing description was not reported beside it: %+v", report.Findings)
	}
}

// Codex is spelled correctly, is a runtime vat supports, and generates a role
// adapter — so checking a skill against the role list finds nothing wrong with
// it. No skill adapter is written for Codex, though, so the definition selects
// nothing that exists: the exact state this rule is documented as catching,
// reached through a value that is not a typo.
func TestASkillTargetingARuntimeWithNoSkillAdapterIsReported(t *testing.T) {
	// Arrange
	ws := fixture(t)
	dir := filepath.Join(ws.Root, ".agents", "skills", "codex-only")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	skill := "---\nname: codex-only\ndescription: A procedure only Codex should discover.\nruntimes: [codex]\n---\n\n# Codex only\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["harness/runtime-unknown"]
	if !found {
		t.Fatalf("a skill that generates no adapter went unreported: %+v", report.Findings)
	}
	if finding.Subject != "codex-only" {
		t.Errorf("subject = %q, want the skill's own name", finding.Subject)
	}
	if !strings.Contains(finding.Message, "codex") {
		t.Errorf("the finding does not name the value that selects nothing: %q", finding.Message)
	}
	// Reading "no adapter" when a role adapter for codex plainly exists sends
	// the reader to look for a bug that is not there.
	if !strings.Contains(finding.Message, "skill adapter") {
		t.Errorf("the finding does not say which kind of adapter is missing: %q", finding.Message)
	}
}

// The counterpart, so the fix cannot be a rule that reports every codex it
// sees: on a role, codex is exactly right and must stay silent.
func TestARoleTargetingCodexIsNotReported(t *testing.T) {
	// Arrange
	ws := fixture(t)
	dir := filepath.Join(ws.Root, ".agents", "roles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	role := "---\nname: planner\ndescription: Plans things.\nruntimes: [codex]\nmodel: gpt-5.6-sol\n---\n\n# Planner\n"
	if err := os.WriteFile(filepath.Join(dir, "planner.md"), []byte(role), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	if finding, found := rules(report)["harness/runtime-unknown"]; found {
		t.Errorf("a role targeting codex was reported, and codex is what it should target: %q", finding.Message)
	}
}

// The fix line is a command somebody pastes. For a repository the manifest
// records as having no remote, it handed back the placeholder as though it were
// a URL — `git remote add origin local:scratch` creates a remote that can never
// be fetched, which is the state the rule was reporting in the first place.
func TestTheRemoteMissingFixDoesNotHandBackThePlaceholderAsAUrl(t *testing.T) {
	// Arrange
	ws := fixture(t, manifest.Repo{
		Name: "scratch", Origin: manifest.LocalOrigin("scratch"), Role: manifest.RoleProduct,
	})
	git(t, filepath.Join(ws.Root, "scratch"), "remote", "remove", "origin")

	// Act
	finding, found := rules(run(t, ws))["repo/remote-missing"]

	// Assert
	if !found {
		t.Fatal("the rule did not fire")
	}
	if strings.Contains(finding.Fix, manifest.LocalOrigin("scratch")) {
		t.Errorf("the fix tells the reader to configure the placeholder: %q", finding.Fix)
	}
	if !strings.Contains(finding.Fix, "<url>") {
		t.Errorf("the fix does not say a real url is needed: %q", finding.Fix)
	}
}
