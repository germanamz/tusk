package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
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

func TestPackAdd_TagsPackEndToEnd(test *testing.T) {
	dir := test.TempDir()

	// Resolve the repo-local pack file BEFORE chdir-ing — runtime.Caller
	// returns this test file's absolute path, so we don't depend on cwd.
	packPath := filepath.Join(testSourceDir(test), "..", "..", "packs", "tags.toml")

	if _, statErr := os.Stat(packPath); statErr != nil {
		test.Fatalf("packs/tags.toml not found at %s: %v", packPath, statErr)
	}

	chdir(test, dir)

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "tags-smoke"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("init: %v", execErr)
	}

	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "file://" + packPath})

	var stdout, stderr bytes.Buffer

	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("pack add: %v\nstderr: %s", execErr, stderr.String())
	}

	manifestBody, readErr := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if readErr != nil {
		test.Fatalf("read tusk.toml: %v", readErr)
	}

	if !strings.Contains(string(manifestBody), "[node-types.tag]") {
		test.Errorf("tusk.toml missing [node-types.tag]: %q", manifestBody)
	}

	if !strings.Contains(string(manifestBody), "[edge-types.tagged]") {
		test.Errorf("tusk.toml missing [edge-types.tagged]: %q", manifestBody)
	}

	// Create two tag nodes.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"node", "create", "--type", "tag", "--title", "auth", "--path", "tag/auth.md"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("create tag/auth: %v", execErr)
	}

	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"node", "create", "--type", "tag", "--title", "security", "--path", "tag/security.md"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("create tag/security: %v", execErr)
	}

	// Add a tagged edge between them (many-to-many semantic — works tag-to-tag too).
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"edge", "add", "--type", "tagged", "--source", "tag/auth", "--target", "tag/security"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("add tagged edge: %v", execErr)
	}

	// Verify the edge via `edge list`.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"edge", "list", "--from", "tag/auth"})

	var listStdout bytes.Buffer

	rootCmd.SetOut(&listStdout)
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge list: %v", execErr)
	}

	if !strings.Contains(listStdout.String(), "tagged") || !strings.Contains(listStdout.String(), "tag/security") {
		test.Errorf("edge list output missing expected edge: %q", listStdout.String())
	}
}

// testSourceDir returns the absolute directory of this test source file.
// runtime.Caller(0) returns the caller's file path; calling it from a helper
// in cmd/tusk/cmd_pack_add_test.go yields <repo>/cmd/tusk, which is what we
// need to resolve <repo>/packs/tags.toml regardless of the test's cwd.
func testSourceDir(test *testing.T) string {
	test.Helper()

	_, callerFile, _, ok := runtime.Caller(0)

	if !ok {
		test.Fatal("runtime.Caller failed")
	}

	return filepath.Dir(callerFile)
}
