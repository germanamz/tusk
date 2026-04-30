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
func decodeWorkspace(test *testing.T, path string) *portability.PortableWorkspace {
	test.Helper()
	fileHandle, openErr := os.Open(path)

	if openErr != nil {
		test.Fatalf("opening %s: %v", path, openErr)
	}

	defer fileHandle.Close()
	ws, decodeErr := portability.Decode(fileHandle)

	if decodeErr != nil {
		test.Fatalf("decoding %s: %v", path, decodeErr)
	}

	return ws
}

// stripVolatile zeroes the per-export timestamp and drops every
// workspace_imported event so two dumps taken across an import boundary
// compare equal under reflect-style equality.
func stripVolatile(ws *portability.PortableWorkspace) {
	ws.ExportedAt = time.Time{}
	kept := ws.Events[:0]
	for _, event := range ws.Events {
		if event.Type == "workspace_imported" {
			continue
		}
		kept = append(kept, event)
	}
	ws.Events = kept
}

func mustRun(test *testing.T, env *Env, args ...string) Result {
	test.Helper()
	result := env.Run(args...)
	if result.Err != nil {
		test.Fatalf("tusk %v: %v\nstderr: %s\nstdout: %s", args, result.Err, result.Stderr, result.Stdout)
	}
	return result
}

func TestPortability_RoundTrip(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	src := newEnv(test, binPath, "flag", "json")
	mustRun(test, src, "task", "create", "first")
	mustRun(test, src, "task", "create", "second")
	mustRun(test, src, "task", "create", "third")

	dumpPath := filepath.Join(test.TempDir(), "ws.json")
	mustRun(test, src, "export", "--output", dumpPath)

	dst := newEnv(test, binPath, "flag", "json")
	// Fresh DB is seeded with the default project + workflow on first open,
	// so a clean rehydrate uses --replace --truncate to wipe-and-restore.
	mustRun(test, dst, "import", "--input", dumpPath, "--replace", "--truncate")

	rtPath := filepath.Join(test.TempDir(), "ws-rt.json")
	mustRun(test, dst, "export", "--output", rtPath)

	srcWS := decodeWorkspace(test, dumpPath)
	rtWS := decodeWorkspace(test, rtPath)
	stripVolatile(srcWS)
	stripVolatile(rtWS)

	srcJSON, srcMarshalErr := json.MarshalIndent(srcWS, "", "  ")

	if srcMarshalErr != nil {
		test.Fatalf("marshaling source: %v", srcMarshalErr)
	}

	rtJSON, rtMarshalErr := json.MarshalIndent(rtWS, "", "  ")

	if rtMarshalErr != nil {
		test.Fatalf("marshaling round-trip: %v", rtMarshalErr)
	}

	if !bytes.Equal(srcJSON, rtJSON) {
		test.Fatalf("round-trip diverged.\nsource:\n%s\nrehydrated:\n%s", srcJSON, rtJSON)
	}
}

func TestPortability_StdinStdout(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	src := newEnv(test, binPath, "flag", "json")
	mustRun(test, src, "task", "create", "piped")

	exportRes := mustRun(test, src, "export")

	dst := newEnv(test, binPath, "flag", "json")
	dst.step = currentStep{stdin: exportRes.Stdout}
	importRes := dst.Run("import", "--input", "-", "--replace", "--truncate")
	dst.step = currentStep{}
	if importRes.Err != nil {
		test.Fatalf("piped import failed: %v\nstderr: %s\nstdout: %s", importRes.Err, importRes.Stderr, importRes.Stdout)
	}

	listRes := mustRun(test, dst, "task", "list")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(listRes.Stdout), &rows); err != nil {
		test.Fatalf("decoding task list: %v\nraw: %s", err, listRes.Stdout)
	}
	if len(rows) != 1 {
		test.Fatalf("expected 1 task after piped import, got %d: %v", len(rows), rows)
	}
	if got, _ := rows[0]["title"].(string); got != "piped" {
		test.Fatalf("expected title \"piped\", got %q", got)
	}
}

