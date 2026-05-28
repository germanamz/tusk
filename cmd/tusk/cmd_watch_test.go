package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestWatchCmd_HelpRenders(test *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"watch", "--help"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Errorf("watch --help: %v", execErr)
	}
}

func TestWatchCmd_WorkersZeroEarlyExit(test *testing.T) {
	wsDir := setupTempWorkspace(test)
	chdir(test, wsDir)

	test.Setenv("TUSK_EMBED_WORKERS", "0")

	stderr := &bytes.Buffer{}
	rootCmd := newRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs([]string{"watch"})

	runErr := rootCmd.Execute()

	if runErr == nil {
		test.Fatalf("expected error when workers=0; got nil")
	}

	if !strings.Contains(runErr.Error(), "embed workers disabled") {
		test.Errorf("error %q does not contain %q", runErr.Error(), "embed workers disabled")
	}
}

func TestWatchCmd_VerboseEmitsInitialReindexLogs(test *testing.T) {
	wsDir := setupTempWorkspace(test)
	chdir(test, wsDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so signal.NotifyContext fires Done() immediately

	stderr := &bytes.Buffer{}
	rootCmd := newRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(stderr)
	rootCmd.SetContext(ctx)
	rootCmd.SetArgs([]string{"watch", "--verbose"})

	_ = rootCmd.Execute() // watcher.Run returns nil on ctx-done; ignore any error from the post-cancel teardown

	if !strings.Contains(stderr.String(), `msg="reindex walk complete"`) {
		test.Errorf("expected walk-complete log on stderr; got %q", stderr.String())
	}
}
