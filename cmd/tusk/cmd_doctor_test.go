package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctor_RendersPropertyTypeMismatch(test *testing.T) {
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

	if _, _, reindexOk := runCLISplit(root, "reindex"); !reindexOk {
		test.Fatalf("reindex failed")
	}

	stdout, _, ok := runCLISplit(root, "doctor")

	if !ok {
		test.Errorf("exit non-zero, want 0")
	}

	if !strings.Contains(stdout.String(), "type-mismatch") {
		test.Errorf("stdout = %q, want mention of type-mismatch", stdout.String())
	}

	if !strings.Contains(stdout.String(), "tickets/bar") {
		test.Errorf("stdout = %q, want mention of tickets/bar", stdout.String())
	}
}

func TestDoctor_PrintsCleanReport(test *testing.T) {
	root := setupTempWorkspace(test)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("doctor")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "no issues") {
		test.Errorf("expected 'no issues', got:\n%s", out)
	}
}

func TestDoctor_RendersWorkflowViolation(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	// Use mustCreateNode to create a node with off-schema status.
	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "blocked"})

	stdout, _, ok := runCLISplit(root, "doctor")

	if !ok {
		test.Errorf("exit non-zero, want 0")
	}

	if !strings.Contains(stdout.String(), "workflow-violation") {
		test.Errorf("stdout = %q, want mention of workflow-violation", stdout.String())
	}

	if !strings.Contains(stdout.String(), "tickets/foo") {
		test.Errorf("stdout = %q, want mention of tickets/foo", stdout.String())
	}
}
