package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeWorkspaceFile(test *testing.T, root, relPath, content string) {
	test.Helper()

	absPath := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(absPath, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", relPath, writeErr)
	}
}

func setupPathRefsWorkspace(test *testing.T) string {
	test.Helper()

	root := setupTempWorkspace(test)

	appendAliasBlock(test, root, `[node-types.technical]

[edge-types.describes]
from = ["technical"]
paths = true
`)
	writeWorkspaceFile(test, root, "server/ledger/core/service.go", "package core\n")
	writeWorkspaceFile(test, root, "server/wire.go", "package server\n")
	writeWorkspaceFile(test, root, "technical/ledger.md",
		"---\ntype: technical\ntitle: Ledger module\n---\n\n# Ledger\n\n## Core service\n\nLives in `server/ledger/core/service.go`,\nsee `server/ledger/core/service.go:12`.\n")
	writeWorkspaceFile(test, root, "technical/arch.md",
		"---\ntype: technical\ntitle: Architecture\n---\n\n# Modules\n\nThe ledger is `server/ledger/`.\n")

	if _, stderr, ok := runCLISplit(root, "reindex"); !ok {
		test.Fatalf("reindex: %s", stderr)
	}

	return root
}

func TestClaudeRefs_ListsPagesNamingTheEditedFile(test *testing.T) {
	root := setupPathRefsWorkspace(test)

	stdout, stderr, ok := runCLISplit(root, "claude", "refs", filepath.Join(root, "server/ledger/core/service.go"))

	if !ok {
		test.Fatalf("claude refs: %s", stderr)
	}

	want := `tusk: 2 pages name server/ledger/core/service.go. Check whether this edit changes what they say.
- technical/ledger "Ledger module": lines 10-11 (Core service)
- technical/arch "Architecture": line 8 (Modules), via server/ledger/
`

	if got := stdout.String(); got != want {
		test.Errorf("claude refs =\n%s\nwant\n%s", got, want)
	}
}

func TestClaudeRefs_PrintsNothingWhenNothingApplies(test *testing.T) {
	root := setupPathRefsWorkspace(test)

	for _, path := range []string{
		filepath.Join(root, "server/wire.go"),         // named by no page
		"server/wire.go",                              // relative, same
		filepath.Join(test.TempDir(), "elsewhere.go"), // outside the workspace
	} {
		stdout, stderr, ok := runCLISplit(root, "claude", "refs", path)

		if !ok || stdout.Len() != 0 || stderr.Len() != 0 {
			test.Errorf("claude refs %s: ok=%v stdout=%q stderr=%q, want silence", path, ok, stdout, stderr)
		}
	}

	bare := setupTempWorkspace(test)
	writeWorkspaceFile(test, bare, "server/a.go", "package server\n")

	if stdout, _, ok := runCLISplit(bare, "claude", "refs", "server/a.go"); !ok || stdout.Len() != 0 {
		test.Errorf("a workspace with no paths edge type printed %q", stdout)
	}
}
