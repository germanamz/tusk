package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAliasManifest appends the given alias block to an existing tusk.toml
// at root (created by setupTempWorkspace). Used by the run-command tests.
func appendAliasBlock(test *testing.T, root, block string) {
	test.Helper()

	manifestPath := filepath.Join(root, "tusk.toml")

	body, readErr := os.ReadFile(manifestPath)

	if readErr != nil {
		test.Fatalf("read manifest: %v", readErr)
	}

	combined := string(body) + "\n" + block

	if writeErr := os.WriteFile(manifestPath, []byte(combined), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}
}

func TestRun_List_PrintsAliases(test *testing.T) {
	root := setupTempWorkspace(test)
	appendAliasBlock(test, root, `[alias.everything]
command = "status"
description = "Quick health snapshot"
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("run", "--list")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "everything") {
		test.Errorf("stdout missing alias name 'everything':\n%s", out)
	}

	if !strings.Contains(out, "status") {
		test.Errorf("stdout missing command 'status':\n%s", out)
	}

	if !strings.Contains(out, "Quick health snapshot") {
		test.Errorf("stdout missing description:\n%s", out)
	}
}

func TestRun_DispatchesStatusAlias(test *testing.T) {
	root := setupTempWorkspace(test)
	appendAliasBlock(test, root, `[alias.snap]
command = "status"
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("run", "snap", "--format", "compact")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "nodes by type") {
		test.Errorf("stdout missing 'nodes by type' header:\n%s", out)
	}
}

func TestRun_JSON_EnvelopeShape(test *testing.T) {
	root := setupTempWorkspace(test)
	appendAliasBlock(test, root, `[alias.snap]
command = "status"
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("run", "snap", "--json")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	var envelope map[string]any

	if unmarshalErr := json.Unmarshal([]byte(out), &envelope); unmarshalErr != nil {
		test.Fatalf("Unmarshal: %v\n%s", unmarshalErr, out)
	}

	if envelope["alias"] != "snap" {
		test.Errorf("alias = %v, want snap", envelope["alias"])
	}

	if envelope["command"] != "status" {
		test.Errorf("command = %v, want status", envelope["command"])
	}

	if envelope["kind"] != "status" {
		test.Errorf("kind = %v, want status", envelope["kind"])
	}

	if envelope["result"] == nil {
		test.Errorf("result = nil; want object")
	}
}

func TestRun_UnknownAlias_ReportsError(test *testing.T) {
	root := setupTempWorkspace(test)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("run", "nope")

	if runErr == nil {
		test.Fatalf("expected error, got nil: %s", out)
	}

	if !strings.Contains(runErr.Error(), "not declared") {
		test.Errorf("error = %v, want mention of 'not declared'", runErr)
	}
}

func TestRun_InvalidAlias_ReportsAliasError(test *testing.T) {
	root := setupTempWorkspace(test)
	appendAliasBlock(test, root, `[alias.bad]
command = "no-such-verb"
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("run", "bad")

	if runErr == nil {
		test.Fatalf("expected error, got nil: %s", out)
	}

	if !strings.Contains(runErr.Error(), "invalid") {
		test.Errorf("error = %v, want mention of 'invalid'", runErr)
	}
}

// TestRun_DoctorAlias_AcquiresLock confirms that dispatching a `doctor`
// alias through `tusk run` succeeds end-to-end. The dispatch path wraps
// the store-open and doctor.RunWithMigration call inside
// withWorkspaceLock; this test exercises that wrapper to catch
// regressions that would skip the lock acquisition (doctor.Migrate
// mutates source files and requires it).
func TestRun_DoctorAlias_AcquiresLock(test *testing.T) {
	root := setupTempWorkspace(test)
	appendAliasBlock(test, root, `[alias.health]
command = "doctor"
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("run", "health", "--json")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	var envelope map[string]any

	if unmarshalErr := json.Unmarshal([]byte(out), &envelope); unmarshalErr != nil {
		test.Fatalf("Unmarshal: %v\n%s", unmarshalErr, out)
	}

	if envelope["kind"] != "doctor" {
		test.Errorf("kind = %v, want doctor", envelope["kind"])
	}

	if envelope["command"] != "doctor" {
		test.Errorf("command = %v, want doctor", envelope["command"])
	}
}

func TestRun_QueryAliasWithMinScore(test *testing.T) {
	root := setupTempWorkspace(test)
	appendAliasBlock(test, root, `[alias.semantic]
command = "query"
args.filter = "type=note"
args.min-score = 0.7
`)

	chdir(test, root)
	defer chdir(test, "")

	// Just confirm validation passes — surfaced via doctor's alias_errors
	// section. The dispatch itself would need an embedder; we only need
	// to verify the alias is accepted as valid.
	out, runErr := runCLI("doctor")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if strings.Contains(out, "semantic:") {
		test.Errorf("doctor reported semantic alias as invalid:\n%s", out)
	}
}

func TestDoctor_ReportsAliasErrors(test *testing.T) {
	root := setupTempWorkspace(test)
	appendAliasBlock(test, root, `[alias.bad]
command = "no-such-verb"
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("doctor")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "aliases:") {
		test.Errorf("stdout missing 'aliases:' header:\n%s", out)
	}

	if !strings.Contains(out, "bad:") {
		test.Errorf("stdout missing alias name 'bad':\n%s", out)
	}

	if !strings.Contains(out, "unknown verb") {
		test.Errorf("stdout missing 'unknown verb' message:\n%s", out)
	}
}
