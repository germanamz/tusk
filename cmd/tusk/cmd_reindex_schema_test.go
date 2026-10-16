package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/epoch"
)

const schemaTestReferences = `
[edge-types.references]
from        = ["*"]
to          = ["*"]
cardinality = "many-to-many"
wikilinks   = true
`

// schemaTestWorkspace is the #831 repro: two notes, one linking the other, in
// a workspace that declares nothing yet.
func schemaTestWorkspace(test *testing.T) string {
	test.Helper()

	root := initWorkspaceWithManifest(test, "[workspace]\nname = \"test\"\n")

	writeTestFile(test, filepath.Join(root, "notes/auth.md"), "---\ntype: note\ntitle: Auth\n---\n\nAuth.\n")
	writeTestFile(test, filepath.Join(root, "notes/home.md"), "---\ntype: note\ntitle: Home\n---\n\nSee [[notes/auth]].\n")

	return root
}

func writeTestFile(test *testing.T, path, content string) {
	test.Helper()

	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(path, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", path, writeErr)
	}
}

func appendManifest(test *testing.T, root, body string) {
	test.Helper()

	path := filepath.Join(root, "tusk.toml")
	current, readErr := os.ReadFile(path)

	if readErr != nil {
		test.Fatalf("read tusk.toml: %v", readErr)
	}

	writeTestFile(test, path, string(current)+body)
}

func runOK(test *testing.T, root string, args ...string) (string, string) {
	test.Helper()

	stdout, stderr, ok := runCLISplit(root, args...)

	if !ok {
		test.Fatalf("tusk %s failed: %s", strings.Join(args, " "), stderr.String())
	}

	return stdout.String(), stderr.String()
}

func manifestEpoch(test *testing.T, root string) int64 {
	test.Helper()

	value, readErr := epoch.Manifest.Read(root)

	if readErr != nil {
		test.Fatalf("read manifest epoch: %v", readErr)
	}

	return value
}

// TestReindexCmd_SchemaChangeReachesUnchangedFiles is the #831 report end to
// end: after a new edge type is declared, a plain `tusk reindex` re-processes
// the unchanged notes, and it bumps the manifest epoch so a running daemon
// reloads tusk.toml instead of re-deriving under its old copy.
func TestReindexCmd_SchemaChangeReachesUnchangedFiles(test *testing.T) {
	root := schemaTestWorkspace(test)

	runOK(test, root, "reindex")

	built := manifestEpoch(test, root)

	if stdout, _ := runOK(test, root, "reindex"); !strings.Contains(stdout, "0 indexed") {
		test.Errorf("second reindex, same schema: %q, want 0 indexed", stdout)
	}

	if got := manifestEpoch(test, root); got != built {
		test.Errorf("manifest epoch = %d after a reindex with no schema change, want %d", got, built)
	}

	appendManifest(test, root, schemaTestReferences)

	if stdout, _ := runOK(test, root, "reindex"); !strings.Contains(stdout, "2 indexed") {
		test.Errorf("reindex after declaring references: %q, want 2 indexed", stdout)
	}

	if got := manifestEpoch(test, root); got != built+1 {
		test.Errorf("manifest epoch = %d after a schema change, want %d", got, built+1)
	}

	edges, _ := runOK(test, root, "edge", "list", "--from", "notes/home")

	if !strings.Contains(edges, "references") {
		test.Errorf("edge list --from notes/home = %q, want a references edge", edges)
	}
}

// TestDoctorCmd_ReportsStaleSchema covers step 5 of #831: doctor read the
// index as the old schema left it and reported nothing. It now warns until a
// pass applies the new declarations, which `doctor --reindex` runs first.
func TestDoctorCmd_ReportsStaleSchema(test *testing.T) {
	root := schemaTestWorkspace(test)

	runOK(test, root, "reindex")
	appendManifest(test, root, schemaTestReferences)

	if stdout, _ := runOK(test, root, "doctor"); !strings.Contains(stdout, "index-schema-stale") {
		test.Errorf("doctor after a schema edit = %q, want index-schema-stale", stdout)
	}

	before := manifestEpoch(test, root)
	stdout, _ := runOK(test, root, "doctor", "--reindex")

	if strings.Contains(stdout, "index-schema-stale") {
		test.Errorf("doctor --reindex = %q, want no index-schema-stale", stdout)
	}

	if got := manifestEpoch(test, root); got != before+1 {
		test.Errorf("manifest epoch = %d after doctor --reindex applied a schema change, want %d", got, before+1)
	}
}
