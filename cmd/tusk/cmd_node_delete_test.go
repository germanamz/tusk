package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNodeDeleteCmd_RemovesFileAndIndex(test *testing.T) {
	tmpDir := initWorkspace(test)

	createCmd := newRootCmd()
	createCmd.SetArgs([]string{"node", "create", "--type", "note", "--title", "X", "--path", "victim.md"})

	if execErr := createCmd.Execute(); execErr != nil {
		test.Fatalf("create: %v", execErr)
	}

	deleteCmd := newRootCmd()
	deleteCmd.SetArgs([]string{"node", "delete", "victim"})

	if execErr := deleteCmd.Execute(); execErr != nil {
		test.Fatalf("delete: %v", execErr)
	}

	if _, statErr := os.Stat(filepath.Join(tmpDir, "victim.md")); !os.IsNotExist(statErr) {
		test.Errorf("expected file removed, stat err = %v", statErr)
	}
}
