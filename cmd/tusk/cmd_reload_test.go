package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// TestReload_ValidManifestBumpsEpoch_PrintsSchemaSummary verifies happy path:
// valid manifest bumps the manifest-epoch and prints the loaded schema summary
// (node-type/edge-type/behavior counts and names) plus the new epoch. A fresh
// CLI process has no previously-loaded manifest, so it reports the loaded schema,
// not an added/removed diff.
func TestReload_ValidManifestBumpsEpoch_PrintsSchemaSummary(test *testing.T) {
	root := test.TempDir()

	// Bootstrap: workspace, tusk.toml with minimal valid manifest, .tusk/
	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir .tusk: %v", mkErr)
	}

	manifestPath := filepath.Join(root, "tusk.toml")
	manifestContent := `
[workspace]
name = "test-reload"

[node-types.note]
properties = []
`
	if writeErr := os.WriteFile(manifestPath, []byte(manifestContent), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	// Capture output
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	// Create the command and override stdout/stderr
	cmd := newReloadCmd()
	cmd.SetOut(out)
	cmd.SetErr(errOut)

	// Simulate being in the workspace
	oldCwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldCwd) }()
	if cdErr := os.Chdir(root); cdErr != nil {
		test.Fatalf("chdir: %v", cdErr)
	}

	// Run with no args (no --reindex)
	cmd.SetArgs([]string{})
	runErr := cmd.Execute()

	if runErr != nil {
		test.Fatalf("Execute: %v, stderr: %s", runErr, errOut.String())
	}

	output := out.String()

	// Verify output reports the new manifest_epoch plus the loaded schema summary.
	// The summary lists the schema now in effect ("schema" object with node-type,
	// edge-type, and behavior names) — NOT an added/removed diff (a fresh CLI
	// process has no previous manifest to diff against).
	if !bytes.Contains(out.Bytes(), []byte("manifest_epoch")) {
		test.Fatalf("output missing 'manifest_epoch': %s", output)
	}
	if !bytes.Contains(out.Bytes(), []byte("schema")) {
		test.Fatalf("output missing 'schema' summary: %s", output)
	}
	if !bytes.Contains(out.Bytes(), []byte("node_types")) {
		test.Fatalf("output missing 'node_types' in schema summary: %s", output)
	}
	// The summary must name the loaded node-type ("note"), proving it reports the
	// loaded schema rather than empty diff lists.
	if !bytes.Contains(out.Bytes(), []byte("note")) {
		test.Fatalf("output missing loaded node-type 'note' in schema summary: %s", output)
	}
}

// TestReload_InvalidManifest_ExitsNonZero_NoBump verifies that a parse/structural
// error returns non-zero and does NOT bump the epoch.
func TestReload_InvalidManifest_ExitsNonZero_NoBump(test *testing.T) {
	root := test.TempDir()

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir .tusk: %v", mkErr)
	}

	manifestPath := filepath.Join(root, "tusk.toml")
	invalidContent := `
[workspace]
name = "test"

[node-types.note
// Unclosed bracket — syntax error
`
	if writeErr := os.WriteFile(manifestPath, []byte(invalidContent), 0o644); writeErr != nil {
		test.Fatalf("write invalid manifest: %v", writeErr)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	cmd := newReloadCmd()
	cmd.SetOut(out)
	cmd.SetErr(errOut)

	oldCwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldCwd) }()
	if cdErr := os.Chdir(root); cdErr != nil {
		test.Fatalf("chdir: %v", cdErr)
	}

	cmd.SetArgs([]string{})
	runErr := cmd.Execute()

	// Should error
	if runErr == nil {
		test.Fatalf("expected error on invalid manifest, got nil")
	}

	// Verify no epoch file was created (no bump)
	epochPath := filepath.Join(root, ".tusk", "manifest-epoch")
	if _, statErr := os.Stat(epochPath); statErr == nil {
		test.Fatalf("epoch file should not exist after validation failure, but found it")
	}
}

// TestReload_WithReindexFlag_RunsSyncReindex verifies that the --reindex flag
// triggers a synchronous reindex.Run and that the reindex actually executed
// (not just that the epoch bumped). It seeds a valid on-disk index plus a node
// file, runs with --verbose so the "reindex completed" Info line surfaces on
// stderr, and asserts the indexed/removed/skipped counts appear.
func TestReload_WithReindexFlag_RunsSyncReindex(test *testing.T) {
	root := test.TempDir()

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir .tusk: %v", mkErr)
	}

	manifestPath := filepath.Join(root, "tusk.toml")
	manifestContent := `
[workspace]
name = "test-reindex"

[node-types.note]
properties = []
`
	if writeErr := os.WriteFile(manifestPath, []byte(manifestContent), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	// Seed a node file on disk so the reindex walk has content to index.
	noteContent := `+++
type = "note"
id = "n1"
+++

# A note
`
	if writeErr := os.WriteFile(filepath.Join(root, "n1.md"), []byte(noteContent), 0o644); writeErr != nil {
		test.Fatalf("write node file: %v", writeErr)
	}

	// Seed a valid (empty, schema-current) index so reindex.Run can open and
	// populate it rather than skipping on a missing store.
	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))
	if openErr != nil {
		test.Fatalf("seed index.Open: %v", openErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		test.Fatalf("seed index.Close: %v", closeErr)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	cmd := newReloadCmd()
	cmd.SetOut(out)
	cmd.SetErr(errOut)

	oldCwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldCwd) }()
	if cdErr := os.Chdir(root); cdErr != nil {
		test.Fatalf("chdir: %v", cdErr)
	}

	// --verbose drops the logger to Debug so the Info-level "reindex completed"
	// line (which carries the counts) is emitted to stderr.
	cmd.SetArgs([]string{"--reindex", "--verbose"})
	runErr := cmd.Execute()

	if runErr != nil {
		test.Fatalf("Execute with --reindex: %v, stderr: %s", runErr, errOut.String())
	}

	// Verify manifest_epoch was bumped (stdout schema summary).
	if !bytes.Contains(out.Bytes(), []byte("manifest_epoch")) {
		test.Fatalf("output missing 'manifest_epoch' after --reindex: %s", out.String())
	}

	// Prove the reindex actually ran: the "reindex completed" log line carries
	// indexed/removed/skipped counts on stderr.
	stderr := errOut.String()
	if !bytes.Contains(errOut.Bytes(), []byte("reindex completed")) {
		test.Fatalf("stderr missing 'reindex completed' (reindex did not run): %s", stderr)
	}
	for _, field := range []string{"indexed", "removed", "skipped"} {
		if !bytes.Contains(errOut.Bytes(), []byte(field)) {
			test.Fatalf("stderr missing reindex count %q: %s", field, stderr)
		}
	}
}
