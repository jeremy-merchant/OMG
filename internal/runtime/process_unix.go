//go:build !windows

package runtime

import (
	"os/exec"
	"syscall"
)

func configureProcessCancellation(command *exec.Cmd) func() error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return func() error {
		if command.Process == nil {
			return nil
		}
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return err
		}
		return nil
	}
}
