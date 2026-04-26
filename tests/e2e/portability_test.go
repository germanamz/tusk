// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/portability"
)

// runTusk invokes the compiled binary with the given args and the supplied
// db path. Returns stdout, stderr, and any exec error so tests can assert
// on non-zero exit codes.
func runTusk(t *testing.T, dbPath string, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	full := append([]string{"--db", dbPath}, args...)
	cmd := exec.Command(binPath, full...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "TUSK_CONFIG_DIR="+t.TempDir())
	var sout, serr bytes.Buffer
	cmd.Stdout = &sout
	cmd.Stderr = &serr
	err = cmd.Run()
	return sout.String(), serr.String(), err
}

// mustRunTusk fails the test on non-zero exit. Returns stdout.
func mustRunTusk(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	out, errOut, err := runTusk(t, dbPath, "", args...)
	if err != nil {
		t.Fatalf("tusk %v: %v\nstderr: %s\nstdout: %s", args, err, errOut, out)
	}
	return out
}

// freshDBPath returns a path under t.TempDir() where the binary will create
// a brand-new SQLite file on first invocation.
func freshDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "tusk.db")
}

// decodeWorkspace reads a portability dump from the given path. The codec
// rejects unknown fields so a successful decode is a structural check too.
func decodeWorkspace(t *testing.T, path string) *portability.PortableWorkspace {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	ws, err := portability.Decode(f)
	if err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return ws
}

// stripVolatile zeroes the per-export timestamp and drops every
// workspace_imported event so two dumps taken across an import boundary
// compare equal under reflect-style equality.
func stripVolatile(ws *portability.PortableWorkspace) {
	ws.ExportedAt = time.Time{}
	kept := ws.Events[:0]
	for _, e := range ws.Events {
		if e.Type == "workspace_imported" {
			continue
		}
		kept = append(kept, e)
	}
	ws.Events = kept
}

func TestPortability_RoundTrip(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	srcDB := freshDBPath(t)
	mustRunTusk(t, srcDB, "task", "create", "first")
	mustRunTusk(t, srcDB, "task", "create", "second")
	mustRunTusk(t, srcDB, "task", "create", "third")

	dumpPath := filepath.Join(t.TempDir(), "ws.json")
	mustRunTusk(t, srcDB, "export", "--output", dumpPath)

	dstDB := freshDBPath(t)
	// Fresh DB is seeded with the default project + workflow on first open,
	// so a clean rehydrate uses --replace --truncate to wipe-and-restore.
	mustRunTusk(t, dstDB, "import", "--input", dumpPath, "--replace", "--truncate")

	rtPath := filepath.Join(t.TempDir(), "ws-rt.json")
	mustRunTusk(t, dstDB, "export", "--output", rtPath)

	srcWS := decodeWorkspace(t, dumpPath)
	rtWS := decodeWorkspace(t, rtPath)
	stripVolatile(srcWS)
	stripVolatile(rtWS)

	srcJSON, err := json.MarshalIndent(srcWS, "", "  ")
	if err != nil {
		t.Fatalf("marshaling source: %v", err)
	}
	rtJSON, err := json.MarshalIndent(rtWS, "", "  ")
	if err != nil {
		t.Fatalf("marshaling round-trip: %v", err)
	}
	if !bytes.Equal(srcJSON, rtJSON) {
		t.Fatalf("round-trip diverged.\nsource:\n%s\nrehydrated:\n%s", srcJSON, rtJSON)
	}
}

func TestPortability_StdinStdout(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	srcDB := freshDBPath(t)
	mustRunTusk(t, srcDB, "task", "create", "piped")

	exportOut, _, err := runTusk(t, srcDB, "", "export")
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	dstDB := freshDBPath(t)
	out, errOut, err := runTusk(t, dstDB, exportOut, "import", "--input", "-", "--replace", "--truncate")
	if err != nil {
		t.Fatalf("piped import failed: %v\nstderr: %s\nstdout: %s", err, errOut, out)
	}

	taskList := mustRunTusk(t, dstDB, "--format", "json", "task", "list")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(taskList), &rows); err != nil {
		t.Fatalf("decoding task list: %v\nraw: %s", err, taskList)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 task after piped import, got %d: %v", len(rows), rows)
	}
	if got, _ := rows[0]["title"].(string); got != "piped" {
		t.Fatalf("expected title \"piped\", got %q", got)
	}
}

