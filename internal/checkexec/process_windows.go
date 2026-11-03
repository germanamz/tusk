//go:build windows

package checkexec

import "os/exec"

// stopProcessGroup leaves the default cancel, which kills the shell; WaitDelay
// stops Run waiting on any child that keeps the output pipe open.
func stopProcessGroup(*exec.Cmd) {}
