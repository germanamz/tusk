package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2E_Plan3IgnoreLockMoveDelete(test *testing.T) {
	tmpDir := initWorkspaceWithManifest(test, edgeManifestBody())

	// Add a .gitignore that excludes a "drafts" dir.
	if writeErr := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("drafts/\n"), 0o644); writeErr != nil {
		test.Fatalf("gitignore: %v", writeErr)
	}

	// Drop a node in the ignored dir.
	if mkErr := os.MkdirAll(filepath.Join(tmpDir, "drafts"), 0o755); mkErr != nil {
		test.Fatalf("mkdir drafts: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "drafts/private.md"), []byte("---\ntype: note\n---\n\nshhh\n"), 0o644); writeErr != nil {
		test.Fatalf("write drafts: %v", writeErr)
	}

	// And two real nodes.
	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "Foo", "--path", "tickets/foo.md"},
		{"node", "create", "--type", "ticket", "--title", "Bar", "--path", "tickets/bar.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("create %v: %v", args, execErr)
		}
	}

	// Reindex — should NOT pick up drafts/private.md.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"reindex"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("reindex: %v", execErr)
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"node", "list"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list: %v", execErr)
		}

		if strings.Contains(out.String(), "drafts/private") {
			test.Errorf("drafts should be ignored: %s", out.String())
		}

		if !strings.Contains(out.String(), "tickets/foo") {
			test.Errorf("tickets/foo should be listed: %s", out.String())
		}
	}

	// Add a parent edge from foo → bar via manual edit + reindex.
	fooPath := filepath.Join(tmpDir, "tickets/foo.md")
	fooBody, _ := os.ReadFile(fooPath)
	fooBodyWithParent := strings.Replace(string(fooBody), "title: Foo", "title: Foo\nparent: tickets/bar", 1)
	os.WriteFile(fooPath, []byte(fooBodyWithParent), 0o644)

	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"reindex"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("reindex 2: %v", execErr)
		}
	}

	// Move bar → tickets/baz; foo's frontmatter must be rewritten.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"node", "move", "tickets/bar", "tickets/baz.md"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("move: %v", execErr)
		}
	}

	updatedFoo, _ := os.ReadFile(fooPath)

	if !strings.Contains(string(updatedFoo), "parent: tickets/baz") {
		test.Errorf("foo should now reference baz:\n%s", string(updatedFoo))
	}

	// Delete foo via CLI; file gone.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"node", "delete", "tickets/foo"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("delete: %v", execErr)
		}
	}

	if _, statErr := os.Stat(fooPath); !os.IsNotExist(statErr) {
		test.Errorf("foo should be gone")
	}
}
