package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackAdd_HappyFileURL(test *testing.T) {
	dir := test.TempDir()

	chdir(test, dir)

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "test"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("init: %v", execErr)
	}

	packPath := filepath.Join(dir, "pack.toml")
	os.WriteFile(packPath, []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }]
`), 0o644)

	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "file://" + packPath})

	var stdout, stderr bytes.Buffer

	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("pack add: %v", execErr)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if !strings.Contains(string(body), "[node-types.task]") {
		test.Errorf("tusk.toml = %q", body)
	}
}

func TestPackAdd_UnknownNameFails(test *testing.T) {
	dir := test.TempDir()
	chdir(test, dir)

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "test"})
	rootCmd.Execute()

	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "not-a-pack"})

	var stderr bytes.Buffer

	rootCmd.SetErr(&stderr)

	execErr := rootCmd.Execute()

	if execErr == nil {
		test.Fatal("expected error")
	}

	if !strings.Contains(execErr.Error(), "unknown pack name") {
		test.Errorf("err = %v", execErr)
	}
}

func TestPackAdd_RejectsCollisionWithoutForce(test *testing.T) {
	dir := test.TempDir()
	chdir(test, dir)

	os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(`
[workspace]
name = "test"

[node-types.task]
properties = [{ name = "summary", type = "string" }]
`), 0o644)

	packPath := filepath.Join(dir, "pack.toml")
	os.WriteFile(packPath, []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }, { name = "priority", type = "int" }]
`), 0o644)

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "file://" + packPath})
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr == nil {
		test.Fatal("expected collision error")
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if strings.Contains(string(body), "priority") {
		test.Errorf("manifest unexpectedly mutated: %q", body)
	}
}

func TestPackAdd_ForceOverwrites(test *testing.T) {
	dir := test.TempDir()
	chdir(test, dir)

	os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(`
[workspace]
name = "test"

[node-types.task]
properties = [{ name = "summary", type = "string" }]

[node-types.note]
properties = [{ name = "summary", type = "string" }]
`), 0o644)

	packPath := filepath.Join(dir, "pack.toml")
	os.WriteFile(packPath, []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }, { name = "priority", type = "int" }]
`), 0o644)

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "--force", "file://" + packPath})
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("pack add --force: %v", execErr)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	// task section is replaced (priority appears now).
	if !strings.Contains(string(body), "priority") {
		test.Errorf("expected pack content with priority, got %q", body)
	}

	// note section is preserved.
	if !strings.Contains(string(body), "[node-types.note]") {
		test.Errorf("--force should not touch unrelated sections: %q", body)
	}
}
