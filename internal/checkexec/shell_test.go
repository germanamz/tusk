package checkexec_test

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/checkexec"
	"github.com/germanamz/tusk/internal/doctor"
)

// The doctor.CheckRunner contract, against the real shell: exit code and
// interleaved output come back as a result, the command runs in dir, a
// command past its timeout is stopped with its children, a command that can't
// start is an error, and runaway output is capped.
func TestShell_MeetsTheCheckRunnerContract(test *testing.T) {
	if runtime.GOOS == "windows" {
		test.Skip("the commands below are POSIX shell")
	}

	var runner doctor.CheckRunner = checkexec.Shell{}

	dir := test.TempDir()

	test.Run("exit code and output", func(test *testing.T) {
		result, runErr := runner.Run("echo out; echo err >&2; exit 3", dir, 5*time.Second)

		if runErr != nil || result.ExitCode != 3 || result.TimedOut || string(result.Output) != "out\nerr\n" {
			test.Fatalf("Run = %+v (%q), %v, want exit 3 with out and err", result, result.Output, runErr)
		}
	})

	test.Run("runs in dir", func(test *testing.T) {
		result, runErr := runner.Run("pwd", dir, 5*time.Second)

		wantDir, _ := filepath.EvalSymlinks(dir)
		gotDir, _ := filepath.EvalSymlinks(strings.TrimSpace(string(result.Output)))

		if runErr != nil || result.ExitCode != 0 || gotDir != wantDir {
			test.Fatalf("pwd = %q, %v, want %q", result.Output, runErr, wantDir)
		}
	})

	test.Run("timeout stops the command and its children", func(test *testing.T) {
		started := time.Now()

		result, runErr := runner.Run("(sleep 30; echo late) & sleep 30", dir, 200*time.Millisecond)

		if elapsed := time.Since(started); elapsed > 10*time.Second {
			test.Fatalf("Run returned after %s, want soon after the timeout", elapsed)
		}

		if runErr != nil || !result.TimedOut {
			test.Fatalf("Run = %+v, %v, want TimedOut", result, runErr)
		}
	})

	test.Run("a command that can't start is an error", func(test *testing.T) {
		if _, runErr := runner.Run("true", filepath.Join(dir, "missing"), 5*time.Second); runErr == nil {
			test.Fatal("Run in a missing dir = nil error, want the start failure")
		}
	})

	test.Run("output is capped", func(test *testing.T) {
		result, runErr := runner.Run("head -c 6000000 /dev/zero | tr '\\0' a", dir, 30*time.Second)

		if runErr != nil || result.ExitCode != 0 {
			test.Fatalf("Run = exit %d, %v", result.ExitCode, runErr)
		}

		if len(result.Output) > checkexec.MaxOutputBytes+100 || !bytes.HasSuffix(result.Output, []byte(checkexec.TruncatedNote)) {
			test.Fatalf("output is %d bytes ending %q, want at most the cap and the truncation note", len(result.Output), result.Output[max(0, len(result.Output)-40):])
		}
	})
}
