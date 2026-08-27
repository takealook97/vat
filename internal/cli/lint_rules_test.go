package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every rule in this table is one that had been written down somewhere and was
// checked by nothing until it became a rule. The table asserts the check fires
// on the state it was written for — a rule that never fires is indistinguishable
// from a rule that was never added.

type lintFinding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Subject  string `json:"subject"`
	Message  string `json:"message"`
}

type lintReport struct {
	Findings []lintFinding `json:"findings"`
	Checked  int           `json:"rules_checked"`
}

func (h *workspaceFixture) lint(t *testing.T, args ...string) lintReport {
	t.Helper()
	var report lintReport
	h.runJSON(&report, append([]string{"lint"}, args...)...)
	return report
}

func (r lintReport) reports(rule string) bool {
	for _, finding := range r.Findings {
		if finding.Rule == rule {
			return true
		}
	}
	return false
}

func (r lintReport) rules() []string {
	seen := make([]string, 0, len(r.Findings))
	for _, finding := range r.Findings {
		seen = append(seen, finding.Rule)
	}
	return seen
}

func TestEachLintRuleFiresOnTheStateItWasWrittenFor(t *testing.T) {
	cases := []struct {
		rule   string
		damage func(t *testing.T, h *workspaceFixture)
	}{
		{
			rule: "repo/missing",
			damage: func(t *testing.T, h *workspaceFixture) {
				if err := os.RemoveAll(h.path("payments")); err != nil {
					t.Fatalf("remove: %v", err)
				}
			},
		},
		{
			rule: "repo/not-a-repository",
			damage: func(t *testing.T, h *workspaceFixture) {
				if err := os.RemoveAll(h.path("payments", ".git")); err != nil {
					t.Fatalf("remove: %v", err)
				}
			},
		},
		{
			rule: "repo/remote-mismatch",
			damage: func(t *testing.T, h *workspaceFixture) {
				// A remote pointing somewhere else is a supply-chain signal, so
				// the rule reports it and never rewrites it.
				git(t, h.path("payments"), "remote", "set-url", "origin",
					"https://example.invalid/somewhere-else/payments.git")
			},
		},
		{
			rule: "repo/remote-missing",
			damage: func(t *testing.T, h *workspaceFixture) {
				git(t, h.path("payments"), "remote", "remove", "origin")
			},
		},
		{
			rule: "harness/repo-missing",
			damage: func(t *testing.T, h *workspaceFixture) {
				if err := os.Remove(h.path("payments", "AGENTS.md")); err != nil {
					t.Fatalf("remove: %v", err)
				}
			},
		},
		{
			rule: "harness/repo-drift",
			damage: func(t *testing.T, h *workspaceFixture) {
				overwriteGeneratedRegion(t, h.path("payments", "AGENTS.md"))
			},
		},
		{
			rule: "harness/workspace-missing",
			damage: func(t *testing.T, h *workspaceFixture) {
				if err := os.Remove(h.path("AGENTS.md")); err != nil {
					t.Fatalf("remove: %v", err)
				}
			},
		},
		{
			rule: "harness/workspace-drift",
			damage: func(t *testing.T, h *workspaceFixture) {
				overwriteGeneratedRegion(t, h.path("AGENTS.md"))
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.rule, func(t *testing.T) {
			// Arrange
			h := adoptedFixture(t, "payments")
			if h.lint(t).reports(testCase.rule) {
				t.Fatalf("%s already fires on a clean workspace, so this test proves nothing", testCase.rule)
			}

			// Act
			testCase.damage(t, h)
			report := h.lint(t)

			// Assert
			if !report.reports(testCase.rule) {
				t.Errorf("%s did not fire; lint reported %v", testCase.rule, report.rules())
			}
		})
	}
}

// overwriteGeneratedRegion replaces the body of the generated region, which is
// the edit the drift rules exist to catch: someone changing generated text by
// hand and losing it on the next render without being told.
func overwriteGeneratedRegion(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(content)
	begin := strings.Index(body, "<!-- vat:begin generated -->")
	end := strings.Index(body, "<!-- vat:end generated -->")
	if begin < 0 || end < 0 {
		t.Fatalf("%s has no generated region to drift:\n%s", path, body)
	}
	drifted := body[:begin] +
		"<!-- vat:begin generated -->\nhand-edited, which the next render would silently discard\n" +
		body[end:]
	if err := os.WriteFile(path, []byte(drifted), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLintOnlySelectsASingleRule(t *testing.T) {
	// Arrange: --only is how someone repairing one class of problem avoids
	// reading past everything else, so a rule that cannot be selected by name is
	// a rule that cannot be worked through.
	h := adoptedFixture(t, "payments")
	if err := os.RemoveAll(h.path("payments", ".git")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.Remove(h.path("payments", "AGENTS.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Act
	all := h.lint(t)
	narrowed := h.lint(t, "--only", "harness/repo-missing")

	// Assert
	if len(all.Findings) <= len(narrowed.Findings) {
		t.Errorf("--only did not narrow anything: %v vs %v", all.rules(), narrowed.rules())
	}
	for _, finding := range narrowed.Findings {
		if finding.Rule != "harness/repo-missing" {
			t.Errorf("--only harness/repo-missing also reported %s", finding.Rule)
		}
	}
}

func TestLintFixRepairsWhatItReportedAndNothingElse(t *testing.T) {
	// Arrange: repair is a separate decision from diagnosis, and a --fix that
	// touched anything it had not reported would be the silent modification
	// this tool refuses to make.
	h := adoptedFixture(t, "payments")
	if err := os.Remove(h.path("payments", "AGENTS.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	handWritten := filepath.Join(h.path("payments"), "NOTES.md")
	if err := os.WriteFile(handWritten, []byte("mine\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	h.run("lint", "--fix")

	// Assert
	if _, err := os.Stat(h.path("payments", "AGENTS.md")); err != nil {
		t.Error("--fix did not regenerate the contract it reported missing")
	}
	notes, err := os.ReadFile(handWritten)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(notes) != "mine\n" {
		t.Errorf("--fix rewrote a file it never reported: %q", notes)
	}
	if report := h.lint(t); report.reports("harness/repo-missing") {
		t.Errorf("the finding survived its own repair: %v", report.rules())
	}
}

func TestLintCountsTheRulesItActuallyChecked(t *testing.T) {
	// Arrange: the count is what tells a reader that a clean report means every
	// rule ran, rather than that no rule did.
	h := adoptedFixture(t, "payments")

	// Act
	report := h.lint(t)

	// Assert
	if report.Checked == 0 {
		t.Error("lint reported checking no rules at all")
	}
}

// A generated adapter whose definition has been deleted keeps being loaded. The
// runtime still advertises the role, the session still opens it, and the file it
// points at is gone — while `vat harness check` reported that every contract
// matched. Drift compares an adapter to its definition and cannot see the case
// where there is no definition left to compare against.
func TestAnAdapterLeftBehindByADeletedDefinitionIsReported(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	h.mustRun("harness", "role", "new", "planner", "--description", "Plans work.")
	h.mustRun("harness", "skill", "new", "deploy", "--description", "Deploys one service.")
	stale := []string{
		filepath.Join(".claude", "agents", "planner.md"),
		filepath.Join(".codex", "agents", "planner.toml"),
		filepath.Join(".claude", "skills", "deploy", "SKILL.md"),
	}
	for _, path := range stale {
		if _, err := os.Stat(h.path(strings.Split(path, string(filepath.Separator))...)); err != nil {
			t.Fatalf("the adapter this test is about was never generated: %v", err)
		}
	}
	if err := os.Remove(h.path(".agents", "roles", "planner.md")); err != nil {
		t.Fatalf("remove the role: %v", err)
	}
	if err := os.RemoveAll(h.path(".agents", "skills", "deploy")); err != nil {
		t.Fatalf("remove the skill: %v", err)
	}

	// Act
	report := h.lint(t)

	// Assert
	if !report.reports("harness/adapter-orphaned") {
		t.Fatalf("three adapters point at definitions that no longer exist and lint reported %v",
			report.rules())
	}
	reported := map[string]bool{}
	for _, finding := range report.Findings {
		if finding.Rule == "harness/adapter-orphaned" {
			reported[filepath.ToSlash(finding.Subject)] = true
		}
	}
	for _, path := range stale {
		if !reported[filepath.ToSlash(path)] {
			t.Errorf("%s was left behind and is not reported; reported %v", path, reported)
		}
	}
	if code, output := h.run("harness", "check"); code == ExitOK && !strings.Contains(output, "orphaned") {
		t.Errorf("`harness check` still says the harness is healthy:\n%s", output)
	}
}

// The rule reads the generated marker rather than the directory, so an agent
// definition somebody wrote by hand and never asked vat to manage is left alone.
// A rule that fires on a correct workspace is a rule that gets disabled.
func TestAHandWrittenAgentFileIsNotMistakenForAnOrphan(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	mine := h.path(".claude", "agents", "mine.md")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const handWritten = "---\nname: mine\ndescription: Written by me.\n---\n\nMy own agent.\n"
	if err := os.WriteFile(mine, []byte(handWritten), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	report := h.lint(t)

	// Assert
	if report.reports("harness/adapter-orphaned") {
		t.Errorf("a hand-written agent file was reported as an orphaned adapter: %+v", report.Findings)
	}
	after, err := os.ReadFile(mine)
	if err != nil || string(after) != handWritten {
		t.Errorf("the hand-written file was disturbed: %v", err)
	}
}

// `vat lint --only harness` in CI is what this project's own adoption guide
// recommends. Mistype the selector and the run reported "0 rules checked,
// nothing to report" and exited 0 — a green build that checked nothing, for as
// long as nobody looked. The selector is a substring match on purpose, so
// `--only harness` keeps working; a value that matches no rule at all is the
// case that cannot be intentional.
func TestASelectorThatMatchesNoRuleIsRefusedRatherThanReportedClean(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("lint", "--only", "harness/adaptor-drift")

	// Assert
	if code == ExitOK {
		t.Errorf("a selector matching no rule reported success:\n%s", output)
	}
	if !strings.Contains(output, "adaptor-drift") {
		t.Errorf("the refusal does not name what was passed:\n%s", output)
	}
	if !strings.Contains(output, "--list") {
		t.Errorf("the refusal does not say how to find the real names:\n%s", output)
	}
}

func TestASelectorThatMatchesAFamilyOfRulesStillRuns(t *testing.T) {
	// Arrange: the substring match is the feature, not an accident.
	h := adoptedFixture(t, "payments")

	// Act
	report := h.lint(t, "--only", "harness")

	// Assert
	if report.Checked == 0 {
		t.Error("`--only harness` selected no rules")
	}
	for _, finding := range report.Findings {
		if !strings.HasPrefix(finding.Rule, "harness/") {
			t.Errorf("`--only harness` also reported %s", finding.Rule)
		}
	}
}

// `vat brain check` refuses to judge a brain written against a newer schema,
// because "reading it silently and reporting on fields it cannot see would be
// the worst outcome: the records would look clean because half of what governs
// them was invisible". `vat lint` read it silently and reported clean — and
// `vat lint` is the command this project's own adoption guide puts in CI.
func TestLintDoesNotCertifyABrainWrittenAgainstANewerSchema(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments", "brain")
	h.mustRun("brain", "init")
	marker := h.path("brain", ".brain")
	content := readFile(t, marker)
	if err := os.WriteFile(marker,
		[]byte(strings.Replace(content, "schema: 1", "schema: 99", 1)), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	// Act
	report := h.lint(t, "--offline")

	// Assert
	if !report.reports("brain/schema-newer") {
		t.Errorf("lint certified a knowledge layer it cannot read; it reported %v", report.rules())
	}
	if report.reports("brain/generated-drift") {
		t.Error("lint judged the projections of a brain whose format it does not understand")
	}
}

// A file carrying two generated regions has one vat maintains and one it never
// looks at again. The second keeps whatever it held, marked as generated, and
// every session loads it as though vat wrote it that morning — which is the
// failure this whole layer exists to prevent, inside the file that prevents it.
func TestASecondGeneratedRegionIsReported(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	path := h.path("AGENTS.md")
	content := readFile(t, path)
	duplicate := content + "\n<!-- vat:begin generated -->\nStale, and nobody will ever regenerate it.\n<!-- vat:end generated -->\n"
	if err := os.WriteFile(path, []byte(duplicate), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	report := h.lint(t, "--offline")

	// Assert
	if !report.reports("harness/region-duplicated") {
		t.Errorf("a second generated region went unreported; lint said %v", report.rules())
	}
	// Not repaired: which of the two is the real one is a judgement, and the
	// abandoned one may be the only copy of something somebody wrote.
	h.mustRun("lint", "--fix", "--offline")
	if !strings.Contains(readFile(t, path), "Stale, and nobody will ever regenerate it.") {
		t.Error("--fix deleted a region it cannot know the meaning of")
	}
}

func TestASingleGeneratedRegionIsNotReported(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")

	// Act
	report := h.lint(t, "--offline")

	// Assert
	if report.reports("harness/region-duplicated") {
		t.Errorf("a correct contract was reported: %+v", report.Findings)
	}
}

// The same file in a repository, where a session actually opens. The rule was
// written for the workspace contract and the repositories carry the identical
// structure.
func TestASecondGeneratedRegionInARepositoryContractIsReported(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	path := h.path("payments", "AGENTS.md")
	content := readFile(t, path)
	if err := os.WriteFile(path, []byte(content+
		"\n<!-- vat:begin generated -->\nStale.\n<!-- vat:end generated -->\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	report := h.lint(t, "--offline")

	// Assert
	if !report.reports("harness/region-duplicated") {
		t.Errorf("a second region in a repository contract went unreported; lint said %v", report.rules())
	}
	for _, finding := range report.Findings {
		if finding.Rule == "harness/region-duplicated" && !strings.Contains(finding.Subject, "payments") {
			t.Errorf("the finding does not name the repository: %q", finding.Subject)
		}
	}
}

// A governed repository inside another governed repository is the harm
// `workspace/gitignore-drift` names, one level down: a commit in the outer one
// swallows the inner one's entire tree and duplicates its history, and the
// outer repository shows as permanently dirty in the meantime. The rule guards
// the workspace root and nothing guarded this.
func TestAGovernedRepositoryInsideAnotherIsReported(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	nested := h.path("payments", "inner")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initRepoAt(t, nested)
	h.mustRun("repo", "add", "inner",
		"--origin", "https://example.invalid/acme/inner.git", "--path", "payments/inner", "--no-clone")

	// Act
	report := h.lint(t, "--offline")

	// Assert
	if !report.reports("repo/nested") {
		t.Errorf("a repository inside another went unreported; lint said %v", report.rules())
	}
}

// A nested directory the outer repository already ignores is somebody who has
// thought about it, and a rule that fires on a correct workspace gets disabled.
func TestANestedRepositoryTheOuterOneIgnoresIsNotReported(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	nested := h.path("payments", "inner")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initRepoAt(t, nested)
	if err := os.WriteFile(h.path("payments", ".gitignore"), []byte("inner/\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h.mustRun("repo", "add", "inner",
		"--origin", "https://example.invalid/acme/inner.git", "--path", "payments/inner", "--no-clone")

	// Act
	report := h.lint(t, "--offline")

	// Assert
	if report.reports("repo/nested") {
		t.Errorf("a nested repository the outer one ignores was reported: %+v", report.Findings)
	}
}

func initRepoAt(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch", "main", "."},
		{"add", "-A"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
