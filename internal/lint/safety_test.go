package lint_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/manifest"
)

// The rules exercised here were each a shipped defect before they were a rule.
// v0.1.5 wrote a credential into .git/config and v0.1.2 through v0.1.4 created
// directories outside the workspace; both were fixed at the commands that cause
// them, which does nothing for a workspace those versions already built. A
// guard at the entry point protects the next invocation. A lint rule is what
// examines the state that is already on disk.

const tokenBearingRemote = "https://x-token:ghp_EXAMPLETOKEN@example.invalid/acme/payments.git"

func payments() manifest.Repo {
	return manifest.Repo{
		Name:   "payments",
		Origin: "https://example.invalid/acme/payments.git",
		Role:   manifest.RoleProduct,
	}
}

func TestACredentialLeftInACloneRemoteIsAnError(t *testing.T) {
	// Arrange: the manifest holds the plain URL — it has always refused an
	// embedded credential — while the clone's own remote carries the token,
	// which is exactly the state v0.1.5 left behind.
	ws := fixture(t, payments())
	git(t, filepath.Join(ws.Root, "payments"), "remote", "set-url", "origin", tokenBearingRemote)

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["repo/credential-in-remote"]
	if !found {
		t.Fatalf("a token in .git/config was not reported: %+v", report.Findings)
	}
	if finding.Severity != lint.SeverityError {
		t.Errorf("severity = %q, want error", finding.Severity)
	}
	if finding.Subject != "payments" {
		t.Errorf("subject = %q, want the repository name", finding.Subject)
	}
	if finding.Fixable {
		t.Error("the finding is marked fixable; vat does not rewrite a remote")
	}
}

func TestACredentialInARemoteIsInvisibleToTheMismatchRule(t *testing.T) {
	// Arrange: this is why the rule has to exist separately. NormaliseURL
	// strips userinfo before comparing, so a remote carrying a token compares
	// *equal* to the plain manifest origin and the mismatch rule — the only
	// rule that looked at the configured remote at all — reported nothing.
	ws := fixture(t, payments())
	git(t, filepath.Join(ws.Root, "payments"), "remote", "set-url", "origin", tokenBearingRemote)

	// Act
	report := run(t, ws)

	// Assert
	if _, mismatched := rules(report)["repo/remote-mismatch"]; mismatched {
		t.Error("the mismatch rule fired; this test no longer proves the gap it was written for")
	}
	if _, found := rules(report)["repo/credential-in-remote"]; !found {
		t.Error("nothing at all reported a token sitting in .git/config")
	}
}

func TestNoFindingEverPrintsTheCredentialItFound(t *testing.T) {
	// Arrange: "never print a secret" is the rule most easily broken by a
	// helpful error message, so the whole report is searched rather than the
	// one finding that was expected to mention it.
	ws := fixture(t, payments())
	git(t, filepath.Join(ws.Root, "payments"), "remote", "set-url", "origin", tokenBearingRemote)

	// Act
	report := run(t, ws)

	// Assert
	for _, finding := range report.Findings {
		whole := strings.Join([]string{finding.Subject, finding.Message, finding.Fix}, " ")
		if strings.Contains(whole, "ghp_EXAMPLETOKEN") {
			t.Errorf("finding %q disclosed the credential: %s", finding.Rule, whole)
		}
		if strings.Contains(whole, "x-token:") {
			t.Errorf("finding %q disclosed the credential user: %s", finding.Rule, whole)
		}
	}
}

func TestAPlainRemoteIsNeverReportedAsCarryingACredential(t *testing.T) {
	// Arrange
	ws := fixture(t, payments())

	// Act
	report := run(t, ws)

	// Assert
	if finding, found := rules(report)["repo/credential-in-remote"]; found {
		t.Errorf("a credential-free remote was reported: %s", finding.Message)
	}
}

func TestAGovernedDirectoryResolvingOutsideTheWorkspaceIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink on Windows needs a privilege the test cannot assume")
	}
	// Arrange: a symlink satisfies the manifest's textual containment and the
	// re-rooted join in RepoPath. Every contract vat renders and every
	// directory it deletes through this entry would land outside the workspace.
	ws := fixture(t, payments())
	inside := filepath.Join(ws.Root, "payments")
	outside := t.TempDir()
	if err := os.RemoveAll(inside); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := os.Symlink(outside, inside); err != nil {
		t.Skipf("symlinks are unavailable here: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	finding, found := rules(report)["repo/outside-workspace"]
	if !found {
		t.Fatalf("an escaping directory was not reported: %+v", report.Findings)
	}
	if finding.Severity != lint.SeverityError {
		t.Errorf("severity = %q, want error", finding.Severity)
	}
	if finding.Fixable {
		t.Error("the finding is marked fixable; where a repository lives is a decision")
	}
}

func TestARepositoryThatReallyIsInsideIsNeverReportedAsOutside(t *testing.T) {
	// Arrange
	ws := fixture(t, payments())

	// Act
	report := run(t, ws)

	// Assert
	if finding, found := rules(report)["repo/outside-workspace"]; found {
		t.Errorf("a contained repository was reported as escaping: %s", finding.Message)
	}
}
