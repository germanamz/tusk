package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
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
