package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReindex_OffSchemaPropertyReportedInSummary(test *testing.T) {
	root := newWorkspaceWithNodeTypes(test)

	if mkErr := os.MkdirAll(filepath.Join(root, "tickets"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "tickets/bar.md"), []byte(`---
type: ticket
summary: hi
priority: high
---
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	stdout, _, ok := runCLISplit(root, "reindex")

	if !ok {
		test.Errorf("exit non-zero, want 0")
	}

	if !strings.Contains(stdout.String(), "property-violation") {
		test.Errorf("stdout = %q, want mention of property-violation", stdout.String())
	}
}

func TestReindexCmd_PicksUpExternalFile(test *testing.T) {
	tmpDir := initWorkspace(test)

	external := filepath.Join(tmpDir, "external.md")
	body := []byte("---\ntype: note\ntitle: External\n---\n\nbody.\n")

	if writeErr := os.WriteFile(external, body, 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	output := &bytes.Buffer{}

	reindexCmd := newRootCmd()
	reindexCmd.SetOut(output)
	reindexCmd.SetErr(output)
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	listOutput := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(listOutput)
	listCmd.SetErr(listOutput)
	listCmd.SetArgs([]string{"node", "list"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("list: %v", execErr)
	}

	if !bytes.Contains(listOutput.Bytes(), []byte("external")) {
		test.Errorf("missing external in list:\n%s", listOutput.String())
	}
}

func TestReindexCmd_RespectsWorkspaceIgnoreFromManifest(test *testing.T) {
	tmpDir := initWorkspaceWithManifest(test, `[workspace]
name = "test"
ignore = ["scratch/"]
`)

	if mkErr := os.MkdirAll(filepath.Join(tmpDir, "scratch"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "scratch/skipme.md"), []byte("---\ntype: note\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "keep.md"), []byte("---\ntype: note\ntitle: Keep\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"reindex"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	listOutput := &bytes.Buffer{}
	listCmd := newRootCmd()
	listCmd.SetOut(listOutput)
	listCmd.SetErr(listOutput)
	listCmd.SetArgs([]string{"node", "list"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("list: %v", execErr)
	}

	if strings.Contains(listOutput.String(), "scratch/skipme") {
		test.Errorf("scratch/ should be ignored:\n%s", listOutput.String())
	}

	if !strings.Contains(listOutput.String(), "keep") {
		test.Errorf("keep.md should be listed:\n%s", listOutput.String())
	}
}

func TestReindex_OffSchemaStatusReportedInSummary(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	if mkErr := os.MkdirAll(filepath.Join(root, "tickets"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "tickets/foo.md"), []byte(`---
type: ticket
status: bogus
---
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	stdout, _, ok := runCLISplit(root, "reindex")

	if !ok {
		test.Errorf("exit non-zero, want 0")
	}

	if !strings.Contains(stdout.String(), "workflow-violation") {
		test.Errorf("stdout = %q, want mention of workflow-violation", stdout.String())
	}
}

func TestReindexCmd_VerboseEmitsWalkLogs(test *testing.T) {
	wsDir := setupTempWorkspace(test)
	chdir(test, wsDir)

	stderr := &bytes.Buffer{}
	rootCmd := newRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs([]string{"reindex", "--verbose"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	if !strings.Contains(stderr.String(), `msg="reindex walk complete"`) {
		test.Errorf("expected walk-complete log on stderr; got %q", stderr.String())
	}
}

func TestReindexCmd_DefaultSilentOnWalkLogs(test *testing.T) {
	wsDir := setupTempWorkspace(test)
	chdir(test, wsDir)

	stderr := &bytes.Buffer{}
	rootCmd := newRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs([]string{"reindex"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	if strings.Contains(stderr.String(), `msg="reindex walk complete"`) {
		test.Errorf("default reindex should not emit Info logs; got %q", stderr.String())
	}
}
