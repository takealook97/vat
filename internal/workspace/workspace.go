// Package workspace resolves the workspace root and gives the rest of vat a
// single, validated view of it: where the manifest is, where each repository
// should live, and which files vat owns.
package workspace

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/changeset"
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

// ParticipantDir maps a changeset participant name onto its working tree.
//
// Two kinds of name resolve: a repository in `repos:`, and
// changeset.WorkspaceParticipant, the root itself. Any other name is unknown,
// or a mistyped repository would be taken for something real. It is the one
// lookup both the commands and lint go through, because a rule that resolved
// names differently from the command that wrote the record would check a
// different tree than the one the record describes.
func (w *Workspace) ParticipantDir(name string) (string, bool) {
	if name == changeset.WorkspaceParticipant {
		return w.Root, true
	}
	repo, ok := w.Manifest.Find(name)
	if !ok {
		return "", false
	}
	return w.RepoPath(repo), true
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
		release, err := createLock(path)
		if err == nil {
			return release, nil
		}
		if !IsLockContention(err, path) {
			return nil, err
		}
		// A lock is held for one read and one write. One older than lockStale
		// belongs to something that died holding it, and a workspace nobody can
		// write to until they know to delete a file is a worse failure than the
		// one the lock prevents.
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStale {
			reclaimed, err := reclaimStaleLock(path, info)
			if err != nil {
				return nil, err
			}
			if reclaimed {
				continue
			}
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

// createLock creates the lock file holding a token of its own and returns a
// release that removes the file only while it still holds that token.
//
// Removing by path alone let a command whose lock had been taken over delete
// the lock that replaced it, handing the workspace to a third command while
// the second was still writing.
func createLock(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fsx.DefaultFileMode)
	if err != nil {
		return nil, err
	}
	token := make([]byte, lockTokenBytes)
	_, _ = rand.Read(token) // crypto/rand.Read never returns an error.
	mine := hex.EncodeToString(token)
	_, writeErr := file.WriteString(mine)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	return func() {
		if held, err := os.ReadFile(path); err == nil && string(held) == mine {
			_ = os.Remove(path)
		}
	}, nil
}

// reclaimStaleLock removes the stale lock the caller saw, and only that one.
// It reports whether the caller should try to take the lock again at once.
//
// Every waiter sees the same stale file. Each used to remove whatever sat at
// the path, so the first to remove it and take the lock could have its fresh
// lock removed by the next, and two commands then held it at once — eight
// waiters overlapped in eight rounds out of four hundred.
//
// File systems have no compare-and-delete, so removal of one stale file is
// serialised by a claim named after that file's modification time: O_EXCL lets
// exactly one waiter create it, and that waiter checks under the claim that the
// path still holds the same stale file before removing it. Every other waiter
// goes back to waiting and sees the new holder's fresh lock. A late waiter that
// creates the claim after the stale file is gone finds a different file at the
// path and removes nothing.
//
// The modification time is the identity rather than os.SameFile because on
// Windows SameFile reads the file IDs from the path when it is called, so two
// Stat results of one path always compare equal and would prove nothing; and a
// fresh lock cannot share a stale one's modification time.
//
// A claim left by a waiter that died between creating and removing it only
// stops automatic recovery of that one stale file; the wait then ends in the
// usual message naming the lock to delete.
func reclaimStaleLock(path string, seen fs.FileInfo) (bool, error) {
	claim := fmt.Sprintf("%s.reclaim-%d", path, seen.ModTime().UnixNano())
	file, err := os.OpenFile(claim, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fsx.DefaultFileMode)
	if err != nil {
		if IsLockContention(err, claim) {
			return false, nil
		}
		return false, err
	}
	_ = file.Close()
	defer func() { _ = os.Remove(claim) }()

	now, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !now.ModTime().Equal(seen.ModTime()) {
		return true, nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	return true, nil
}

// IsLockContention reports whether a failure to create the lock file means
// somebody else holds it, rather than that something is wrong.
//
// POSIX answers "already exists". Windows answers "access denied" whenever the
// name is in delete-pending state — the instant between one holder releasing
// the lock and the next acquiring it — so an ordinary race between two vat
// commands surfaced as "Access is denied", which names nothing a person can act
// on and stopped the write.
//
// Deciding by whether the directory can be written gives the same answer on
// every platform, and keeps a real permission problem a real error: where a
// file cannot be created at all, a refusal to create the lock is not
// contention.
//
// Asking whether the lock file exists is not enough, and this is the second
// version of that idea. A name in delete-pending state answers "access denied"
// to stat as readily as to open, and it stops existing entirely the moment the
// holder's Remove lands — so both of the states this is meant to recognise can
// present as "the lock is not there", and four writers racing on Windows had
// three of them fail with a message naming nothing they could act on.
func IsLockContention(err error, path string) bool {
	if errors.Is(err, fs.ErrExist) {
		return true
	}
	if !errors.Is(err, fs.ErrPermission) {
		return false
	}
	if _, statErr := os.Stat(path); statErr == nil {
		return true
	}
	// The lock is not visible, so the question is whether this directory
	// refuses writes or refuses only this name. A probe answers it without
	// guessing: creating and removing a file of our own choosing exercises the
	// same permission and none of the contention.
	probe, probeErr := os.CreateTemp(filepath.Dir(path), "lock-probe-")
	if probeErr != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true
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
	// lockTokenBytes is enough randomness that two holders never draw the same
	// token, which is all a release needs to tell its own lock from another's.
	lockTokenBytes = 16
)
