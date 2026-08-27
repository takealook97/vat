package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The signal wiring itself is exercised in process, because a subprocess proves
// the behaviour and reports nothing about which lines ran.
func TestRunWiresTheSignalHandlerAndReturnsTheExitCode(t *testing.T) {
	// Arrange
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })

	// Act & Assert
	os.Args = []string{"vat", "version"}
	if code := run(); code != 0 {
		t.Errorf("`vat version` returned %d", code)
	}
	os.Args = []string{"vat", "nosuchcommand"}
	if code := run(); code != 2 {
		t.Errorf("an unknown command returned %d, want 2", code)
	}
}

// The entry point is the one package `make cover` reports as not measured, and
// it decides the two things every invocation depends on: the exit code, and what
// happens to in-flight work when somebody presses Ctrl-C. Both were fixed once
// by hand — the signal path by sending SIGINT to a running command rather than
// by reading the code — and neither was held to anything afterwards.
//
// Built and run for real, because what is under test is the process.

func build(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "vat")
	if os.PathSeparator == '\\' {
		binary += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binary, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return binary
}

func TestTheExitCodeSaysWhichKindOfFailureItWas(t *testing.T) {
	// Arrange
	binary := build(t)
	workspace := t.TempDir()

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"version"}, 0},
		{"called wrong", []string{"nosuchcommand"}, 2},
		{"called wrong: outside a workspace", []string{"status"}, 2},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// Act
			cmd := exec.Command(binary, testCase.args...)
			cmd.Dir = workspace
			output, err := cmd.CombinedOutput()

			// Assert
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatalf("run: %v", err)
				}
				code = exit.ExitCode()
			}
			if code != testCase.want {
				t.Errorf("exit = %d, want %d\n%s", code, testCase.want, output)
			}
		})
	}
}

// Ctrl-C during a run stops it rather than being ignored, and stops it promptly:
// a control plane that has to be killed twice is one people learn to kill with
// -9, which is how a half-fetched repository gets left behind.
func TestAnInterruptStopsTheRunPromptly(t *testing.T) {
	// Arrange
	// Windows has no signal delivery: os.Interrupt cannot be sent to another
	// process at all, so there is nothing here to assert rather than a
	// behaviour that differs. The handler it exercises is POSIX-only too.
	if runtime.GOOS == "windows" {
		t.Skip("sending os.Interrupt to another process is not supported on windows")
	}
	binary := build(t)
	workspace := t.TempDir()
	initialise := exec.Command(binary, "init", "--name", "solo")
	initialise.Dir = workspace
	if out, err := initialise.CombinedOutput(); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	cmd := exec.Command(binary, "exec", "--", "sleep", "30")
	cmd.Dir = workspace
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Give it long enough to be inside the command it is running.
	time.Sleep(300 * time.Millisecond)

	// Act
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("signal: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Assert
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the run did not stop within ten seconds of an interrupt")
	}
}

// A temporary file is written beside its destination and renamed. An interrupt
// must not leave one behind for somebody to find and wonder about.
func TestAnInterruptLeavesNoHalfWrittenFile(t *testing.T) {
	// Arrange
	binary := build(t)
	workspace := t.TempDir()
	initialise := exec.Command(binary, "init", "--name", "solo")
	initialise.Dir = workspace
	if out, err := initialise.CombinedOutput(); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	cmd := exec.Command(binary, "exec", "--", "sleep", "30")
	cmd.Dir = workspace
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	// Act & Assert
	var stray []string
	err := filepath.WalkDir(workspace, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".vat-") {
			stray = append(stray, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(stray) > 0 {
		t.Errorf("an interrupted run left temporary files behind: %v", stray)
	}
}
