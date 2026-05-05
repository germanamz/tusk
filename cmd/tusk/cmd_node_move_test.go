package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeMoveCmd_RenamesFileAndRewritesReferences(test *testing.T) {
	tmpDir := initWorkspaceWithManifest(test, edgeManifestBody())

	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "Old", "--path", "tickets/old.md"},
		{"node", "create", "--type", "ticket", "--title", "Child", "--path", "tickets/child.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	childPath := filepath.Join(tmpDir, "tickets/child.md")
	body, _ := os.ReadFile(childPath)
	bodyWithParent := strings.Replace(string(body), "title: Child", "title: Child\nparent: tickets/old", 1)
	_ = os.WriteFile(childPath, []byte(bodyWithParent), 0o644)

	reindexCmd := newRootCmd()
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	moveCmd := newRootCmd()
	moveCmd.SetArgs([]string{"node", "move", "tickets/old", "tickets/new.md"})

	if execErr := moveCmd.Execute(); execErr != nil {
		test.Fatalf("move: %v", execErr)
	}

	if _, statErr := os.Stat(filepath.Join(tmpDir, "tickets/new.md")); statErr != nil {
		test.Errorf("new file missing: %v", statErr)
	}

	if _, statErr := os.Stat(filepath.Join(tmpDir, "tickets/old.md")); !os.IsNotExist(statErr) {
		test.Errorf("old file should be gone")
	}

	updatedChild, _ := os.ReadFile(childPath)

	if !strings.Contains(string(updatedChild), "parent: tickets/new") {
		test.Errorf("child frontmatter not rewritten:\n%s", string(updatedChild))
	}
}
