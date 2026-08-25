package doctor_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/doctor"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

var reference = time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, output)
	}
}

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
	if _, err := ws.SyncGitignore(ws.Manifest); err != nil {
		t.Fatalf("SyncGitignore: %v", err)
	}
	return ws
}

func run(t *testing.T, ws *workspace.Workspace) doctor.Report {
	t.Helper()
	return doctor.Run(context.Background(), ws, doctor.Options{Now: reference})
}

func find(report doctor.Report, section, subject string) (doctor.Finding, bool) {
	for _, finding := range report.Findings {
		if finding.Section == section && finding.Subject == subject {
			return finding, true
		}
	}
	return doctor.Finding{}, false
}

func TestAHealthyWorkspaceReportsNoFailures(t *testing.T) {
	// Arrange
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "https://example.com/acme/payments.git",
		Role: manifest.RoleProduct,
	})

	// Act
	report := run(t, ws)

	// Assert
	if report.Failures != 0 {
		for _, finding := range report.Findings {
			if finding.Status == doctor.StatusFail {
				t.Errorf("unexpected failure: %s/%s — %s", finding.Section, finding.Subject, finding.Detail)
			}
		}
	}
}

func TestAMissingRequiredRepositoryFails(t *testing.T) {
	// Arrange
	ws := fixture(t)
	ws.Manifest = manifest.WithRepo(ws.Manifest, manifest.Repo{
		Name: "ghost", Origin: "u", Role: manifest.RoleProduct, Required: true,
	})

	// Act
	report := run(t, ws)

	// Assert
	finding, found := find(report, "repositories", "ghost")
	if !found || finding.Status != doctor.StatusFail {
		t.Errorf("a missing required repository was not reported as a failure: %+v", finding)
	}
}

func TestAnOriginPointingElsewhereFails(t *testing.T) {
	// Arrange
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "https://example.com/acme/payments.git",
		Role: manifest.RoleProduct,
	})
	git(t, ws.RepoPath(manifest.Repo{Name: "payments"}),
		"remote", "set-url", "origin", "https://evil.example/acme/payments.git")

	// Act
	report := run(t, ws)

	// Assert
	finding, found := find(report, "repositories", "payments")
	if !found || finding.Status != doctor.StatusFail {
		t.Fatalf("a redirected origin was not reported as a failure: %+v", finding)
	}
	if !strings.Contains(finding.Detail, "evil.example") {
		t.Errorf("the finding does not name where origin actually points: %q", finding.Detail)
	}
}

func TestAMismatchReportNeverPrintsACredential(t *testing.T) {
	// Arrange: the mismatch report shows both URLs, and must not become the one
	// place a token is disclosed.
	const secret = "ghp_SUPERSECRETVALUE"
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "https://example.com/acme/payments.git",
		Role: manifest.RoleProduct,
	})
	git(t, ws.RepoPath(manifest.Repo{Name: "payments"}), "remote", "set-url", "origin",
		"https://x-token:"+secret+"@evil.example/acme/payments.git")

	// Act
	report := run(t, ws)

	// Assert
	for _, finding := range report.Findings {
		if strings.Contains(finding.Detail, secret) {
			t.Fatalf("doctor printed a credential: %q", finding.Detail)
		}
	}
}

func TestGitignoreDriftFails(t *testing.T) {
	// Arrange: a governed repository missing from .gitignore means the next
	// root commit swallows the whole clone.
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "https://example.com/acme/payments.git",
		Role: manifest.RoleProduct,
	})
	if err := os.WriteFile(ws.GitignorePath(), []byte("# emptied\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	finding, found := find(report, "workspace", ".gitignore")
	if !found || finding.Status != doctor.StatusFail {
		t.Errorf("gitignore drift was not reported as a failure: %+v", finding)
	}
}

func TestACredentialRepositoryHoldingPlaintextFails(t *testing.T) {
	// Arrange
	ws := fixture(t, manifest.Repo{
		Name: "credential", Origin: "https://example.com/acme/credential.git",
		Role: manifest.RoleCredential,
	})
	dir := ws.RepoPath(manifest.Repo{Name: "credential"})
	if err := os.WriteFile(filepath.Join(dir, ".env.production"), []byte("TOKEN=abc\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	report := run(t, ws)

	// Assert
	finding, found := find(report, "credentials", "credential")
	if !found || finding.Status != doctor.StatusFail {
		t.Fatalf("plaintext in a credential repository was not reported: %+v", finding)
	}
	if strings.Contains(finding.Detail, "TOKEN=abc") {
		t.Errorf("doctor printed the file's contents: %q", finding.Detail)
	}
}

func TestDoctorNeverRepairsWhatItFinds(t *testing.T) {
	// Arrange: a tool that silently fixes what it finds teaches nothing about
	// why it broke, and on a machine holding unpushed work, fixing loses data.
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "https://example.com/acme/payments.git",
		Role: manifest.RoleProduct,
	})
	if err := os.WriteFile(ws.GitignorePath(), []byte("# emptied\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	run(t, ws)

	// Assert
	after, err := os.ReadFile(ws.GitignorePath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != "# emptied\n" {
		t.Errorf("doctor modified .gitignore: %q", after)
	}
}
