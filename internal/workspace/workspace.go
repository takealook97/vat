// Package workspace resolves the workspace root and gives the rest of vat a
// single, validated view of it: where the manifest is, where each repository
// should live, and which files vat owns.
package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/manifest"
)

// ErrNoWorkspace is returned when no vat.yaml exists in the current directory
// or any ancestor.
var ErrNoWorkspace = errors.New("not inside a vat workspace")

// EnvRoot lets a user or a CI job pin the workspace root explicitly.
const EnvRoot = "VAT_WORKSPACE"

// Workspace is a loaded workspace: its root directory and its manifest.
type Workspace struct {
	Root     string
	Manifest manifest.Manifest
	// read is what vat.yaml held when this workspace was opened. A save
	// compares against it, because every command that changes the manifest
	// reads the whole file, edits what it read, and writes the whole file back
	// — so two of them running together leave only the later one's work, and
	// both report success. Eight `vat repo add` calls started at once left two
	// entries.
	read []byte
}

// Find walks up from start looking for a manifest and returns the directory
// holding it. VAT_WORKSPACE, when set, wins over the walk.
func Find(start string) (string, error) {
	if pinned := os.Getenv(EnvRoot); pinned != "" {
		if _, err := os.Stat(filepath.Join(pinned, manifest.FileName)); err != nil {
			return "", fmt.Errorf("%s=%s has no %s", EnvRoot, pinned, manifest.FileName)
		}
		return filepath.Clean(pinned), nil
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", start, err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w: no %s in %s or any parent directory",
				ErrNoWorkspace, manifest.FileName, start)
		}
		dir = parent
	}
}

// Open finds and loads the workspace containing start.
func Open(start string) (*Workspace, error) {
	root, err := Find(start)
	if err != nil {
		return nil, err
	}
	return OpenAt(root)
}

// OpenAt loads the workspace rooted at an exact directory.
func OpenAt(root string) (*Workspace, error) {
	path := filepath.Join(root, manifest.FileName)
	loaded, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}
	// Read again rather than threaded out of Load: what matters is the bytes on
	// disk at the moment this command decided what the workspace was.
	read, _, err := fsx.ReadFileIfExists(path)
	if err != nil {
		return nil, err
	}
	return &Workspace{Root: filepath.Clean(root), Manifest: loaded, read: read}, nil
}

// ManifestPath returns the absolute path of vat.yaml.
func (w *Workspace) ManifestPath() string {
	return filepath.Join(w.Root, manifest.FileName)
}

// RepoPath returns the absolute directory a repository should occupy, and can
// never point outside the workspace.
//
// The manifest already rejects an escaping path, but this is the function every
// destructive operation resolves through — cloning, writing a harness, deleting
// a directory. Rooting the join here means a path that somehow bypassed
// validation still cannot reach outside.
func (w *Workspace) RepoPath(repo manifest.Repo) string {
	return w.Path(repo.Dir())
}

// Path joins workspace-relative parts onto the root, containing the result.
// "../../etc" resolves to <root>/etc rather than escaping.
func (w *Workspace) Path(parts ...string) string {
	joined := filepath.Join(parts...)
	contained := filepath.Clean(string(filepath.Separator) + joined)
	return filepath.Join(w.Root, strings.TrimPrefix(contained, string(filepath.Separator)))
}

// Rel makes an absolute path readable in output by expressing it relative to
// the workspace root.
func (w *Workspace) Rel(path string) string {
	rel, err := filepath.Rel(w.Root, path)
	if err != nil {
		return path
	}
	return rel
}

// StateDir is where vat keeps derived, regenerable local state such as the
// metrics ledger. Nothing canonical is ever stored here.
func (w *Workspace) StateDir() string { return filepath.Join(w.Root, ".vat") }

// ChangesetsDir holds the multi-repository completion records.
func (w *Workspace) ChangesetsDir() string { return filepath.Join(w.Root, "changesets") }

