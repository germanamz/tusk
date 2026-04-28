// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/portability"
)

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

func mustRun(t *testing.T, env *Env, args ...string) Result {
	t.Helper()
	r := env.Run(args...)
	if r.Err != nil {
		t.Fatalf("tusk %v: %v\nstderr: %s\nstdout: %s", args, r.Err, r.Stderr, r.Stdout)
	}
	return r
}

func TestPortability_RoundTrip(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	src := newEnv(t, binPath, "flag", "json")
	mustRun(t, src, "task", "create", "first")
	mustRun(t, src, "task", "create", "second")
	mustRun(t, src, "task", "create", "third")

	dumpPath := filepath.Join(t.TempDir(), "ws.json")
	mustRun(t, src, "export", "--output", dumpPath)

	dst := newEnv(t, binPath, "flag", "json")
	// Fresh DB is seeded with the default project + workflow on first open,
	// so a clean rehydrate uses --replace --truncate to wipe-and-restore.
	mustRun(t, dst, "import", "--input", dumpPath, "--replace", "--truncate")

	rtPath := filepath.Join(t.TempDir(), "ws-rt.json")
	mustRun(t, dst, "export", "--output", rtPath)

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
	src := newEnv(t, binPath, "flag", "json")
	mustRun(t, src, "task", "create", "piped")

	exportRes := mustRun(t, src, "export")

	dst := newEnv(t, binPath, "flag", "json")
	dst.step = currentStep{stdin: exportRes.Stdout}
	importRes := dst.Run("import", "--input", "-", "--replace", "--truncate")
	dst.step = currentStep{}
	if importRes.Err != nil {
		t.Fatalf("piped import failed: %v\nstderr: %s\nstdout: %s", importRes.Err, importRes.Stderr, importRes.Stdout)
	}

	listRes := mustRun(t, dst, "task", "list")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(listRes.Stdout), &rows); err != nil {
		t.Fatalf("decoding task list: %v\nraw: %s", err, listRes.Stdout)
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

	env := newEnv(t, binPath, "flag", "text")
	r := env.Run("import", "--input", stubPath)
	if r.Err == nil {
		t.Fatalf("expected non-zero exit; stdout: %s\nstderr: %s", r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "999") || !strings.Contains(r.Stderr, "1") {
		t.Fatalf("expected stderr to name dump version 999 and supported 1; got:\n%s", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "[schema]") {
		t.Fatalf("expected [schema] tag in stderr; got:\n%s", r.Stderr)
	}
}

func TestPortability_FKValidationError(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	src := newEnv(t, binPath, "flag", "text")
	mustRun(t, src, "task", "create", "with bad parent")

	dumpPath := filepath.Join(t.TempDir(), "ws.json")
	mustRun(t, src, "export", "--output", dumpPath)

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

	dst := newEnv(t, binPath, "flag", "text")
	r := dst.Run("import", "--input", badPath, "--replace", "--truncate")
	if r.Err == nil {
		t.Fatalf("expected non-zero exit; stdout: %s\nstderr: %s", r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "[fk]") {
		t.Fatalf("expected [fk] kind in stderr; got:\n%s", r.Stderr)
	}
}

func TestPortability_CollisionWithoutReplace(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	src := newEnv(t, binPath, "flag", "text")
	mustRun(t, src, "task", "create", "collidable")

	dumpPath := filepath.Join(t.TempDir(), "ws.json")
	mustRun(t, src, "export", "--output", dumpPath)

	dst := newEnv(t, binPath, "flag", "text")
	mustRun(t, dst, "import", "--input", dumpPath, "--replace", "--truncate")

	r := dst.Run("import", "--input", dumpPath)
	if r.Err == nil {
		t.Fatalf("expected collision exit; stdout: %s\nstderr: %s", r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "[collision]") {
		t.Fatalf("expected [collision] kind in stderr; got:\n%s", r.Stderr)
	}
}

func TestPortability_DryRunDoesNotMutate(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newEnv(t, binPath, "flag", "json")
	mustRun(t, env, "task", "create", "before")

	dumpPath := filepath.Join(t.TempDir(), "ws.json")
	mustRun(t, env, "export", "--output", dumpPath)
	mustRun(t, env, "task", "create", "scratch")

	listBefore := mustRun(t, env, "task", "list")
	var before []any
	if err := json.Unmarshal([]byte(listBefore.Stdout), &before); err != nil {
		t.Fatalf("decoding before list: %v", err)
	}

	mustRun(t, env, "import", "--input", dumpPath, "--replace", "--dry-run")

	listAfter := mustRun(t, env, "task", "list")
	var after []any
	if err := json.Unmarshal([]byte(listAfter.Stdout), &after); err != nil {
		t.Fatalf("decoding after list: %v", err)
	}
	if len(before) != len(after) {
		t.Fatalf("dry-run mutated workspace: before=%d after=%d", len(before), len(after))
	}
}
