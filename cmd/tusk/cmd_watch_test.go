package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestWatchCmd_HelpRenders(test *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"watch", "--help"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Errorf("watch --help: %v", execErr)
	}
}

func TestWatchCmd_WorkersZeroEarlyExit(test *testing.T) {
	wsDir := setupTempWorkspace(test)
	chdir(test, wsDir)

	test.Setenv("TUSK_EMBED_WORKERS", "0")

	stderr := &bytes.Buffer{}
	rootCmd := newRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs([]string{"watch"})

	runErr := rootCmd.Execute()

	if runErr == nil {
		test.Fatalf("expected error when workers=0; got nil")
	}

	if !strings.Contains(runErr.Error(), "embed workers disabled") {
		test.Errorf("error %q does not contain %q", runErr.Error(), "embed workers disabled")
	}
}

// writeWatchTestFile writes relPath (workspace-relative) under root, creating
// parent dirs. A small helper for the A4 drift tests.
func writeWatchTestFile(test *testing.T, root, relPath, body string) {
	test.Helper()

	abs := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatalf("mkdir %s: %v", relPath, mkErr)
	}

	if writeErr := os.WriteFile(abs, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", relPath, writeErr)
	}
}

// runWatchOnce runs `tusk watch` with a pre-cancelled context: the initial
// reindex runs synchronously (Async=false), then watcher.Run returns on
// ctx-done. This exercises the real cmd_watch reindex config path.
func runWatchOnce(test *testing.T, wsDir string) {
	test.Helper()

	chdir(test, wsDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rootCmd := newRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetContext(ctx)
	rootCmd.SetArgs([]string{"watch"})

	_ = rootCmd.Execute() // returns on ctx-done after the initial reindex
}

const personRequiredEmailManifest = `[workspace]
name = "test"

[node-types.person]
properties = [
    { name = "email", type = "string", required = true },
]
`

// TestWatchCmd_RecordsPropertyDriftOnInitialReindex pins the A4 fix: a
// watch-triggered reindex must run the property validator and record drift.
// Before A4, cmd_watch's reindex config omitted NodeTypes/PropertyDrift, so
// `tusk watch` indexed content but never recorded (or cleared) doctor drift.
func TestWatchCmd_RecordsPropertyDriftOnInitialReindex(test *testing.T) {
	wsDir := test.TempDir()

	writeWatchTestFile(test, wsDir, "tusk.toml", personRequiredEmailManifest)
	// A person node missing the required "email" property → required-missing drift.
	writeWatchTestFile(test, wsDir, "people/alice.md", "---\ntype: person\ntitle: Alice\n---\n\nbody\n")

	runWatchOnce(test, wsDir)

	idx, idxErr := index.Open(filepath.Join(wsDir, ".tusk", "index.db"))

	if idxErr != nil {
		test.Fatalf("open index: %v", idxErr)
	}

	defer idx.Close()

	rows, listErr := index.NewPropertyDriftRepo(idx).ListAll()

	if listErr != nil {
		test.Fatalf("ListAll: %v", listErr)
	}

	found := false

	for _, row := range rows {
		if row.Property == "email" && row.Kind == "required-missing" {
			found = true
		}
	}

	if !found {
		test.Errorf("expected a required-missing property-drift row for email; got %+v", rows)
	}
}

// TestWatchCmd_ClearsFixedPropertyDriftOnInitialReindex pins the other
// direction: a watch-triggered reindex over a now-valid node must clear a stale
// drift row. Before A4 the clearing path was gated off too.
func TestWatchCmd_ClearsFixedPropertyDriftOnInitialReindex(test *testing.T) {
	wsDir := test.TempDir()

	writeWatchTestFile(test, wsDir, "tusk.toml", personRequiredEmailManifest)
	// A valid person node — has the required "email".
	writeWatchTestFile(test, wsDir, "people/alice.md", "---\ntype: person\ntitle: Alice\nemail: alice@example.com\n---\n\nbody\n")

	// Seed a stale drift row as if a prior pass had flagged alice, then close the
	// handle so the watch command can take the index lock.
	seed, seedErr := index.Open(filepath.Join(wsDir, ".tusk", "index.db"))

	if seedErr != nil {
		test.Fatalf("open index (seed): %v", seedErr)
	}

	if appendErr := index.NewPropertyDriftRepo(seed).Append(index.PropertyDriftRow{
		NodeID:   "people/alice",
		NodeType: "person",
		Kind:     "required-missing",
		Property: "email",
		Details:  "stale",
	}); appendErr != nil {
		test.Fatalf("seed drift: %v", appendErr)
	}

	seed.Close()

	runWatchOnce(test, wsDir)

	idx, idxErr := index.Open(filepath.Join(wsDir, ".tusk", "index.db"))

	if idxErr != nil {
		test.Fatalf("open index: %v", idxErr)
	}

	defer idx.Close()

	rows, listErr := index.NewPropertyDriftRepo(idx).ListAll()

	if listErr != nil {
		test.Fatalf("ListAll: %v", listErr)
	}

	for _, row := range rows {
		if row.NodeID == "people/alice" {
			test.Errorf("expected stale drift for people/alice to be cleared by a clean watch reindex; got %+v", rows)
		}
	}
}

func TestWatchCmd_VerboseEmitsInitialReindexLogs(test *testing.T) {
	wsDir := setupTempWorkspace(test)
	chdir(test, wsDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so signal.NotifyContext fires Done() immediately

	stderr := &bytes.Buffer{}
	rootCmd := newRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(stderr)
	rootCmd.SetContext(ctx)
	rootCmd.SetArgs([]string{"watch", "--verbose"})

	_ = rootCmd.Execute() // watcher.Run returns nil on ctx-done; ignore any error from the post-cancel teardown

	if !strings.Contains(stderr.String(), `msg="reindex walk complete"`) {
		test.Errorf("expected walk-complete log on stderr; got %q", stderr.String())
	}
}
