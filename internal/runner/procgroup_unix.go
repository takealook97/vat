//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// isolateProcessGroup puts the command in its own process group so the whole
// tree can be signalled at once.
//
// A shell command spawns children, and killing only the shell leaves them
// running with the job already reported as timed out. A check that hangs would
// otherwise leak a process per timeout for the life of the session.
func isolateProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateGroup signals the command's whole process group.
func terminateGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// The negated pid addresses the group. If the group was never created —
	// the process died between starting and cancelling — fall back to the
	// process itself rather than signalling nothing.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}
