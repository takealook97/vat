package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Several commands waiting on a lock left by something that died all see the
// same stale file. Each used to remember only the path, so the one that
// removed it and took the lock could have its fresh lock removed by the next
// one, and two commands then held it at once: eight waiters overlapped in
// eight rounds out of four hundred.
func TestLockManifestStaleTakeoverHasOneHolder(t *testing.T) {
	const rounds, waiters = 400, 8
	root := t.TempDir()
	dir := filepath.Join(root, lockDir)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.lock")
	type acquisition struct {
		release func()
		err     error
	}
	overlaps, maxHolders := 0, 0
	for round := 1; round <= rounds; round++ {
		// Arrange: a lock left behind by something that died.
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-lockStale - time.Minute)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		ready := make(chan struct{}, waiters)
		start := make(chan struct{})
		results := make(chan acquisition, waiters)
		for range waiters {
			go func() {
				ready <- struct{}{}
				<-start
				release, err := lockManifest(root)
				results <- acquisition{release, err}
			}()
		}
		for range waiters {
			<-ready
		}

		// Act
		close(start)
		first := <-results
		if first.err != nil {
			t.Fatal(first.err)
		}
		held := []func(){first.release}
		window := time.NewTimer(2 * time.Millisecond)
	collect:
		for len(held) < waiters {
			select {
			case result := <-results:
				if result.err != nil {
					t.Fatal(result.err)
				}
				held = append(held, result.release)
			case <-window.C:
				break collect
			}
		}
		window.Stop()

		// Assert: nobody released yet, so every acquisition so far is a holder.
		if len(held) > 1 {
			overlaps++
		}
		maxHolders = max(maxHolders, len(held))
		for _, release := range held {
			release()
		}
		for range waiters - len(held) {
			result := <-results
			if result.err != nil {
				t.Fatal(result.err)
			}
			result.release()
		}
	}
	t.Logf("rounds=%d waiters=%d overlap_rounds=%d max_holders=%d", rounds, waiters, overlaps, maxHolders)
	if overlaps != 0 {
		t.Fatalf("%d of %d rounds had more than one holder at once (max %d)", overlaps, rounds, maxHolders)
	}
}

// A command whose lock was taken over must not remove the lock that replaced
// it: removing by path alone would hand the workspace to a third command while
// the second still writes.
func TestLockManifestReleaseLeavesALockItDoesNotOwn(t *testing.T) {
	// Arrange
	root := t.TempDir()
	release, err := lockManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, lockDir, "manifest.lock")
	if err := os.WriteFile(path, []byte("somebody-else"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	release()

	// Assert
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("release removed a lock it does not own: %v", err)
	}
	if string(got) != "somebody-else" {
		t.Errorf("lock now holds %q", got)
	}
}

// The ordinary path still works: a release removes the lock it took.
func TestLockManifestReleaseRemovesItsOwnLock(t *testing.T) {
	// Arrange
	root := t.TempDir()
	release, err := lockManifest(root)
	if err != nil {
		t.Fatal(err)
	}

	// Act
	release()

	// Assert
	if _, err := os.Stat(filepath.Join(root, lockDir, "manifest.lock")); !os.IsNotExist(err) {
		t.Errorf("own lock was not removed: %v", err)
	}
}
