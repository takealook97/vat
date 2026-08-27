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

func TestPathCannotResolveOutsideTheWorkspace(t *testing.T) {
	// Arrange: the manifest already rejects an escaping path, but this is the
	// function every destructive operation resolves through, so it contains the
	// result even for input that bypassed validation.
	ws := newWorkspace(t)

	escapes := []string{"../outside", "sub/../../outside", "/etc/passwd", "../../.."}

	for _, path := range escapes {
		// Act
		resolved := ws.Path(path)

		// Assert
		relative, err := filepath.Rel(ws.Root, resolved)
		if err != nil {
			t.Errorf("Path(%q) = %q, which is not under the root: %v", path, resolved, err)
			continue
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Errorf("Path(%q) escaped the workspace: %q", path, resolved)
		}
	}
}

func TestRepoPathContainsAnEscapingRepositoryDirectory(t *testing.T) {
	// Arrange: the manifest refuses to save such a path, so it is constructed
	// directly. That is the point of this test — the second line of defence has
	// to hold for a value that never went through validation.
	ws := newWorkspace(t)
	repo := manifest.Repo{
		Name: "payments", Origin: "u", Role: manifest.RoleProduct, Path: "../../escaped",
	}

	// Act
	resolved := ws.RepoPath(repo)

	// Assert
	if !strings.HasPrefix(resolved, ws.Root+string(filepath.Separator)) {
		t.Errorf("RepoPath escaped the workspace: %q is not under %q", resolved, ws.Root)
	}
}

func TestTheManagedRegionReadsAsEnglish(t *testing.T) {
	// Arrange: this text is committed into every workspace's .gitignore, so a
	// typo in it is a typo in every user's repository. It said "vat'''s".
	ws := newWorkspace(t, productRepo("payments"))

	// Act
	region := workspace.RenderGitignoreRegion(ws.Manifest)

	// Assert
	if strings.Contains(region, "'''") {
		t.Errorf("the generated region contains stray apostrophes:\n%s", region)
	}
	if !strings.Contains(region, "vat's own derived") {
		t.Errorf("the note about vat's own state is missing or misspelt:\n%s", region)
	}
}

func TestSyncGitignoreKeepsWhatWasThereBefore(t *testing.T) {
	// Arrange: a workspace root usually has a .gitignore before vat sees it,
	// and swallowing those lines would be the silent modification this tool
	// refuses to make.
	ws := newWorkspace(t, productRepo("payments"))
	existing := "# mine\nnode_modules/\n*.log\n"
	if err := os.WriteFile(ws.GitignorePath(), []byte(existing), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	if _, err := ws.SyncGitignore(ws.Manifest); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Assert
	written, err := os.ReadFile(ws.GitignorePath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, line := range []string{"# mine", "node_modules/", "*.log"} {
		if !strings.Contains(string(written), line) {
			t.Errorf("the hand-written line %q was lost:\n%s", line, written)
		}
	}
	if !strings.Contains(string(written), "payments/") {
		t.Errorf("the governed repository was not excluded:\n%s", written)
	}
}

// A workspace that governs nothing is what somebody adopting the harness for a
// single repository ends up with, and this text is committed into their
// repository. It promised a list — "Every repository below" — and then listed
// none, which reads as a bug in the generated file rather than as the state it
// actually describes.
func TestTheManagedRegionDoesNotPromiseAListItHasNot(t *testing.T) {
	// Arrange
	empty := manifest.Manifest{Version: 1}

	// Act
	region := workspace.RenderGitignoreRegion(empty)

	// Assert
	if strings.Contains(region, "Every repository below") {
		t.Errorf("the region announces repositories it does not list:\n%s", region)
	}
	if !strings.Contains(region, ".vat/") {
		t.Errorf("the region stopped excluding vat's own local state:\n%s", region)
	}
}

// The sentence exists for the case it was written for, and must survive.
func TestTheManagedRegionStillExplainsWhyItExcludesTheRepositories(t *testing.T) {
	// Arrange
	governed := manifest.Manifest{Version: 1, Repos: []manifest.Repo{
		{Name: "payments", Origin: "https://example.invalid/acme/payments.git"},
	}}

	// Act
	region := workspace.RenderGitignoreRegion(governed)

	// Assert
	if !strings.Contains(region, "independent git repository") {
		t.Errorf("the region no longer says why the directories are excluded:\n%s", region)
	}
	if !strings.Contains(region, "payments/") {
		t.Errorf("the governed repository is not excluded:\n%s", region)
	}
}

// Under git's default on Windows this file comes back with CRLF, and an exact
// comparison had every command that touches the manifest rewrite it and report
// ".gitignore updated" — on a file nobody had changed.
func TestSyncingTheGitignoreIsIdempotentAcrossLineEndings(t *testing.T) {
	// Arrange
	root := t.TempDir()
	m := manifest.Default("acme")
	m = manifest.WithRepo(m, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct,
	})
	if err := manifest.Save(filepath.Join(root, manifest.FileName), m); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	if _, err := ws.SyncGitignore(m); err != nil {
		t.Fatalf("SyncGitignore: %v", err)
	}
	path := ws.GitignorePath()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(content), "\n", "\r\n")), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	changed, err := ws.SyncGitignore(m)

	// Assert
	if err != nil {
		t.Fatalf("SyncGitignore: %v", err)
	}
	if changed {
		t.Error("the managed region was rewritten for its line endings")
	}
}

// A second managed region is a frozen copy of an old roster that vat never
// updates again, and in a .gitignore the *last* matching pattern decides. So
// `vat repo remove` reports success, drops the directory from the region it
// maintains, and the abandoned copy below it keeps the tree invisible to git.
func TestCountGitignoreRegionsSeesTheCopyVatWillNeverUpdate(t *testing.T) {
	// Arrange
	one := workspace.RenderGitignoreRegion(manifest.Manifest{})
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"no region", "build/\n", 0},
		{"one region", "build/\n" + one + "\n", 1},
		{"duplicated", one + "\n" + one + "\n", 2},
		{"duplicated with prose between", one + "\n# mine\n" + one + "\n", 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := workspace.CountGitignoreRegions(tc.content)

			// Assert
			if got != tc.want {
				t.Errorf("counted %d regions, want %d", got, tc.want)
			}
		})
	}
}
