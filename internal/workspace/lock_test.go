package workspace

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lockPath is where lockManifest holds the lock for root.
func lockPath(root string) string { return filepath.Join(root, lockDir, lockFile) }

// lockIsFree reports whether a fresh handle can take the lock at once. It
// answers without waiting out lockWait, and releases what it took.
func lockIsFree(t *testing.T, root string) bool {
	t.Helper()
	file, err := os.OpenFile(lockPath(root), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	held, err := tryLockFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if held {
		releaseOnce(file)()
	} else {
		_ = file.Close()
	}
	return held
}

// Eight commands racing for the lock never hold it together. The earlier
// O_EXCL design let two waiters both remove a file they judged abandoned, and
// eight waiters overlapped in eight rounds out of four hundred.
func TestLockManifestHasOneHolderAtATime(t *testing.T) {
	const rounds, waiters = 400, 8
	root := t.TempDir()
	var holders, maxHolders atomic.Int32
	for range rounds {
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make(chan error, waiters)
		for range waiters {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				release, err := lockManifest(root)
				if err != nil {
					errs <- err
					return
				}
				now := holders.Add(1)
				for {
					seen := maxHolders.Load()
					if now <= seen || maxHolders.CompareAndSwap(seen, now) {
						break
					}
				}
				runtime.Gosched()
				holders.Add(-1)
				release()
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
	}
	t.Logf("rounds=%d waiters=%d max_holders=%d", rounds, waiters, maxHolders.Load())
	if got := maxHolders.Load(); got != 1 {
		t.Fatalf("max holders = %d, want 1", got)
	}
}

// A holder that is alive keeps the lock however old its file looks. Judging by
// age let a command that was merely suspended share the lock with the one that
// removed its file.
func TestLockManifestAnOldFileOfALiveHolderIsStillHeld(t *testing.T) {
	// Arrange
	root := t.TempDir()
	release, err := lockManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(lockPath(root), old, old); err != nil {
		t.Fatal(err)
	}

	// Act
	started := time.Now()
	second, err := lockManifest(root)
	waited := time.Since(started)

	// Assert
	if err == nil {
		second()
		t.Fatal("a second command took the lock while its live holder still had it")
	}
	if !strings.Contains(err.Error(), "another vat command") {
		t.Errorf("the refusal does not say what is in the way: %v", err)
	}
	if waited > lockWait+2*time.Second {
		t.Errorf("waited %v, longer than the %v deadline", waited, lockWait)
	}
}

// TestLockManifestChildHolder is not a test of its own: it is the process the
// next test starts and kills while it holds the lock.
func TestLockManifestChildHolder(t *testing.T) {
	root := os.Getenv("VAT_LOCK_CHILD_ROOT")
	if root == "" {
		t.Skip("run only as the child of TestLockManifestADeadHolderLeavesItFree")
	}
	if _, err := lockManifest(root); err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("held\n")
	time.Sleep(time.Minute)
	t.Fatal("the parent never killed this holder")
}

// A command killed while holding the lock leaves it free. The kernel releases
// an OS lock when its holder dies, so no age threshold is needed to recover.
func TestLockManifestADeadHolderLeavesItFree(t *testing.T) {
	// Arrange: a child process that takes the lock and keeps it.
	root := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^TestLockManifestChildHolder$", "-test.count=1")
	child.Env = append(os.Environ(), "VAT_LOCK_CHILD_ROOT="+root)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "held\n" {
		_ = child.Process.Kill()
		_ = child.Wait()
		t.Fatalf("child did not report holding the lock: %q, %v", line, err)
	}
	if lockIsFree(t, root) {
		_ = child.Process.Kill()
		_ = child.Wait()
		t.Fatal("the lock was free while the child held it")
	}

	// Act: the holder dies without releasing.
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	release, err := lockManifest(root)

	// Assert
	if err != nil {
		t.Fatalf("a lock held by a killed process was not recovered: %v", err)
	}
	release()
}

// A release lets go of its own lock and nothing else, however many times it
// is called. Releasing by path let one command delete the lock that a second
// had just taken and hand the workspace to a third.
func TestLockManifestReleaseLeavesAnotherHoldersLock(t *testing.T) {
	// Arrange
	root := t.TempDir()
	releaseA, err := lockManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	releaseA()
	releaseB, err := lockManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseB()

	// Act
	releaseA()

	// Assert
	if lockIsFree(t, root) {
		t.Fatal("a stale release freed a lock somebody else holds")
	}
	if _, err := os.Stat(lockPath(root)); err != nil {
		t.Fatalf("the lock file is gone: %v", err)
	}
}

// A directory vat cannot write to is reported as what it is, not as a lock
// somebody else holds, which would send the reader looking for a command that
// is not running.
func TestLockManifestAPermissionProblemStaysAPermissionProblem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory modes do not deny writes on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root writes to a directory regardless of its mode")
	}
	// Arrange
	root := t.TempDir()
	sealed := filepath.Join(root, lockDir)
	if err := os.Mkdir(sealed, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sealed, 0o700) })

	// Act
	release, err := lockManifest(root)

	// Assert
	if err == nil {
		release()
		t.Fatal("took a lock in a directory that refuses writes")
	}
	if strings.Contains(err.Error(), "another vat command") {
		t.Errorf("an unwritable directory was reported as a lock somebody holds: %v", err)
	}
}
