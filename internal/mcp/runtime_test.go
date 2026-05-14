package mcp_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/lock"
	"github.com/germanamz/tusk/internal/mcp"
)

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

func TestRuntime_OpenAcquiresWorkspaceLock(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir .tusk: %v", mkErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	extLock, newErr := lock.NewWorkspaceLock(root)

	if newErr != nil {
		test.Fatalf("NewWorkspaceLock: %v", newErr)
	}

	busyCtx, busyCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer busyCancel()

	if acqErr := extLock.Acquire(busyCtx); !errors.Is(acqErr, lock.ErrBusy) {
		test.Errorf("expected ErrBusy while runtime holds lock; got %v", acqErr)
	}

	if closeErr := rt.Close(); closeErr != nil {
		test.Fatalf("Close: %v", closeErr)
	}

	freeCtx, freeCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer freeCancel()

	if acqErr := extLock.Acquire(freeCtx); acqErr != nil {
		test.Errorf("expected lock to be free after Close; got %v", acqErr)
	}

	_ = extLock.Release()
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

func TestRuntime_ReloadManifestRebuildsBehaviorEngine(test *testing.T) {
	root := test.TempDir()

	manifestBody := []byte(`
[workspace]
name = "test"

[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
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

	if rt.BehaviorEngine == nil {
		test.Fatalf("BehaviorEngine is nil after Open")
	}

	// Modify the manifest: add a transitions block.
	updated := []byte(`
[workspace]
name = "test"

[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
]
transitions = [
  { from = "pending", to = "active" },
]
`)

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), updated, 0o644); writeErr != nil {
		test.Fatalf("rewrite: %v", writeErr)
	}

	if reloadErr := rt.ReloadManifest(); reloadErr != nil {
		test.Fatalf("ReloadManifest: %v", reloadErr)
	}

	if rt.BehaviorEngine == nil {
		test.Errorf("BehaviorEngine is nil after ReloadManifest")
	}
}
