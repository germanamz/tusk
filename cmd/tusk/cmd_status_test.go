package main

import (
	"strings"
	"testing"
)

func TestStatus_PrintsCounts(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "tickets/a.md", "ticket", "A", "")
	createNode(test, root, "tickets/b.md", "ticket", "B", "")
	createNode(test, root, "notes/c.md", "note", "C", "")

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("status")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "ticket") || !strings.Contains(out, "2") {
		test.Errorf("expected 'ticket … 2' in:\n%s", out)
	}

	if !strings.Contains(out, "note") || !strings.Contains(out, "1") {
		test.Errorf("expected 'note … 1' in:\n%s", out)
	}
}
