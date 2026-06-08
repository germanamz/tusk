package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/indexepoch"
)

// runResetCLI runs the CLI with an explicit stdin (for the confirmation prompt)
// and returns combined stdout/stderr.
func runResetCLI(stdin string, args ...string) (string, error) {
	buf := &bytes.Buffer{}
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetIn(strings.NewReader(stdin))
	rootCmd.SetArgs(args)

	execErr := rootCmd.Execute()

	return buf.String(), execErr
}

func TestReset_WithYesRebuilds(test *testing.T) {
	root := setupTempWorkspace(test)
	createNode(test, root, "notes/hi.md", "note", "Hi", "")
	chdir(test, root)

	out, err := runResetCLI("", "reset", "--yes")

	if err != nil {
		test.Fatalf("reset --yes: %v (out=%s)", err, out)
	}

	if !strings.Contains(out, "Reset done") {
		test.Fatalf("expected 'Reset done' summary, got: %s", out)
	}

	// The node must be back in the index (rebuilt from disk).
	listOut, listErr := runCLI("node", "list")

	if listErr != nil {
		test.Fatalf("node list: %v", listErr)
	}

	if !strings.Contains(listOut, "notes/hi") {
		test.Fatalf("expected rebuilt index to contain notes/hi, got: %s", listOut)
	}
}

func TestReset_WithoutYesAborts(test *testing.T) {
	root := setupTempWorkspace(test)
	createNode(test, root, "notes/hi.md", "note", "Hi", "")
	chdir(test, root)

	// Answer "n" to the prompt.
	out, err := runResetCLI("n\n", "reset")

	if err != nil {
		test.Fatalf("reset (declined) should exit cleanly, got: %v", err)
	}

	if !strings.Contains(out, "Aborted") {
		test.Fatalf("expected 'Aborted', got: %s", out)
	}

	// Reset must NOT have run: the epoch is still 0.
	if epoch, _ := indexepoch.Read(root); epoch != 0 {
		test.Fatalf("reset ran despite decline (epoch=%d)", epoch)
	}
}

func TestReset_RecoversCorruptIndex(test *testing.T) {
	root := setupTempWorkspace(test)
	createNode(test, root, "notes/hi.md", "note", "Hi", "")
	chdir(test, root)

	// Corrupt the index file with garbage bytes (no process holds it open here).
	indexPath := filepath.Join(root, ".tusk", "index.db")

	if writeErr := os.WriteFile(indexPath, []byte("not a sqlite database at all"), 0o644); writeErr != nil {
		test.Fatalf("corrupt index: %v", writeErr)
	}

	out, err := runResetCLI("", "reset", "--yes")

	if err != nil {
		test.Fatalf("reset --yes on corrupt index: %v (out=%s)", err, out)
	}

	if !strings.Contains(out, "Reset done") {
		test.Fatalf("expected recovery, got: %s", out)
	}

	listOut, listErr := runCLI("node", "list")

	if listErr != nil {
		test.Fatalf("node list after recovery: %v", listErr)
	}

	if !strings.Contains(listOut, "notes/hi") {
		test.Fatalf("expected recovered index to contain notes/hi, got: %s", listOut)
	}
}
