//go:build !windows

package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/runner"
)

func TestATimedOutJobDoesNotLeaveItsChildrenRunning(t *testing.T) {
	// Arrange: a shell that outlives its own child. Killing only the shell
	// would leave the sleep running with the job already reported timed out,
	// leaking a process per timeout for the life of the session.
	dir := t.TempDir()
	marker := filepath.Join(dir, "child.pid")
	job := runner.Job{
		Repo: "a", Dir: dir,
		Command: "sh -c 'sleep 30 & echo $! > " + marker + "; wait'",
	}

	// Act
	result := runner.RunOne(context.Background(), job, 300*time.Millisecond)

	// Assert
	if result.OK() {
		t.Fatal("the job should have timed out")
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Skipf("the shell did not record a child pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Skipf("unreadable child pid %q", raw)
	}
	// Give the signal a moment to land before asking whether the child is gone.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return // the child is gone, which is what this test wants
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("child process %d survived the timeout", pid)
}
