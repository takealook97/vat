package workspace_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

// Eight `vat repo add` calls started together left a manifest holding two of
// them. Every one reported success. Each read the file, added its entry to what
// it had read, and wrote the whole thing back, so six additions were overwritten
// by a command that had never seen them — silently, which is the one outcome
// this tool exists to prevent.
//
// The refusal, not a merge: a state that cannot be resolved safely is reported.
func TestSavingAManifestSomebodyElseChangedIsRefused(t *testing.T) {
	// Arrange
	root := t.TempDir()
	first := manifest.Default("acme")
	if err := manifest.Save(filepath.Join(root, manifest.FileName), first); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}

	// Act: somebody else writes between this workspace being read and saved.
	elsewhere := manifest.WithRepo(first, manifest.Repo{
		Name: "theirs", Origin: "https://example.invalid/acme/theirs.git",
		Role: manifest.RoleProduct,
	})
	if err := manifest.Save(filepath.Join(root, manifest.FileName), elsewhere); err != nil {
		t.Fatalf("concurrent save: %v", err)
	}
	mine := manifest.WithRepo(ws.Manifest, manifest.Repo{
		Name: "mine", Origin: "https://example.invalid/acme/mine.git",
		Role: manifest.RoleProduct,
	})
	err = ws.SaveManifest(mine)

	// Assert
	if err == nil {
		t.Fatal("a manifest was overwritten on top of a change this command never saw")
	}
	if !strings.Contains(err.Error(), "changed") {
		t.Errorf("the refusal does not say what happened: %v", err)
	}
	after, err := manifest.Load(filepath.Join(root, manifest.FileName))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if _, kept := after.Find("theirs"); !kept {
		t.Error("the other command's entry was destroyed")
	}
	if _, wrote := after.Find("mine"); wrote {
		t.Error("the refused save was written anyway")
	}
}

// The ordinary path must not have become harder.
func TestSavingAManifestNobodyElseTouchedSucceeds(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if err := manifest.Save(filepath.Join(root, manifest.FileName), manifest.Default("acme")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}

	// Act
	next := manifest.WithRepo(ws.Manifest, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct,
	})
	if err := ws.SaveManifest(next); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Assert: and again, because saving must not invalidate the workspace it
	// was called on — `vat repo new` saves and then renders from the same one.
	if err := ws.SaveManifest(next); err != nil {
		t.Errorf("a second save from the same workspace was refused: %v", err)
	}
}

// The version comparison narrows the window; it does not close it. Two commands
// can both read the old file before either writes, and both then pass a check
// that was true when they made it — which is how eight concurrent calls left two
// entries while every check passed. The lock closes it.
func TestConcurrentSavesLoseNothingAndSayWhichWereRefused(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if err := manifest.Save(filepath.Join(root, manifest.FileName), manifest.Default("acme")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Act: every writer opens before any of them saves, which is the shape that
	// loses work.
	const writers = 8
	spaces := make([]*workspace.Workspace, writers)
	for i := range spaces {
		ws, err := workspace.OpenAt(root)
		if err != nil {
			t.Fatalf("OpenAt: %v", err)
		}
		spaces[i] = ws
	}
	var wg sync.WaitGroup
	results := make([]error, writers)
	for i, ws := range spaces {
		wg.Add(1)
		go func(index int, ws *workspace.Workspace) {
			defer wg.Done()
			results[index] = ws.SaveManifest(manifest.WithRepo(ws.Manifest, manifest.Repo{
				Name:   fmt.Sprintf("svc%d", index),
				Origin: fmt.Sprintf("https://example.invalid/acme/svc%d.git", index),
				Role:   manifest.RoleProduct,
			}))
		}(i, ws)
	}
	wg.Wait()

	// Assert
	var wrote int
	for index, err := range results {
		if err == nil {
			wrote++
			continue
		}
		if !strings.Contains(err.Error(), "changed") {
			t.Errorf("writer %d failed for a reason other than the conflict: %v", index, err)
		}
	}
	if wrote == 0 {
		t.Fatal("every writer was refused; none of them made progress")
	}
	after, err := manifest.Load(filepath.Join(root, manifest.FileName))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	// Whatever landed is exactly what the writers that were told they succeeded
	// wrote. Nothing was accepted and then overwritten.
	if len(after.Repos) != wrote {
		t.Errorf("%d writers were told they succeeded and the manifest holds %d entries",
			wrote, len(after.Repos))
	}
}

// The lock is held for one read and one write — milliseconds. A process killed
// inside that window used to leave a file that made every later command wait
// five seconds and then refuse, and the only repair was knowing to delete it.
// A workspace bricked by a Ctrl-C is a worse failure than the one the lock
// prevents.
func TestALockLeftBySomethingThatDiedIsNotPermanent(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if err := manifest.Save(filepath.Join(root, manifest.FileName), manifest.Default("acme")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	lock := filepath.Join(root, ".vat", "manifest.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	// Older than anything a read and a write could take.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatalf("age the lock: %v", err)
	}

	// Act
	err = ws.SaveManifest(manifest.WithRepo(ws.Manifest, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct,
	}))

	// Assert
	if err != nil {
		t.Fatalf("a lock left behind an hour ago blocked a save: %v", err)
	}
	if _, err := os.Stat(lock); err == nil {
		t.Error("the lock was not released")
	}
}

// A lock somebody is actually holding is still waited on and then reported.
func TestALockSomebodyIsHoldingIsWaitedOnAndThenReported(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if err := manifest.Save(filepath.Join(root, manifest.FileName), manifest.Default("acme")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	lock := filepath.Join(root, ".vat", "manifest.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	// Act
	err = ws.SaveManifest(ws.Manifest)

	// Assert
	if err == nil {
		t.Fatal("a save went through a lock somebody is holding")
	}
	if !strings.Contains(err.Error(), "another vat command") {
		t.Errorf("the refusal does not say what is in the way: %v", err)
	}
}