func TestPortability_SchemaVersionError(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	stub := []byte(`{
  "schema_version": 999,
  "tusk_version": "test",
  "exported_at": "2026-04-26T00:00:00Z",
  "workflows": [], "projects": [], "players": [], "tags": [],
  "tasks": [], "relations": [], "annotations": [], "notes": [], "events": []
}`)
	stubPath := filepath.Join(test.TempDir(), "future.json")
	if err := os.WriteFile(stubPath, stub, 0o644); err != nil {
		test.Fatalf("writing stub: %v", err)
	}

	env := newEnv(test, binPath, "flag", "text")
	result := env.Run("import", "--input", stubPath)
	if result.Err == nil {
		test.Fatalf("expected non-zero exit; stdout: %s\nstderr: %s", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "999") || !strings.Contains(result.Stderr, "1") {
		test.Fatalf("expected stderr to name dump version 999 and supported 1; got:\n%s", result.Stderr)
	}
	if !strings.Contains(result.Stderr, "[schema]") {
		test.Fatalf("expected [schema] tag in stderr; got:\n%s", result.Stderr)
	}
}

func TestPortability_FKValidationError(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	src := newEnv(test, binPath, "flag", "text")
	mustRun(test, src, "task", "create", "with bad parent")

	dumpPath := filepath.Join(test.TempDir(), "ws.json")
	mustRun(test, src, "export", "--output", dumpPath)

	raw, readErr := os.ReadFile(dumpPath)

	if readErr != nil {
		test.Fatalf("reading dump: %v", readErr)
	}

	var ws map[string]any
	if unmarshalErr := json.Unmarshal(raw, &ws); unmarshalErr != nil {
		test.Fatalf("parsing dump: %v", unmarshalErr)
	}

	tasks := ws["tasks"].([]any)
	if len(tasks) == 0 {
		test.Fatalf("no tasks in dump")
	}
	tasks[0].(map[string]any)["parent_id"] = "00000000-0000-0000-0000-deadbeefdead"
	patched, marshalErr := json.Marshal(ws)

	if marshalErr != nil {
		test.Fatalf("re-encoding dump: %v", marshalErr)
	}

	badPath := filepath.Join(test.TempDir(), "bad.json")
	if writeErr := os.WriteFile(badPath, patched, 0o644); writeErr != nil {
		test.Fatalf("writing bad dump: %v", writeErr)
	}

	dst := newEnv(test, binPath, "flag", "text")
	result := dst.Run("import", "--input", badPath, "--replace", "--truncate")
	if result.Err == nil {
		test.Fatalf("expected non-zero exit; stdout: %s\nstderr: %s", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "[fk]") {
		test.Fatalf("expected [fk] kind in stderr; got:\n%s", result.Stderr)
	}
}

func TestPortability_CollisionWithoutReplace(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	src := newEnv(test, binPath, "flag", "text")
	mustRun(test, src, "task", "create", "collidable")

	dumpPath := filepath.Join(test.TempDir(), "ws.json")
	mustRun(test, src, "export", "--output", dumpPath)

	dst := newEnv(test, binPath, "flag", "text")
	mustRun(test, dst, "import", "--input", dumpPath, "--replace", "--truncate")

	result := dst.Run("import", "--input", dumpPath)
	if result.Err == nil {
		test.Fatalf("expected collision exit; stdout: %s\nstderr: %s", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "[collision]") {
		test.Fatalf("expected [collision] kind in stderr; got:\n%s", result.Stderr)
	}
}

func TestPortability_DryRunDoesNotMutate(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := newEnv(test, binPath, "flag", "json")
	mustRun(test, env, "task", "create", "before")

	dumpPath := filepath.Join(test.TempDir(), "ws.json")
	mustRun(test, env, "export", "--output", dumpPath)
	mustRun(test, env, "task", "create", "scratch")

	listBefore := mustRun(test, env, "task", "list")
	var before []any
	if err := json.Unmarshal([]byte(listBefore.Stdout), &before); err != nil {
		test.Fatalf("decoding before list: %v", err)
	}

	mustRun(test, env, "import", "--input", dumpPath, "--replace", "--dry-run")

	listAfter := mustRun(test, env, "task", "list")
	var after []any
	if err := json.Unmarshal([]byte(listAfter.Stdout), &after); err != nil {
		test.Fatalf("decoding after list: %v", err)
	}
	if len(before) != len(after) {
		test.Fatalf("dry-run mutated workspace: before=%d after=%d", len(before), len(after))
	}
}
