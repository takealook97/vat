package runner_test

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/runner"
)

func TestASucceedingCommandReportsItsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture command is POSIX shell")
	}
	// Arrange
	job := runner.Job{Repo: "a", Dir: t.TempDir(), Command: "echo hello"}

	// Act
	result := runner.RunOne(context.Background(), job, 10*time.Second)

	// Assert
	if !result.OK() {
		t.Fatalf("command failed: %v", result.Err)
	}
	if strings.TrimSpace(result.Stdout) != "hello" {
		t.Errorf("stdout = %q", result.Stdout)
	}
}

func TestAFailingCommandCarriesItsExitCodeAndMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture command is POSIX shell")
	}
	// Arrange
	job := runner.Job{Repo: "a", Dir: t.TempDir(), Command: "echo trouble >&2; exit 3"}

	// Act
	result := runner.RunOne(context.Background(), job, 10*time.Second)

	// Assert
	if result.OK() {
		t.Fatal("a failing command reported success")
	}
	if result.ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", result.ExitCode)
	}
	if result.FirstLine() != "trouble" {
		t.Errorf("first line = %q, want trouble", result.FirstLine())
	}
}

func TestRunPreservesJobOrderDespiteConcurrency(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture command is POSIX shell")
	}
	// Arrange: the slowest job is first, so unordered results would be visible.
	dir := t.TempDir()
	jobs := []runner.Job{
		{Repo: "slow", Dir: dir, Command: "sleep 0.2; echo slow"},
		{Repo: "fast", Dir: dir, Command: "echo fast"},
	}

	// Act
	results := runner.Run(context.Background(), jobs, runner.Options{Parallelism: 2, Timeout: 10 * time.Second})

	// Assert
	if len(results) != 2 {
		t.Fatalf("result count = %d, want 2", len(results))
	}
	if results[0].Repo != "slow" || results[1].Repo != "fast" {
		t.Errorf("results are out of order: %s then %s", results[0].Repo, results[1].Repo)
	}
}

func TestATimeoutIsReportedAsSuch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture command is POSIX shell")
	}
	// Arrange
	job := runner.Job{Repo: "a", Dir: t.TempDir(), Command: "sleep 5"}

	// Act
	result := runner.RunOne(context.Background(), job, 100*time.Millisecond)

	// Assert
	if result.OK() {
		t.Fatal("a timed-out command reported success")
	}
	if !strings.Contains(result.Err.Error(), "timed out") {
		t.Errorf("error = %v, want a timeout message", result.Err)
	}
}
