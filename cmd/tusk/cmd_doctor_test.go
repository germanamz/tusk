package main

import (
	"strings"
	"testing"
)

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
