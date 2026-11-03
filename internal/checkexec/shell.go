// Package checkexec runs [check.<name>] project check commands for doctor
// through the platform shell. It is the driven adapter behind
// doctor.CheckRunner; the CLI wires it only under `tusk doctor --checks`.
package checkexec

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/germanamz/tusk/internal/doctor"
)

// MaxOutputBytes caps how much of a check's output Run keeps, so a command
// that prints without end can't exhaust memory before its timeout.
const MaxOutputBytes = 4 << 20

// TruncatedNote ends output that hit MaxOutputBytes.
const TruncatedNote = "[output truncated]\n"

// waitDelay bounds how long Run waits for the output pipe to close after the
// command exits or is stopped: a child that outlives the shell and keeps the
// pipe open would otherwise hold doctor until it finishes.
const waitDelay = 2 * time.Second

// Shell runs check commands through the platform shell: `sh -c` on Unix and
// `cmd /C` on Windows, with the caller's environment.
type Shell struct{}

// Run implements doctor.CheckRunner. On timeout the whole process group is
// killed (on Unix), so a check's children stop with it.
func (Shell) Run(command, dir string, timeout time.Duration) (doctor.CheckResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := shellCommand(ctx, command)
	cmd.Dir = dir
	cmd.WaitDelay = waitDelay
	stopProcessGroup(cmd)

	output := &cappedBuffer{limit: MaxOutputBytes}
	cmd.Stdout = output
	cmd.Stderr = output

	if startErr := cmd.Start(); startErr != nil {
		return doctor.CheckResult{}, startErr
	}

	waitErr := cmd.Wait()
	result := doctor.CheckResult{Output: output.Bytes(), TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded)}

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	var exitErr *exec.ExitError

	if waitErr != nil && !result.TimedOut && !errors.As(waitErr, &exitErr) && !errors.Is(waitErr, exec.ErrWaitDelay) {
		return result, waitErr
	}

	return result, nil
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}

	return exec.CommandContext(ctx, "sh", "-c", command)
}

// cappedBuffer keeps the first limit bytes written to it and drops the rest,
// noting the cut. Safe for stdout and stderr writing at once.
type cappedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (capped *cappedBuffer) Write(chunk []byte) (int, error) {
	capped.mu.Lock()
	defer capped.mu.Unlock()

	if room := capped.limit - capped.buffer.Len(); room < len(chunk) {
		capped.buffer.Write(chunk[:max(room, 0)])
		capped.truncated = true
	} else {
		capped.buffer.Write(chunk)
	}

	return len(chunk), nil
}

// Bytes returns what was kept, ending with TruncatedNote when output was cut.
func (capped *cappedBuffer) Bytes() []byte {
	capped.mu.Lock()
	defer capped.mu.Unlock()

	kept := bytes.Clone(capped.buffer.Bytes())

	if capped.truncated {
		if len(kept) > 0 && kept[len(kept)-1] != '\n' {
			kept = append(kept, '\n')
		}

		kept = append(kept, TruncatedNote...)
	}

	return kept
}
