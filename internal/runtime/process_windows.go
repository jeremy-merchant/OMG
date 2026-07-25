//go:build windows

package runtime

import "os/exec"

// Windows process groups are not configured here; killing the direct process is the
// portable safe fallback used when the context is cancelled.
func configureProcessCancellation(command *exec.Cmd) func() error {
	return func() error {
		if command.Process == nil {
			return nil
		}
		return command.Process.Kill()
	}
}
