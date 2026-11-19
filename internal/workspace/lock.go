package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/manifest"
)

// lockDir is vat's own local state, already excluded from the workspace's
// history by the generated .gitignore region.
const lockDir = ".vat"

// lockFile is the file the manifest lock is held on, inside lockDir.
const lockFile = "manifest.lock"

// lockWait is how long a command waits for another to finish writing the
// manifest. Long enough that a queue of commands never sees it, short enough
// that a command stopped while holding the lock is reported rather than hung
// on.
const (
	lockWait = 5 * time.Second
	lockPoll = 5 * time.Millisecond
)

// lockManifest takes an exclusive lock on the manifest and returns the release.
//
// The lock is an operating-system lock on an open file — flock on Unix,
// LockFileEx on Windows — not the existence of a file. The kernel releases it
// when the holder exits for any reason, so a command killed while holding it
// never leaves a workspace that nobody can write to, and nothing has to guess
// from a file's age whether its holder is still alive.
//
// The two earlier designs guessed. An O_EXCL file older than a minute was
// treated as abandoned and removed, which let a holder that was merely slow —
// suspended, or paused under a debugger — share the lock with the command that
// removed its file; and file systems have no compare-and-delete, so every
// attempt to make that removal safe left a window where one command deleted a
// lock another had just created.
//
// The file itself is never removed. Removing it would reopen the race between
// a holder deleting the name and a waiter creating it, and an empty file in
// vat's own state directory costs nothing.
func lockManifest(root string) (func(), error) {
	if err := fsx.EnsureDir(filepath.Join(root, lockDir)); err != nil {
		return nil, err
	}
	path := filepath.Join(root, lockDir, lockFile)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fsx.DefaultFileMode)
	if err != nil {
		return nil, fmt.Errorf("open the manifest lock: %w", err)
	}
	deadline := time.Now().Add(lockWait)
	for {
		held, err := tryLockFile(file)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if held {
			return releaseOnce(file), nil
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, fmt.Errorf(
				"another vat command is changing %s and has not finished.\n"+
					"  Wait for it, or stop the vat command that is still running",
				manifest.FileName)
		}
		time.Sleep(lockPoll)
	}
}

// releaseOnce returns a release that unlocks and closes file the first time it
// is called and does nothing after that.
//
// The lock belongs to this handle, so a release can only ever let go of its
// own lock; Once keeps a second call from closing a descriptor number the
// process may already have handed to somebody else.
func releaseOnce(file *os.File) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			// Closing alone also releases the lock, but Windows documents the
			// release on close as happening at a time of the system's choosing,
			// and the next command should not wait on that.
			_ = errors.Join(unlockFile(file), file.Close())
		})
	}
}
