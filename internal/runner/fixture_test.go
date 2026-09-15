package runner_test

import (
	"fmt"
	"runtime"
)

// The helpers below exist because seven of the eight tests in this package
// opened with `t.Skip("the fixture command is POSIX shell")`. On Windows the
// suite did not fail there — it never ran, so `shellCommand`'s `cmd /C` branch
// had no coverage on any platform while docs/FAQ.md stated that `vat.yaml`
// commands run through it. CI on three operating systems could not help: what
// it ran there was the skip.
//
// Each helper returns a line for the shell the runner will actually invoke, so
// a test states what it means rather than which shell it assumes.

// pause blocks for roughly the given whole seconds. `ping` is the timer
// available in `cmd` without a shell builtin: `-n` counts attempts and the
// first is immediate, so one more than the wait is sent. Its granularity is a
// second, which is why every caller here waits in whole seconds.
func pause(seconds int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("ping -n %d 127.0.0.1 >nul", seconds+1)
	}
	return fmt.Sprintf("sleep %d", seconds)
}

// sequence joins two commands so the second runs whatever the first returned.
// `;` is the POSIX separator and `&` is cmd's.
func sequence(first, second string) string {
	if runtime.GOOS == "windows" {
		return first + " & " + second
	}
	return first + "; " + second
}

// succeed does nothing and exits zero.
func succeed() string {
	if runtime.GOOS == "windows" {
		return "exit 0"
	}
	return "true"
}

// writeMarker creates an empty file, which is how a test proves a job ran at
// all rather than trusting the runner's report of itself.
func writeMarker(path string) string {
	if runtime.GOOS == "windows" {
		return "type nul > " + path
	}
	return "touch " + path
}

// complainAndFail writes to stderr and exits with a code, so a test can tell
// the two streams apart and check the status separately.
func complainAndFail(message string, code int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("echo %s 1>&2 & exit %d", message, code)
	}
	return fmt.Sprintf("echo %s >&2; exit %d", message, code)
}
