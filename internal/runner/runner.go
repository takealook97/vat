// Package runner executes shell commands inside repositories, with the
// bookkeeping every caller needs: which repository, what was run, how long it
// took, and what it printed when it failed.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Result is one command execution.
type Result struct {
	Repo     string        `json:"repo"`
	Command  string        `json:"command"`
	Dir      string        `json:"-"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"-"`
	Stdout   string        `json:"stdout,omitempty"`
	Stderr   string        `json:"stderr,omitempty"`
	Err      error         `json:"-"`
	// Skipped marks a job abandoned because an earlier one failed. It is
	// neither a pass nor a failure.
	Skipped bool `json:"skipped,omitempty"`
}

// OK reports whether the command ran and succeeded. A skipped job is not OK
// and is not a failure either; check Skipped to tell them apart.
func (r Result) OK() bool { return !r.Skipped && r.Err == nil && r.ExitCode == 0 }

// Output returns stderr when present, otherwise stdout, trimmed for display.
func (r Result) Output() string {
	if trimmed := strings.TrimSpace(r.Stderr); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(r.Stdout)
}

// FirstLine returns the first non-empty output line, for one-line reporting.
func (r Result) FirstLine() string {
	for _, line := range strings.Split(r.Output(), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	if r.Err != nil {
		return r.Err.Error()
	}
	return ""
}

// Job is one command to run in one directory.
//
// A job carries either Argv or Command, never both. Argv is executed directly,
// so the caller's quoting survives; Command is handed to a shell, which is what
// a canonical check declared in vat.yaml expects.
type Job struct {
	Repo string
	Dir  string
	// Command is a shell fragment, used for checks written in the manifest.
	Command string
	// Argv is an already-split argument vector, executed with no shell.
	Argv []string
	Env  []string
}

// Display returns the job's command as one readable line.
func (j Job) Display() string {
	if len(j.Argv) > 0 {
		return strings.Join(j.Argv, " ")
	}
	return j.Command
}

// Options configure a run.
type Options struct {
	// Parallelism caps concurrent jobs. Commands are run through a shell, so
	// unbounded concurrency can exhaust a machine.
	Parallelism int
	// Timeout bounds each individual command.
	Timeout time.Duration
	// Stream sends live output for a job to the caller as it completes.
	Stream func(Result)
	// StopOnFailure abandons the remaining jobs once one fails. Lowering
	// concurrency alone does not achieve this: every job is still queued and
	// still runs, just one at a time.
	StopOnFailure bool
}

// Run executes every job, returning results in the order the jobs were given.
//
// With StopOnFailure the jobs run sequentially and the run abandons the rest as
// soon as one fails; those jobs come back with Skipped set so the caller can
// tell "did not run" from "ran and passed".
func Run(ctx context.Context, jobs []Job, opts Options) []Result {
	if opts.StopOnFailure {
		return runSequentially(ctx, jobs, opts)
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 1
	}
	results := make([]Result, len(jobs))
	semaphore := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	var streamMu sync.Mutex

	for i, job := range jobs {
		wg.Add(1)
		go func(index int, job Job) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			result := RunOne(ctx, job, opts.Timeout)
			results[index] = result
			if opts.Stream != nil {
				streamMu.Lock()
				opts.Stream(result)
				streamMu.Unlock()
			}
		}(i, job)
	}
	wg.Wait()
	return results
}

func runSequentially(ctx context.Context, jobs []Job, opts Options) []Result {
	results := make([]Result, len(jobs))
	stopped := false
	for i, job := range jobs {
		if stopped {
			results[i] = Result{Repo: job.Repo, Command: job.Display(), Dir: job.Dir, Skipped: true}
			continue
		}
		results[i] = RunOne(ctx, job, opts.Timeout)
		if opts.Stream != nil {
			opts.Stream(results[i])
		}
		if !results[i].OK() {
			stopped = true
		}
	}
	return results
}

// RunOne executes a single command through the platform shell.
func RunOne(ctx context.Context, job Job, timeout time.Duration) Result {
	started := time.Now()
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	var cmd *exec.Cmd
	if len(job.Argv) > 0 {
		// Executed directly: the caller already split these, and re-joining
		// them into a string would let a shell re-interpret quotes, globs, and
		// separators the caller's own shell had consumed.
		cmd = exec.CommandContext(runCtx, job.Argv[0], job.Argv[1:]...)
	} else {
		shell, flag := shellCommand()
		cmd = exec.CommandContext(runCtx, shell, flag, job.Command)
	}
	cmd.Dir = job.Dir
	cmd.Env = append(os.Environ(), job.Env...)
	// A timed-out job must not leave its children running. exec's default
	// cancellation kills the process it started and nothing beneath it, which
	// for a shell command is everything that matters.
	isolateProcessGroup(cmd)
	cmd.Cancel = func() error { return terminateGroup(cmd) }

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := Result{
		Repo: job.Repo, Command: job.Display(), Dir: job.Dir,
		Duration: time.Since(started),
		Stdout:   stdout.String(), Stderr: stderr.String(),
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			result.Err = fmt.Errorf("exit status %d", result.ExitCode)
		} else {
			result.ExitCode = -1
			result.Err = err
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			result.Err = fmt.Errorf("timed out after %s", timeout)
		}
	}
	return result
}

func shellCommand() (string, string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell, "-c"
	}
	return "/bin/sh", "-c"
}
