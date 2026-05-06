package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// setupTempWorkspace creates a temp directory and runs `tusk init` in it, then
// returns the directory path. The working directory is NOT changed.
func setupTempWorkspace(test *testing.T) string {
	test.Helper()

	tmpDir := test.TempDir()
	originalCwd, getCwdErr := os.Getwd()

	if getCwdErr != nil {
		test.Fatalf("Getwd: %v", getCwdErr)
	}

	if chdirErr := os.Chdir(tmpDir); chdirErr != nil {
		test.Fatalf("Chdir to tmpDir: %v", chdirErr)
	}

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "test"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("init: %v", execErr)
	}

	if chdirErr := os.Chdir(originalCwd); chdirErr != nil {
		test.Fatalf("Chdir back: %v", chdirErr)
	}

	return tmpDir
}

// chdir changes the working directory to dir. When dir is non-empty it also
// registers a test cleanup that restores the previous working directory.
// Passing "" is a no-op (cleanup is handled by the prior non-empty call).
func chdir(test *testing.T, dir string) {
	test.Helper()

	if dir == "" {
		return
	}

	originalCwd, getCwdErr := os.Getwd()

	if getCwdErr != nil {
		test.Fatalf("chdir Getwd: %v", getCwdErr)
	}

	if chdirErr := os.Chdir(dir); chdirErr != nil {
		test.Fatalf("Chdir %s: %v", dir, chdirErr)
	}

	test.Cleanup(func() { _ = os.Chdir(originalCwd) })
}

// createNode creates a node via the CLI using node create. It calls
// `tusk reindex` so the index row is present for subsequent commands.
// The body argument is reserved for future use and currently ignored.
func createNode(test *testing.T, root, relPath, nodeType, title, body string) {
	test.Helper()

	_ = body

	originalCwd, getCwdErr := os.Getwd()

	if getCwdErr != nil {
		test.Fatalf("Getwd: %v", getCwdErr)
	}

	if chdirErr := os.Chdir(root); chdirErr != nil {
		test.Fatalf("Chdir to root: %v", chdirErr)
	}

	defer func() { _ = os.Chdir(originalCwd) }()

	args := []string{"node", "create", "--type", nodeType, "--path", relPath}

	if title != "" {
		args = append(args, "--title", title)
	}

	createCmd := newRootCmd()
	createCmd.SetArgs(args)

	if execErr := createCmd.Execute(); execErr != nil {
		test.Fatalf("createNode %s: %v", relPath, execErr)
	}

	// Ensure the index row is visible for subsequent commands.
	reindexCmd := newRootCmd()
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex after createNode: %v", execErr)
	}
}

// captureStdout redirects os.Stdout to a pipe, runs fn, and returns the
// captured output as a string.
func captureStdout(test *testing.T, fn func()) string {
	test.Helper()

	reader, writer, pipeErr := os.Pipe()

	if pipeErr != nil {
		test.Fatalf("captureStdout: pipe: %v", pipeErr)
	}

	original := os.Stdout
	os.Stdout = writer

	fn()

	writer.Close()
	os.Stdout = original

	captured, readErr := io.ReadAll(reader)

	if readErr != nil {
		test.Fatalf("captureStdout: read: %v", readErr)
	}

	return string(captured)
}

// runCLI executes the tusk CLI with the given arguments and returns the combined
// stdout/stderr output.
func runCLI(args ...string) (string, error) {
	buf := &bytes.Buffer{}

	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)

	execErr := rootCmd.Execute()

	return buf.String(), execErr
}
