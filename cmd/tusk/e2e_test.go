package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestE2E_FullLifecycle(test *testing.T) {
	tmpDir := initWorkspace(test)

	// 1) Create a node via CLI.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"node", "create", "--type", "ticket", "--title", "Fix bug", "--path", "tickets/fix-bug.md"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("create: %v", execErr)
		}
	}

	// 2) Drop a second node externally (no CLI).
	external := filepath.Join(tmpDir, "notes/random.md")

	if mkErr := os.MkdirAll(filepath.Dir(external), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	body := []byte("---\ntype: note\ntitle: Random\n---\n\nBody.\n")

	if writeErr := os.WriteFile(external, body, 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	// 3) Reindex picks it up.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"reindex"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("reindex: %v", execErr)
		}
	}

	// 4) List shows both.
	listOut := &bytes.Buffer{}
	{
		cmd := newRootCmd()
		cmd.SetOut(listOut)
		cmd.SetErr(listOut)
		cmd.SetArgs([]string{"node", "list"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list: %v", execErr)
		}
	}

	output := listOut.String()

	if !bytes.Contains([]byte(output), []byte("tickets/fix-bug")) {
		test.Errorf("missing fix-bug:\n%s", output)
	}

	if !bytes.Contains([]byte(output), []byte("notes/random")) {
		test.Errorf("missing random:\n%s", output)
	}

	// 5) Get the externally-created one.
	getOut := &bytes.Buffer{}
	{
		cmd := newRootCmd()
		cmd.SetOut(getOut)
		cmd.SetErr(getOut)
		cmd.SetArgs([]string{"node", "get", "notes/random"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("get: %v", execErr)
		}
	}

	if !bytes.Contains(getOut.Bytes(), []byte("title: Random")) {
		test.Errorf("get output missing title:\n%s", getOut.String())
	}

	// 6) Delete the external file and reindex; list no longer shows it.
	if rmErr := os.Remove(external); rmErr != nil {
		test.Fatalf("rm: %v", rmErr)
	}

	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"reindex"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("second reindex: %v", execErr)
		}
	}

	listOut.Reset()

	{
		cmd := newRootCmd()
		cmd.SetOut(listOut)
		cmd.SetErr(listOut)
		cmd.SetArgs([]string{"node", "list"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("second list: %v", execErr)
		}
	}

	if bytes.Contains(listOut.Bytes(), []byte("notes/random")) {
		test.Errorf("random should be gone after delete + reindex:\n%s", listOut.String())
	}
}
