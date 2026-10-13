package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

func TestInitCmd_CreatesManifestAndIndex(test *testing.T) {
	tmpDir := test.TempDir()
	original, getCwdErr := os.Getwd()

	if getCwdErr != nil {
		test.Fatalf("Getwd: %v", getCwdErr)
	}

	test.Cleanup(func() { os.Chdir(original) })

	if chdirErr := os.Chdir(tmpDir); chdirErr != nil {
		test.Fatalf("Chdir: %v", chdirErr)
	}

	rootCmd := newRootCmd()
	output := &bytes.Buffer{}
	rootCmd.SetOut(output)
	rootCmd.SetErr(output)
	rootCmd.SetArgs([]string{"init", "--name", "test-vault"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("Execute: %v", execErr)
	}

	if _, statErr := os.Stat(filepath.Join(tmpDir, "tusk.toml")); statErr != nil {
		test.Errorf("tusk.toml not created: %v", statErr)
	}

	if _, statErr := os.Stat(filepath.Join(tmpDir, ".tusk", "index.db")); statErr != nil {
		test.Errorf(".tusk/index.db not created: %v", statErr)
	}
}

// writeTree writes files (relative path → content) under root.
func writeTree(test *testing.T, root string, files map[string]string) {
	test.Helper()

	for relPath, content := range files {
		absPath := filepath.Join(root, relPath)

		if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
			test.Fatalf("mkdir: %v", mkErr)
		}

		if writeErr := os.WriteFile(absPath, []byte(content), 0o644); writeErr != nil {
			test.Fatalf("write %s: %v", relPath, writeErr)
		}
	}
}

func loadInitManifest(test *testing.T, root string) *manifest.Manifest {
	test.Helper()

	loaded, loadErr := manifest.Load(filepath.Join(root, "tusk.toml"))

	if loadErr != nil {
		test.Fatalf("load the manifest init wrote: %v", loadErr)
	}

	return loaded
}

// TestInitCmd_AdoptsExistingMarkdown covers #790's "point it at the folder you
// already have": init counts the untyped markdown reindex would walk, sets the
// default type and adds the vault pack that declares it, and a plain reindex
// then indexes those files as notes.
func TestInitCmd_AdoptsExistingMarkdown(test *testing.T) {
	root := test.TempDir()

	writeTree(test, root, map[string]string{
		"ideas/auth.md":     "# Auth ideas\n\nSee [[standup]].\n",
		"standup.md":        "---\ntags: [team]\n---\nNotes.\n",
		"typed.md":          "---\ntype: note\n---\nAlready typed.\n",
		"site/index.html":   "<html><body>generated</body></html>",
		"drafts/ignored.md": "# Ignored by .gitignore\n",
		".gitignore":        "drafts/\n",
	})

	stdout, stderr, ok := runCLISplit(root, "init", "--name", "notes")

	if !ok {
		test.Fatalf("init failed: %s", stderr)
	}

	if !strings.Contains(stdout.String(), "Found 2 markdown files without a type") {
		test.Errorf("stdout = %q; want the count of untyped markdown, ignoring HTML, typed and gitignored files", stdout)
	}

	loaded := loadInitManifest(test, root)

	if loaded.Workspace.DefaultType != "note" {
		test.Errorf("default-type = %q, want note", loaded.Workspace.DefaultType)
	}

	if _, declared := loaded.NodeTypes["note"]; !declared {
		test.Errorf("node-types = %v; want the vault pack's note type", loaded.NodeTypes)
	}

	if _, reindexErr, reindexOK := runCLISplit(root, "reindex"); !reindexOK {
		test.Fatalf("reindex failed: %s", reindexErr)
	}

	listOut, listErr, listOK := runCLISplit(root, "node", "list", "type=note")

	if !listOK {
		test.Fatalf("node list failed: %s", listErr)
	}

	for _, want := range []string{"ideas/auth", "standup", "typed"} {
		if !strings.Contains(listOut.String(), want) {
			test.Errorf("node list = %q; want %s indexed as a note", listOut, want)
		}
	}
}

func TestInitCmd_NoAdoptLeavesPlainMarkdownOut(test *testing.T) {
	root := test.TempDir()

	writeTree(test, root, map[string]string{"plain.md": "# Plain\n"})

	stdout, stderr, ok := runCLISplit(root, "init", "--name", "notes", "--no-adopt")

	if !ok {
		test.Fatalf("init failed: %s", stderr)
	}

	if strings.Contains(stdout.String(), "Found") {
		test.Errorf("stdout = %q; --no-adopt must not adopt", stdout)
	}

	loaded := loadInitManifest(test, root)

	if loaded.Workspace.DefaultType != "" || len(loaded.NodeTypes) != 0 {
		test.Errorf("default-type = %q, node-types = %v; want neither with --no-adopt",
			loaded.Workspace.DefaultType, loaded.NodeTypes)
	}
}

func TestInitCmd_NothingToAdopt(test *testing.T) {
	root := test.TempDir()

	writeTree(test, root, map[string]string{
		"typed.md":  "---\ntype: note\n---\nTyped.\n",
		"page.html": "<html></html>",
		"notes.txt": "not markdown",
		".git/x.md": "# inside .git\n",
	})

	stdout, stderr, ok := runCLISplit(root, "init", "--name", "notes")

	if !ok {
		test.Fatalf("init failed: %s", stderr)
	}

	if strings.Contains(stdout.String(), "Found") {
		test.Errorf("stdout = %q; nothing untyped to adopt", stdout)
	}

	if loaded := loadInitManifest(test, root); loaded.Workspace.DefaultType != "" {
		test.Errorf("default-type = %q, want unset when there is nothing to adopt", loaded.Workspace.DefaultType)
	}
}
