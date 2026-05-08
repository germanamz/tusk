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

func TestPackAdd_KanbanPackEndToEnd(test *testing.T) {
	dir := test.TempDir()

	// Resolve the repo-local pack file BEFORE chdir-ing.
	packPath := filepath.Join(testSourceDir(test), "..", "..", "packs", "kanban.toml")

	if _, statErr := os.Stat(packPath); statErr != nil {
		test.Fatalf("packs/kanban.toml not found at %s: %v", packPath, statErr)
	}

	chdir(test, dir)

	// 1. tusk init.
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "kanban-smoke"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("init: %v", execErr)
	}

	// 2. tusk pack add file://<packs/kanban.toml>.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "file://" + packPath})

	var addStdout, addStderr bytes.Buffer

	rootCmd.SetOut(&addStdout)
	rootCmd.SetErr(&addStderr)

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("pack add: %v\nstderr: %s", execErr, addStderr.String())
	}

	manifestBody, readErr := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if readErr != nil {
		test.Fatalf("read tusk.toml: %v", readErr)
	}

	for _, want := range []string{
		"[node-types.ticket]",
		"[edge-types.parent]",
		"[edge-types.blocks]",
		"[behaviors.workflow.kanban]",
	} {
		if !strings.Contains(string(manifestBody), want) {
			test.Errorf("tusk.toml missing %s: %q", want, manifestBody)
		}
	}

	// 3. Create a parent ticket in the "pending" state.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{
		"node", "create",
		"--type", "ticket",
		"--title", "Parent ticket",
		"--path", "tickets/parent.md",
		"--prop", "status=pending",
		"--prop", "priority=high",
	})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("create tickets/parent: %v", execErr)
	}

	// 4. Create a child ticket.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{
		"node", "create",
		"--type", "ticket",
		"--title", "Child ticket",
		"--path", "tickets/child.md",
		"--prop", "status=pending",
	})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("create tickets/child: %v", execErr)
	}

	// 5. Add a parent edge child -> parent.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{
		"edge", "add",
		"--type", "parent",
		"--source", "tickets/child",
		"--target", "tickets/parent",
	})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge add parent: %v", execErr)
	}

	// 6. Verify the parent edge is indexed via `edge list --from tickets/child`.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"edge", "list", "--from", "tickets/child"})

	var listStdout bytes.Buffer

	rootCmd.SetOut(&listStdout)
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge list: %v", execErr)
	}

	if !strings.Contains(listStdout.String(), "parent") || !strings.Contains(listStdout.String(), "tickets/parent") {
		test.Errorf("edge list output missing expected parent edge: %q", listStdout.String())
	}

	// 7. Workflow transition pending -> active on the parent ticket. Legal.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"node", "modify", "tickets/parent", "--prop", "status=active"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("modify pending->active: %v", execErr)
	}

	// 8. Workflow transition active -> completed. Legal.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"node", "modify", "tickets/parent", "--prop", "status=completed"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("modify active->completed: %v", execErr)
	}

	// 9. Negative — workflow rejects pending -> completed (skip-state).
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"node", "modify", "tickets/child", "--prop", "status=completed"})

	var negStdout, negStderr bytes.Buffer

	rootCmd.SetOut(&negStdout)
	rootCmd.SetErr(&negStderr)

	negErr := rootCmd.Execute()

	if negErr == nil {
		test.Fatalf("expected pending->completed to fail; stdout=%q stderr=%q", negStdout.String(), negStderr.String())
	}
}

func TestPackAdd_VaultPackEndToEnd(test *testing.T) {
	dir := test.TempDir()

	// Resolve the repo-local pack file BEFORE chdir-ing.
	packPath := filepath.Join(testSourceDir(test), "..", "..", "packs", "vault.toml")

	if _, statErr := os.Stat(packPath); statErr != nil {
		test.Fatalf("packs/vault.toml not found at %s: %v", packPath, statErr)
	}

	chdir(test, dir)

	// 1. tusk init.
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "vault-smoke"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("init: %v", execErr)
	}

	// 2. tusk pack add file://<packs/vault.toml>.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "file://" + packPath})

	var addStdout, addStderr bytes.Buffer

	rootCmd.SetOut(&addStdout)
	rootCmd.SetErr(&addStderr)

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("pack add: %v\nstderr: %s", execErr, addStderr.String())
	}

	manifestBody, readErr := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if readErr != nil {
		test.Fatalf("read tusk.toml: %v", readErr)
	}

	for _, want := range []string{
		"[node-types.note]",
		"[node-types.meeting]",
		"[node-types.decision]",
		"[edge-types.references]",
		"[edge-types.relates-to]",
	} {
		if !strings.Contains(string(manifestBody), want) {
			test.Errorf("tusk.toml missing %s: %q", want, manifestBody)
		}
	}

	// 3. Create one of each node type.
	for _, args := range [][]string{
		{"node", "create", "--type", "note", "--title", "Auth RFC", "--path", "notes/auth-rfc.md"},
		{"node", "create", "--type", "meeting", "--title", "Standup", "--path", "meetings/standup.md"},
		{"node", "create", "--type", "decision", "--title", "Use JWT", "--path", "decisions/jwt.md"},
	} {
		rootCmd = newRootCmd()
		rootCmd.SetArgs(args)
		rootCmd.SetOut(new(bytes.Buffer))
		rootCmd.SetErr(new(bytes.Buffer))

		if execErr := rootCmd.Execute(); execErr != nil {
			test.Fatalf("create %v: %v", args, execErr)
		}
	}

	// 4. Externally write a second note containing a body wikilink.
	external := filepath.Join(dir, "notes/refback.md")

	body := []byte(`---
type: note
title: Backreference
---

This references [[notes/auth-rfc]] in the body.
`)

	if writeErr := os.WriteFile(external, body, 0o644); writeErr != nil {
		test.Fatalf("write external: %v", writeErr)
	}

	// 5. tusk reindex — picks up the wikilink and materializes a `references` edge.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"reindex"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	// 6. Verify the references edge is present.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"edge", "list", "--from", "notes/refback"})

	var refbackStdout bytes.Buffer

	rootCmd.SetOut(&refbackStdout)
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge list refback: %v", execErr)
	}

	if !strings.Contains(refbackStdout.String(), "references") || !strings.Contains(refbackStdout.String(), "notes/auth-rfc") {
		test.Errorf("edge list output missing references→notes/auth-rfc: %q", refbackStdout.String())
	}

	// 7. Add a relates-to edge between the decision and the note.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{
		"edge", "add",
		"--type", "relates-to",
		"--source", "decisions/jwt",
		"--target", "notes/auth-rfc",
	})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge add relates-to: %v", execErr)
	}

	// 8. Verify the relates-to edge is present.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"edge", "list", "--from", "decisions/jwt"})

	var jwtStdout bytes.Buffer

	rootCmd.SetOut(&jwtStdout)
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge list decisions/jwt: %v", execErr)
	}

	if !strings.Contains(jwtStdout.String(), "relates-to") || !strings.Contains(jwtStdout.String(), "notes/auth-rfc") {
		test.Errorf("edge list output missing relates-to→notes/auth-rfc: %q", jwtStdout.String())
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
