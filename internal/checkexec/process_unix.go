//go:build !windows

package checkexec

import (
	"os/exec"
	"syscall"
)

// stopProcessGroup starts the command in its own process group and makes a
// cancelled context kill the whole group, so a check's children stop too.
func stopProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
