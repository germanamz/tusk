package mcp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
)

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

func TestRuntime_WithWriteLockSerializes(test *testing.T) {
	root := test.TempDir()

	os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644)

	rt, _ := mcp.Open(root)
	defer rt.Close()

	calls := 0

	if lockErr := rt.WithWriteLock(func() error {
		calls++

		return nil
	}); lockErr != nil {
		test.Fatalf("WithWriteLock: %v", lockErr)
	}

	if calls != 1 {
		test.Errorf("body should run once, got %d", calls)
	}
}
