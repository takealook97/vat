//go:build windows

package runner

import "os/exec"

// isolateProcessGroup is a no-op on Windows, which has no process groups in the
// POSIX sense.
func isolateProcessGroup(*exec.Cmd) {}

// terminateGroup kills the command. Windows job objects would be needed to
// reach its children, which vat does not set up.
func terminateGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
