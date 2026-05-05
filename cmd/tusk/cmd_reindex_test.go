package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

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
