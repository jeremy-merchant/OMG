//go:build windows

package platform

import "os/exec"

// Windows lacks Unix process groups here; kill the direct process as a safe fallback.
func configureGitProcess(command *exec.Cmd) func() {
	return func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}
}