// EvidenceDir holds evidence packets handed to workers.
func (w *Workspace) EvidenceDir() string { return filepath.Join(w.Root, "evidence") }

// BrainPath returns the absolute path of the brain repository, when the
// workspace has adopted one.
func (w *Workspace) BrainPath() (string, bool) {
	repo, ok := w.Manifest.BrainRepo()
	if !ok {
		return "", false
	}
	return w.RepoPath(repo), true
}

// SaveManifest validates and writes the manifest back to disk.
func (w *Workspace) SaveManifest(next manifest.Manifest) error {
	// The comparison below narrows the window; the lock closes it. Without one,
	// two commands can both read the old file before either writes, and both
	// then pass a check that was true when they made it — which is how eight
	// concurrent `vat repo add` calls left two entries and reported eight
	// successes.
	//
	// Held for the read and the write and nothing else, so a clone or a commit
	// elsewhere in the command never blocks anybody.
	release, err := lockManifest(w.Root)
	if err != nil {
		return err
	}
	defer release()

	// Refused, never merged. Two commands changing the manifest at once is a
	// state that cannot be resolved safely — nothing here can know whether the
	// other one's entry belongs beside this one — and this tool reports such a
	// state rather than picking a winner in silence.
	current, _, err := fsx.ReadFileIfExists(w.ManifestPath())
	if err != nil {
		return err
	}
	if w.read != nil && !bytes.Equal(current, w.read) {
		return fmt.Errorf(
			"%s changed since this command read it; nothing was written. Re-run it", manifest.FileName)
	}
	if err := manifest.Save(w.ManifestPath(), next); err != nil {
		return err
	}
	// The workspace now describes what it just wrote, so a command that saves
	// and then keeps working — `vat repo new` renders from the same one — is not
	// refused by its own write.
	written, _, err := fsx.ReadFileIfExists(w.ManifestPath())
	if err != nil {
		return err
	}
	w.Manifest, w.read = next, written
	return nil
}

// Select resolves a selector against the manifest.
func (w *Workspace) Select(sel manifest.Selector) ([]manifest.Repo, error) {
	return w.Manifest.Select(sel)
}

// Exists reports whether a repository's directory is present on disk.
func (w *Workspace) Exists(repo manifest.Repo) bool {
	info, err := os.Stat(w.RepoPath(repo))
	return err == nil && info.IsDir()
}

// lockDir is vat's own local state, already excluded from the workspace's
// history by the generated .gitignore region.
const lockDir = ".vat"

// lockManifest takes an exclusive lock on the manifest and returns the release.
//
// O_EXCL rather than flock, because this has to hold on Windows as well, and a
// lock somebody has to remember to release on one platform is not a lock.
// Ordinary back-to-back commands never see it: the lock is held for a read and
// a write. A command that waits longer than that is waiting on one that has
// stopped, and is told rather than left to guess.
func lockManifest(root string) (func(), error) {
	if err := fsx.EnsureDir(filepath.Join(root, lockDir)); err != nil {
		return nil, err
	}
	path := filepath.Join(root, lockDir, "manifest.lock")
	deadline := time.Now().Add(lockWait)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fsx.DefaultFileMode)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		// A lock is held for one read and one write. One older than lockStale
		// belongs to something that died holding it, and a workspace nobody can
		// write to until they know to delete a file is a worse failure than the
		// one the lock prevents.
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStale {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf(
				"another vat command is changing %s and has not finished.\n"+
					"  Wait for it, or delete %s if nothing is running",
				manifest.FileName, filepath.Join(lockDir, "manifest.lock"))
		}
		time.Sleep(lockPoll)
	}
}

// lockWait is how long a command waits for another to finish writing the
// manifest. Long enough that a queue of commands never sees it, short enough
// that a lock left by something that died is reported rather than hung on.
const (
	lockWait = 5 * time.Second
	lockPoll = 5 * time.Millisecond
	// lockStale is four orders of magnitude longer than the critical section,
	// so nothing alive is ever mistaken for something that died.
	lockStale = 60 * time.Second
)
