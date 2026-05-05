package main

import (
	"bytes"
	"os"
	"path/filepath"
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
