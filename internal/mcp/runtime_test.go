package mcp_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/mcp"
)

func TestRuntimeRebuildsOnSchemaMismatch(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[workspace]\nname = \"test\"\n"), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "hello.md"), []byte("---\ntitle: hello\n---\nbody\n"), 0o644); writeErr != nil {
		test.Fatalf("write fixture: %v", writeErr)
	}

	indexPath := filepath.Join(root, ".tusk", "index.db")

	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	seedStore, openErr := index.Open(indexPath)

	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}

	if setErr := index.NewMetaRepo(seedStore).Set(index.MetaSchemaVersionKey, "from-other-binary"); setErr != nil {
		test.Fatalf("seed mismatch: %v", setErr)
	}

	if closeErr := seedStore.Close(); closeErr != nil {
		test.Fatalf("close seed: %v", closeErr)
	}

	rt, bootErr := mcp.Open(root)

	if bootErr != nil {
		test.Fatalf("mcp.Open: %v", bootErr)
	}

	defer rt.Close()

	got, getErr := index.NewMetaRepo(rt.Index).Get(index.MetaSchemaVersionKey)

	if getErr != nil {
		test.Fatalf("read schema_version: %v", getErr)
	}

	if got != index.SchemaVersion {
		test.Errorf("rebuilt schema_version = %q, want %q", got, index.SchemaVersion)
	}
}

func TestRuntime_OpenWithNodeTypesWiresPropertyDrift(test *testing.T) {
	root := test.TempDir()

	manifestBody := []byte(`
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "summary", type = "string", required = true },
]
`)

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestBody, 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	if rt.PropertyDrift == nil {
		test.Errorf("PropertyDrift is nil after Open")
	}

	if _, ok := rt.Manifest.NodeTypes["ticket"]; !ok {
		test.Errorf("Manifest.NodeTypes lacks ticket")
	}
}

func TestOpen_LoadsWorkspace(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	if rt.Manifest == nil {
		test.Errorf("Manifest is nil")
	}

	if rt.Index == nil {
		test.Errorf("Index is nil")
	}

	if rt.NodeService == nil {
		test.Errorf("NodeService is nil")
	}
}

func TestOpen_FailsWhenNoWorkspace(test *testing.T) {
	if _, openErr := mcp.Open(test.TempDir()); openErr == nil {
		test.Fatalf("expected error for missing tusk.toml")
	}
}

func TestOpen_AllowsConcurrentInstances(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir .tusk: %v", mkErr)
	}

	rtA, openErrA := mcp.Open(root)

	if openErrA != nil {
		test.Fatalf("Open A: %v", openErrA)
	}

	defer rtA.Close()

	rtB, openErrB := mcp.Open(root)

	if openErrB != nil {
		test.Fatalf("Open B: %v", openErrB)
	}

	defer rtB.Close()

	if rtA.Nodes == nil || rtA.FileState == nil {
		test.Errorf("runtime A missing Nodes/FileState")
	}

	if rtB.Nodes == nil || rtB.FileState == nil {
		test.Errorf("runtime B missing Nodes/FileState")
	}

	if closeErr := rtA.Close(); closeErr != nil {
		test.Errorf("Close A: %v", closeErr)
	}

	if closeErr := rtB.Close(); closeErr != nil {
		test.Errorf("Close B: %v", closeErr)
	}
}

func TestOpen_WithLogger_PopulatesRuntimeLogger(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[workspace]\nname = \"test\"\n"), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir .tusk: %v", mkErr)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	rt, openErr := mcp.Open(root, mcp.WithLogger(logger))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	defer rt.Close()

	if rt.Logger != logger {
		test.Errorf("Runtime.Logger should be the passed logger")
	}
}
