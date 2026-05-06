package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeCreateCmd_WritesFile(test *testing.T) {
	tmpDir := initWorkspace(test)

	output := &bytes.Buffer{}

	rootCmd := newRootCmd()
	rootCmd.SetOut(output)
	rootCmd.SetErr(output)
	rootCmd.SetArgs([]string{"node", "create", "--type", "note", "--title", "Hello", "--path", "notes/hello.md"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("Execute: %v\noutput: %s", execErr, output.String())
	}

	body, readErr := os.ReadFile(filepath.Join(tmpDir, "notes/hello.md"))

	if readErr != nil {
		test.Fatalf("read: %v", readErr)
	}

	if !bytes.Contains(body, []byte("type: note")) || !bytes.Contains(body, []byte("title: Hello")) {
		test.Errorf("body missing expected frontmatter:\n%s", string(body))
	}
}

func initWorkspaceWithManifest(test *testing.T, manifestBody string) string {
	test.Helper()

	root := initWorkspace(test)

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	return root
}

func initWorkspace(test *testing.T) string {
	test.Helper()

	tmpDir := test.TempDir()
	originalCwd, getCwdErr := os.Getwd()

	if getCwdErr != nil {
		test.Fatalf("Getwd: %v", getCwdErr)
	}

	test.Cleanup(func() { os.Chdir(originalCwd) })

	if chdirErr := os.Chdir(tmpDir); chdirErr != nil {
		test.Fatalf("Chdir: %v", chdirErr)
	}

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "test"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("init: %v", execErr)
	}

	return tmpDir
}

func TestNodeCreate_WorkflowNonInitialRejected(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	_, stderr, ok := runCLISplit(root, "node", "create", "--type", "ticket", "--prop", "status=active", "--path", "tickets/foo.md")

	if ok {
		test.Errorf("exit 0, want non-zero")
	}

	if !strings.Contains(stderr.String(), "non-initial-on-create") && !strings.Contains(stderr.String(), "initial state") {
		test.Errorf("stderr = %q, want mention of non-initial-on-create or initial state", stderr.String())
	}
}

func TestNodeCreate_WorkflowInitialAccepted(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	stdout, _, ok := runCLISplit(root, "node", "create", "--type", "ticket", "--prop", "status=pending", "--path", "tickets/foo.md")

	if !ok {
		test.Errorf("exit non-zero, want 0")
	}

	if !strings.Contains(stdout.String(), "tickets/foo") {
		test.Errorf("stdout = %q, want path mention", stdout.String())
	}
}