func TestPortability_SchemaVersionError(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	stub := []byte(`{
  "schema_version": 999,
  "tusk_version": "test",
  "exported_at": "2026-04-26T00:00:00Z",
  "workflows": [], "projects": [], "players": [], "tags": [],
  "tasks": [], "relations": [], "annotations": [], "notes": [], "events": []
}`)
	stubPath := filepath.Join(t.TempDir(), "future.json")
	if err := os.WriteFile(stubPath, stub, 0o644); err != nil {
		t.Fatalf("writing stub: %v", err)
	}

	dbPath := freshDBPath(t)
	out, errOut, err := runTusk(t, dbPath, "", "import", "--input", stubPath)
	if err == nil {
		t.Fatalf("expected non-zero exit; stdout: %s\nstderr: %s", out, errOut)
	}
	if !strings.Contains(errOut, "999") || !strings.Contains(errOut, "1") {
		t.Fatalf("expected stderr to name dump version 999 and supported 1; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "[schema]") {
		t.Fatalf("expected [schema] tag in stderr; got:\n%s", errOut)
	}
}

func TestPortability_FKValidationError(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	srcDB := freshDBPath(t)
	mustRunTusk(t, srcDB, "task", "create", "with bad parent")

	dumpPath := filepath.Join(t.TempDir(), "ws.json")
	mustRunTusk(t, srcDB, "export", "--output", dumpPath)

	raw, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("reading dump: %v", err)
	}
	var ws map[string]any
	if err := json.Unmarshal(raw, &ws); err != nil {
		t.Fatalf("parsing dump: %v", err)
	}
	tasks := ws["tasks"].([]any)
	if len(tasks) == 0 {
		t.Fatalf("no tasks in dump")
	}
	tasks[0].(map[string]any)["parent_id"] = "00000000-0000-0000-0000-deadbeefdead"
	patched, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("re-encoding dump: %v", err)
	}
	badPath := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badPath, patched, 0o644); err != nil {
		t.Fatalf("writing bad dump: %v", err)
	}

	dstDB := freshDBPath(t)
	out, errOut, err := runTusk(t, dstDB, "", "import", "--input", badPath, "--replace", "--truncate")
	if err == nil {
		t.Fatalf("expected non-zero exit; stdout: %s\nstderr: %s", out, errOut)
	}
	if !strings.Contains(errOut, "[fk]") {
		t.Fatalf("expected [fk] kind in stderr; got:\n%s", errOut)
	}
}

func TestPortability_CollisionWithoutReplace(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	srcDB := freshDBPath(t)
	mustRunTusk(t, srcDB, "task", "create", "collidable")

	dumpPath := filepath.Join(t.TempDir(), "ws.json")
	mustRunTusk(t, srcDB, "export", "--output", dumpPath)

	dstDB := freshDBPath(t)
	mustRunTusk(t, dstDB, "import", "--input", dumpPath, "--replace", "--truncate")

	out, errOut, err := runTusk(t, dstDB, "", "import", "--input", dumpPath)
	if err == nil {
		t.Fatalf("expected collision exit; stdout: %s\nstderr: %s", out, errOut)
	}
	if !strings.Contains(errOut, "[collision]") {
		t.Fatalf("expected [collision] kind in stderr; got:\n%s", errOut)
	}
}

func TestPortability_DryRunDoesNotMutate(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	srcDB := freshDBPath(t)
	mustRunTusk(t, srcDB, "task", "create", "before")

	dumpPath := filepath.Join(t.TempDir(), "ws.json")
	mustRunTusk(t, srcDB, "export", "--output", dumpPath)

	dstDB := freshDBPath(t)
	mustRunTusk(t, dstDB, "import", "--input", dumpPath, "--replace", "--truncate")
	mustRunTusk(t, dstDB, "task", "create", "scratch")

	listBefore := mustRunTusk(t, dstDB, "--format", "json", "task", "list")
	var before []any
	if err := json.Unmarshal([]byte(listBefore), &before); err != nil {
		t.Fatalf("decoding before list: %v", err)
	}

	mustRunTusk(t, dstDB, "import", "--input", dumpPath, "--replace", "--dry-run")

	listAfter := mustRunTusk(t, dstDB, "--format", "json", "task", "list")
	var after []any
	if err := json.Unmarshal([]byte(listAfter), &after); err != nil {
		t.Fatalf("decoding after list: %v", err)
	}
	if len(before) != len(after) {
		t.Fatalf("dry-run mutated workspace: before=%d after=%d", len(before), len(after))
	}
}
