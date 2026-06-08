package main

import (
	"bytes"
	"strings"
	"testing"
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
