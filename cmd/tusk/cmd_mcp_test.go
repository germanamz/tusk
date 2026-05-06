package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
