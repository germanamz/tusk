package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeModify_SetProperty(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "notes/x.md", "note", "X", "")

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("node", "modify", "notes/x", "--prop", "priority=5")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	body, _ := os.ReadFile(filepath.Join(root, "notes/x.md"))

	if !strings.Contains(string(body), "priority: 5") {
		test.Errorf("file missing priority: 5\n%s", body)
	}
}

func TestNodeModify_UnsetProperty(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "notes/x.md", "note", "X", "")

	chdir(test, root)
	defer chdir(test, "")

	if _, runErr := runCLI("node", "modify", "notes/x", "--prop", "priority=5"); runErr != nil {
		test.Fatalf("set: %v", runErr)
	}

	if _, runErr := runCLI("node", "modify", "notes/x", "--unset", "priority"); runErr != nil {
		test.Fatalf("unset: %v", runErr)
	}

	body, _ := os.ReadFile(filepath.Join(root, "notes/x.md"))

	if strings.Contains(string(body), "priority:") {
		test.Errorf("priority should be removed:\n%s", body)
	}
}

// runCLI is defined in cmd_test_helpers_test.go; setupTempWorkspace, createNode, chdir
// are existing helpers used by other cmd_*_test.go files.
var _ = bytes.Buffer{} // keep import alive when only one helper uses it
