package runner_test

import (
	"context"
	"os"
	"path/filepath"
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

func TestStopOnFailureAbandonsTheRemainingJobs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture command is POSIX shell")
	}
	// Arrange: lowering concurrency alone would still run every job, just one
	// at a time. Stopping has to mean the rest do not run.
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran-third")
	jobs := []runner.Job{
		{Repo: "first", Dir: dir, Command: "true"},
		{Repo: "second", Dir: dir, Command: "exit 1"},
		{Repo: "third", Dir: dir, Command: "touch " + marker},
	}

	// Act
	results := runner.Run(context.Background(), jobs, runner.Options{
		StopOnFailure: true, Timeout: 10 * time.Second,
	})

	// Assert
	if !results[0].OK() {
		t.Error("the first job should have run and passed")
	}
	if results[1].OK() || results[1].Skipped {
		t.Error("the second job should have run and failed")
	}
	if !results[2].Skipped {
		t.Error("the third job was not marked skipped")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the third job ran despite the earlier failure")
	}
}

func TestASkippedJobIsNeitherAPassNorAFailure(t *testing.T) {
	// Arrange
	skipped := runner.Result{Skipped: true}

	// Act & Assert
	if skipped.OK() {
		t.Error("a skipped job reported success")
	}
}

func TestKeepGoingRunsEveryJobDespiteAFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture command is POSIX shell")
	}
	// Arrange
	dir := t.TempDir()
	jobs := []runner.Job{
		{Repo: "first", Dir: dir, Command: "exit 1"},
		{Repo: "second", Dir: dir, Command: "true"},
	}

	// Act
	results := runner.Run(context.Background(), jobs, runner.Options{
		Parallelism: 2, Timeout: 10 * time.Second,
	})

	// Assert
	if results[0].OK() {
		t.Error("the failing job reported success")
	}
	if !results[1].OK() {
		t.Error("a later job was not run after an earlier failure")
	}
}
