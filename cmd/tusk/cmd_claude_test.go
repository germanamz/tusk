package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/claudeplugin"
)

func setupClaudeWorkspace(test *testing.T) string {
	test.Helper()

	root := setupTempWorkspace(test)

	appendAliasBlock(test, root, `[node-types.note]

[node-types.decision]
`)
	createNode(test, root, "notes/alpha.md", "note", "Alpha", "")

	return root
}

func TestClaudeContext_OrientationAndDigest(test *testing.T) {
	root := setupClaudeWorkspace(test)

	appendAliasBlock(test, root, `[alias.everything]
command = "node list"

[context]
pinned  = ["notes/alpha"]
include = ["everything"]
`)
	stdout, stderr, ok := runCLISplit(root, "claude", "context")
	out := stdout.String()

	if !ok {
		test.Fatalf("CLI failed: %s", stderr)
	}

	for _, want := range []string{
		`This project is a tusk vault ("test")`,
		"Node types: note 1\n",
		"Aliases: everything (run with tusk_run)\n",
		"\n## Pinned\nnotes/alpha",
		"\n## Aliases / everything\n",
	} {
		if !strings.Contains(out, want) {
			test.Errorf("output missing %q:\n%s", want, out)
		}
	}

	if stderr.Len() != 0 {
		test.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestClaudeContext_MaxBytesTruncates(test *testing.T) {
	root := setupClaudeWorkspace(test)

	appendAliasBlock(test, root, `[context]
pinned    = ["notes/alpha"]
max-bytes = 150
`)
	stdout, stderr, ok := runCLISplit(root, "claude", "context")
	out := stdout.String()

	if !ok {
		test.Fatalf("CLI failed: %s", stderr)
	}

	if strings.Contains(out, "## Pinned") || !strings.Contains(out, "digest truncated at 150 bytes") {
		test.Errorf("want pinned dropped under a 150-byte budget:\n%s", out)
	}

	flagOut, flagStderr, flagOk := runCLISplit(root, "claude", "context", "--max-bytes", "100000")

	if !flagOk {
		test.Fatalf("CLI failed: %s", flagStderr)
	}

	if !strings.Contains(flagOut.String(), "## Pinned") {
		test.Errorf("--max-bytes did not override the manifest budget:\n%s", flagOut)
	}
}

func TestClaudeContext_OutsideWorkspacePrintsNothing(test *testing.T) {
	stdout, stderr, ok := runCLISplit(test.TempDir(), "claude", "context")

	if !ok || stdout.Len() != 0 || stderr.Len() != 0 {
		test.Errorf("outside a workspace: out=%q stderr=%q ok=%v, want empty output and success", stdout, stderr, ok)
	}
}

func TestClaudeContext_BrokenManifestPrintsNotice(test *testing.T) {
	root := setupTempWorkspace(test)

	appendAliasBlock(test, root, "[edge-types.broken]\nfrom = 1\n")

	stdout, stderr, ok := runCLISplit(root, "claude", "context")
	out := stdout.String()

	if !ok {
		test.Fatalf("CLI failed: %s", stderr)
	}

	if !strings.HasPrefix(out, "tusk.toml failed to load: ") || strings.Count(out, "\n") != 1 {
		test.Errorf("want a one-line load notice, got:\n%s", out)
	}
}

func TestClaudeContext_MissingIndexIsNotRebuilt(test *testing.T) {
	root := setupClaudeWorkspace(test)
	indexDir := filepath.Join(root, ".tusk")

	if removeErr := os.RemoveAll(indexDir); removeErr != nil {
		test.Fatal(removeErr)
	}

	stdout, stderr, ok := runCLISplit(root, "claude", "context")
	out := stdout.String()

	if !ok {
		test.Fatalf("CLI failed: %s", stderr)
	}

	for _, want := range []string{"Node types: decision · note\n", "Index: not built yet"} {
		if !strings.Contains(out, want) {
			test.Errorf("output missing %q:\n%s", want, out)
		}
	}

	if _, statErr := os.Stat(indexDir); !errors.Is(statErr, os.ErrNotExist) {
		test.Errorf("claude context created the index (%v); it must leave building to the MCP server", statErr)
	}
}

func TestClaudeContext_PluginVersionDriftWarnsOnStderr(test *testing.T) {
	root := setupClaudeWorkspace(test)

	stdout, stderr, ok := runCLISplit(root, "claude", "context", "--plugin-version", "v0.0.0-old")

	if !ok {
		test.Fatalf("CLI failed: %s", stderr)
	}

	if !strings.Contains(stderr.String(), "tusk plugin is v0.0.0-old") || !strings.Contains(stderr.String(), "run tusk claude install") {
		test.Errorf("stderr = %q, want a drift warning", stderr)
	}

	if !strings.Contains(stdout.String(), "This project is a tusk vault") {
		test.Errorf("drift suppressed the block:\n%s", stdout)
	}
}

func TestClaudeInstall_StatusUninstall(test *testing.T) {
	root := setupClaudeWorkspace(test)
	chdir(test, root)

	out, installErr := runCLI("claude", "install", "--bin", "tusk")

	if installErr != nil {
		test.Fatalf("install: %v\n%s", installErr, out)
	}

	for _, want := range []string{"Installed the tusk plugin for Claude Code", "approved the plugin's MCP server", "Start Claude Code from "} {
		if !strings.Contains(out, want) {
			test.Errorf("install output missing %q:\n%s", want, out)
		}
	}

	if _, statErr := os.Stat(filepath.Join(claudeplugin.PluginDir(root), "hooks", "register.ts")); statErr != nil {
		test.Fatalf("plugin not written: %v", statErr)
	}

	statusOut, statusErr := runCLI("claude", "status")

	if statusErr != nil {
		test.Fatalf("status: %v", statusErr)
	}

	for _, want := range []string{"(matches this binary)", "plugin:tusk:tusk approved"} {
		if !strings.Contains(statusOut, want) {
			test.Errorf("status output missing %q:\n%s", want, statusOut)
		}
	}

	uninstallOut, uninstallErr := runCLI("claude", "uninstall")

	if uninstallErr != nil {
		test.Fatalf("uninstall: %v\n%s", uninstallErr, uninstallOut)
	}

	statusOut, _ = runCLI("claude", "status")

	if !strings.Contains(statusOut, "not installed") || !strings.Contains(statusOut, "not approved") {
		test.Errorf("status after uninstall:\n%s", statusOut)
	}
}

func TestClaudeInstall_RefusesForeignPlugin(test *testing.T) {
	root := setupTempWorkspace(test)
	manifestPath := filepath.Join(claudeplugin.PluginDir(root), ".claude-plugin", "plugin.json")

	if mkdirErr := os.MkdirAll(filepath.Dir(manifestPath), 0o755); mkdirErr != nil {
		test.Fatal(mkdirErr)
	}

	if writeErr := os.WriteFile(manifestPath, []byte(`{"name": "mine"}`), 0o644); writeErr != nil {
		test.Fatal(writeErr)
	}

	chdir(test, root)

	_, installErr := runCLI("claude", "install")

	if installErr == nil || !strings.Contains(installErr.Error(), "--force") {
		test.Errorf("install over a foreign plugin = %v, want a --force hint", installErr)
	}
}

func TestClaudeInstall_NeedsWorkspace(test *testing.T) {
	chdir(test, test.TempDir())

	_, installErr := runCLI("claude", "install")

	if installErr == nil || !strings.Contains(installErr.Error(), "run tusk init first") {
		test.Errorf("install outside a workspace = %v, want a tusk init hint", installErr)
	}
}

func TestInit_ClaudeFlagInstallsPlugin(test *testing.T) {
	root := test.TempDir()
	chdir(test, root)

	out, initErr := runCLI("init", "--name", "brain", "--claude")

	if initErr != nil {
		test.Fatalf("init --claude: %v\n%s", initErr, out)
	}

	if !strings.Contains(out, "Initialized Tusk workspace") || !strings.Contains(out, "Installed the tusk plugin") {
		test.Errorf("init --claude output:\n%s", out)
	}

	if _, statErr := os.Stat(filepath.Join(claudeplugin.PluginDir(root), ".claude-plugin", "plugin.json")); statErr != nil {
		test.Errorf("plugin not written: %v", statErr)
	}
}
