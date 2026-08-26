package workspace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

// Every path this package hands out is one some command is about to write to,
// so a wrong answer here is a file in the wrong repository. The root is also
// what stops a command escaping the workspace, which makes locating it a safety
// property rather than a convenience.

func productRepo(name string) manifest.Repo {
	return manifest.Repo{
		Name: name, Origin: "https://example.invalid/acme/" + name + ".git",
		Role: manifest.RoleProduct,
	}
}

func TestOpenFindsTheRootFromASubdirectory(t *testing.T) {
	// Arrange: commands are run from wherever someone happens to be standing,
	// and every one of them has to agree on which workspace that is.
	ws := newWorkspace(t, productRepo("payments"))
	nested := filepath.Join(ws.Root, "payments", "internal", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Act
	found, err := workspace.Open(nested)

	// Assert
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if found.Root != ws.Root {
		t.Errorf("opening from a subdirectory found %q, want %q", found.Root, ws.Root)
	}
}

func TestOpenRefusesADirectoryThatIsInNoWorkspace(t *testing.T) {
	// Arrange: guessing a root would let a command write into a directory
	// nobody enrolled.

	// Act
	_, err := workspace.Open(t.TempDir())

	// Assert
	if err == nil {
		t.Error("a directory outside any workspace opened successfully")
	}
}

func TestFindReportsTheSameRootOpenAtWasGiven(t *testing.T) {
	// Arrange: the two are used interchangeably by different commands, so them
	// disagreeing would stay invisible until one wrote somewhere wrong.
	ws := newWorkspace(t, productRepo("payments"))

	// Act
	found, err := workspace.Find(ws.Root)

	// Assert
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found != ws.Root {
		t.Errorf("Find reported %q and OpenAt reported %q", found, ws.Root)
	}
}

func TestTheOwnedPathsAllSitInsideTheRoot(t *testing.T) {
	// Arrange: these are the directories vat writes to. One resolving outside
	// the root is the bug that turns a cleanup into data loss.
	ws := newWorkspace(t, productRepo("payments"))

	// Act & Assert
	for name, path := range map[string]string{
		"manifest":   ws.ManifestPath(),
		"state":      ws.StateDir(),
		"changesets": ws.ChangesetsDir(),
		"evidence":   ws.EvidenceDir(),
		"gitignore":  ws.GitignorePath(),
	} {
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(ws.Root)) {
			t.Errorf("the %s path %q is outside the workspace root %q", name, path, ws.Root)
		}
	}
}

func TestRelRendersAPathTheWayItIsPrinted(t *testing.T) {
	// Arrange: every message that names a file names it relative to the root,
	// because an absolute path in a shared log leaks whose machine it came from.
	ws := newWorkspace(t, productRepo("payments"))

	// Act
	relative := ws.Rel(ws.RepoPath(productRepo("payments")))

	// Assert
	if filepath.IsAbs(relative) {
		t.Errorf("Rel returned the absolute path %q", relative)
	}
	if relative != "payments" {
		t.Errorf("Rel returned %q, want payments", relative)
	}
}

func TestBrainPathIsReportedOnlyWhenOneIsDeclared(t *testing.T) {
	// Arrange: the knowledge commands decide whether to run at all from this
	// answer, so a confident guess would create records in a product repository.
	without := newWorkspace(t, productRepo("payments"))
	with := newWorkspace(t, productRepo("payments"), manifest.Repo{
		Name: "knowledge", Origin: "https://example.invalid/acme/knowledge.git",
		Role: manifest.RoleBrain,
	})

	// Act
	_, hasNone := without.BrainPath()
	path, hasOne := with.BrainPath()

	// Assert
	if hasNone {
		t.Error("a workspace with no brain repository reported a path for one")
	}
	if !hasOne {
		t.Fatal("a workspace declaring a brain repository reported no path")
	}
	if filepath.Base(path) != "knowledge" {
		t.Errorf("the brain path is %q, want the declared repository", path)
	}
}

func TestExistsReportsWhatIsActuallyOnDisk(t *testing.T) {
	// Arrange: a manifest entry with no clone is the normal state of a fresh
	// machine, and treating it as present would make every command act on a
	// directory that is not there.
	ws := newWorkspace(t, productRepo("payments"))
	cloned := productRepo("payments")
	if err := os.MkdirAll(ws.RepoPath(cloned), 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Act & Assert
	if !ws.Exists(cloned) {
		t.Error("a repository directory that exists is reported as missing")
	}
	if ws.Exists(productRepo("never-cloned")) {
		t.Error("a repository that was never cloned is reported as present")
	}
}

func TestSaveManifestIsReadBackByTheNextOpen(t *testing.T) {
	// Arrange: every command that changes the workspace writes through here, and
	// a write the next process cannot see is the same as no write at all.
	ws := newWorkspace(t, productRepo("payments"))

	// Act
	if err := ws.SaveManifest(manifest.WithRepo(ws.Manifest, productRepo("console"))); err != nil {
		t.Fatalf("save: %v", err)
	}
	reopened, err := workspace.OpenAt(ws.Root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	// Assert
	if _, exists := reopened.Manifest.Find("console"); !exists {
		t.Error("a repository saved to the manifest is not there when it is read back")
	}
}

func TestSelectRefusesAFilterThatMatchesNothing(t *testing.T) {
	// Arrange: an empty run in CI is a green build that did nothing.
	ws := newWorkspace(t, productRepo("payments"))

	// Act
	_, err := ws.Select(manifest.Selector{Groups: []string{"no-such-group"}})

	// Assert
	if err == nil {
		t.Error("selecting a group that matches nothing reported success")
	}
}
