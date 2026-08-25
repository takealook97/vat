package workspace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

func newWorkspace(t *testing.T, repos ...manifest.Repo) *workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	built := manifest.Default("acme")
	for _, repo := range repos {
		built = manifest.WithRepo(built, repo)
	}
	if err := manifest.Save(filepath.Join(root, manifest.FileName), built); err != nil {
		t.Fatalf("Save returned an error: %v", err)
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		t.Fatalf("OpenAt returned an error: %v", err)
	}
	return ws
}

func TestSyncGitignorePreservesHandWrittenRulesAroundTheManagedRegion(t *testing.T) {
	// Arrange
	ws := newWorkspace(t, manifest.Repo{Name: "payments", Origin: "u", Role: manifest.RoleProduct})
	path := ws.GitignorePath()
	if err := os.WriteFile(path, []byte("# mine\n*.log\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	if _, err := ws.SyncGitignore(ws.Manifest); err != nil {
		t.Fatalf("SyncGitignore returned an error: %v", err)
	}

	// Assert
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "*.log") {
		t.Error("hand-written rules were destroyed")
	}
	if !strings.Contains(text, "payments/") {
		t.Error("the governed repository is not excluded")
	}
}

func TestSyncGitignoreIsIdempotent(t *testing.T) {
	// Arrange
	ws := newWorkspace(t, manifest.Repo{Name: "payments", Origin: "u", Role: manifest.RoleProduct})
	if _, err := ws.SyncGitignore(ws.Manifest); err != nil {
		t.Fatalf("first SyncGitignore returned an error: %v", err)
	}

	// Act
	changed, err := ws.SyncGitignore(ws.Manifest)

	// Assert
	if err != nil {
		t.Fatalf("second SyncGitignore returned an error: %v", err)
	}
	if changed {
		t.Error("a second run rewrote an already-correct file")
	}
}

func TestGitignoreDriftNamesTheRepositoryAWorkspaceCommitWouldSwallow(t *testing.T) {
	// Arrange: the managed region was written before "console" was added.
	ws := newWorkspace(t, manifest.Repo{Name: "payments", Origin: "u", Role: manifest.RoleProduct})
	if _, err := ws.SyncGitignore(ws.Manifest); err != nil {
		t.Fatalf("SyncGitignore returned an error: %v", err)
	}
	extended := manifest.WithRepo(ws.Manifest,
		manifest.Repo{Name: "console", Origin: "u", Role: manifest.RoleProduct})

	// Act
	missing, err := ws.GitignoreDrift(extended)

	// Assert
	if err != nil {
		t.Fatalf("GitignoreDrift returned an error: %v", err)
	}
	if len(missing) != 1 || missing[0] != "console" {
		t.Errorf("drift = %v, want [console]", missing)
	}
}

func TestFindWalksUpwardToTheWorkspaceRoot(t *testing.T) {
	// Arrange
	ws := newWorkspace(t)
	nested := filepath.Join(ws.Root, "payments", "src", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Act
	found, err := workspace.Find(nested)

	// Assert
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	if found != ws.Root {
		t.Errorf("Find = %q, want %q", found, ws.Root)
	}
}

func TestFindReportsWhenThereIsNoWorkspaceAbove(t *testing.T) {
	// Arrange
	t.Setenv(workspace.EnvRoot, "")
	isolated := t.TempDir()

	// Act
	_, err := workspace.Find(isolated)

	// Assert
	if err == nil {
		t.Fatal("Find succeeded outside any workspace")
	}
}
