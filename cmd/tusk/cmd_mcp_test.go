package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/mcp"
	"github.com/spf13/cobra"
)

func TestMCP_DefaultAddrIsLoopback(test *testing.T) {
	addrFlag := newMCPCmd().Flags().Lookup("addr")

	if addrFlag == nil {
		test.Fatal("mcp command has no --addr flag")
	}

	if !isLoopbackAddr(addrFlag.DefValue) {
		test.Errorf("default --addr = %q, want a loopback address", addrFlag.DefValue)
	}
}

func TestGuardMCPBind(test *testing.T) {
	cases := []struct {
		name      string
		transport string
		addr      string
		stdin     string
		wantErr   bool
	}{
		{"stdio skips the check", "stdio", ":8765", "", false},
		{"sse loopback passes", "sse", "127.0.0.1:8765", "", false},
		{"sse non-loopback declined", "sse", "0.0.0.0:8765", "n\n", true},
		{"sse non-loopback confirmed", "sse", "0.0.0.0:8765", "y\n", false},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			cmd := &cobra.Command{}
			cmd.SetIn(strings.NewReader(testCase.stdin))
			cmd.SetErr(io.Discard)

			guardErr := guardMCPBind(cmd, testCase.transport, testCase.addr)

			if (guardErr != nil) != testCase.wantErr {
				subtest.Errorf("guardMCPBind(%q, %q) err = %v, wantErr %v", testCase.transport, testCase.addr, guardErr, testCase.wantErr)
			}
		})
	}
}

func TestMCP_ParsesTransportFlag(test *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"mcp", "--help"})

	out := captureStdout(test, func() {
		_ = cmd.Execute()
	})

	if !strings.Contains(out, "--transport") {
		test.Errorf("expected --transport flag in help, got:\n%s", out)
	}

	if !strings.Contains(out, "stdio") || !strings.Contains(out, "sse") {
		test.Errorf("expected stdio and sse mentioned, got:\n%s", out)
	}
}

func TestMCP_RejectsUnknownTransport(test *testing.T) {
	root := setupTempWorkspace(test)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("mcp", "--transport", "bogus")

	if runErr == nil {
		test.Fatalf("expected error, got out:\n%s", out)
	}
}

func TestMCP_SSEStartsListener(test *testing.T) {
	root := setupTempWorkspace(test)
	repo := repoRoot(test) // capture before chdir changes cwd

	// Build the binary into a temp dir so we can run it from the workspace root.
	binDir := test.TempDir()
	binPath := filepath.Join(binDir, "tusk")

	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/tusk")
	buildCmd.Dir = repo

	if buildOut, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		test.Fatalf("build: %v\n%s", buildErr, buildOut)
	}

	listener, listenErr := net.Listen("tcp", "127.0.0.1:0")

	if listenErr != nil {
		test.Fatalf("listen: %v", listenErr)
	}

	addr := listener.Addr().String()
	listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sseCmd := exec.CommandContext(ctx, binPath, "mcp", "--transport", "sse", "--addr", addr)
	sseCmd.Dir = root

	stdout, _ := sseCmd.StdoutPipe()
	stderr, _ := sseCmd.StderrPipe()

	_ = stdout
	_ = stderr

	if startErr := sseCmd.Start(); startErr != nil {
		test.Fatalf("start: %v", startErr)
	}

	defer func() {
		cancel()
		_ = sseCmd.Wait()
	}()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if conn, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond); dialErr == nil {
			conn.Close()

			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	test.Fatalf("SSE server never accepted on %s", addr)
}

func TestMCPCmd_VerboseSetsRuntimeLogger(test *testing.T) {
	wsDir := setupTempWorkspace(test)
	chdir(test, wsDir)

	rootCmd := newRootCmd()

	if parseErr := rootCmd.ParseFlags([]string{"--verbose"}); parseErr != nil {
		test.Fatalf("parse flags: %v", parseErr)
	}

	logger := mcpLoggerFromFlags(rootCmd)

	if logger == nil {
		test.Fatal("--verbose should produce a non-nil logger")
	}

	if !logger.Enabled(context.Background(), slog.LevelDebug) {
		test.Error("--verbose logger should be enabled at Debug level")
	}

	runtime, openErr := mcp.Open(wsDir, mcp.WithLogger(logger))

	if openErr != nil {
		test.Fatalf("mcp.Open: %v", openErr)
	}

	defer runtime.Close()

	if runtime.Logger != logger {
		test.Errorf("Runtime.Logger should be the verbose logger")
	}
}

// TestMCPCmd_DefaultSetsWarnLogger pins the A1 fix: the default daemon must NOT
// be deaf. mcpLoggerFromFlags returns a non-nil Warn-level stderr logger even
// without --verbose, so background-component failures (a dead watcher, a stuck
// drainer) are surfaced rather than swallowed. --verbose only drops the level
// to Debug.
func TestMCPCmd_DefaultSetsWarnLogger(test *testing.T) {
	rootCmd := newRootCmd()

	if parseErr := rootCmd.ParseFlags(nil); parseErr != nil {
		test.Fatalf("parse: %v", parseErr)
	}

	logger := mcpLoggerFromFlags(rootCmd)

	if logger == nil {
		test.Fatal("default mcp must build a non-nil logger so background-component failures reach stderr")
	}

	ctx := context.Background()

	if !logger.Enabled(ctx, slog.LevelWarn) {
		test.Error("default logger should be enabled at Warn level")
	}

	if logger.Enabled(ctx, slog.LevelDebug) {
		test.Error("default logger should NOT be enabled at Debug level (Debug is --verbose only)")
	}
}

// repoRoot walks up from the cwd to find the tusk repo root (containing go.mod).
func repoRoot(test *testing.T) string {
	test.Helper()

	dir, _ := os.Getwd()

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)

		if parent == dir {
			test.Fatalf("could not find repo root from %s", dir)
		}

		dir = parent
	}
}
