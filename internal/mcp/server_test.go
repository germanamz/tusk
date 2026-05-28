package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/mcp"
)

func TestNewServer_ReturnsServer(test *testing.T) {
	root := test.TempDir()

	os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644)

	rt, _ := mcp.Open(root)
	defer rt.Close()

	srv := mcp.NewServer(rt)

	if srv == nil {
		test.Fatalf("NewServer returned nil")
	}
}

// TestRunBackground_WorkersZeroSkipsDrainers exercises the T7.1 opt-out:
// when runtime.Workers == 0 the embed drainer and reindex drainer goroutines
// never start, so enqueued reindex/embed jobs stay in the queue.
func TestRunBackground_WorkersZeroSkipsDrainers(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"

[embeddings]
provider = "ollama"
model    = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim      = 768
workers  = 0
`), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	if rt.Workers != 0 {
		test.Fatalf("rt.Workers = %d, want 0 (manifest workers=0 must propagate)", rt.Workers)
	}

	if enqErr := rt.EmbedQueue.EnqueueReindex("notes/a.md"); enqErr != nil {
		test.Fatalf("EnqueueReindex: %v", enqErr)
	}

	srv := mcp.NewServer(rt)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if runErr := srv.RunBackground(ctx); runErr != nil && runErr != context.DeadlineExceeded {
		test.Fatalf("RunBackground: %v", runErr)
	}

	depth, depthErr := rt.EmbedQueue.DepthByKind("reindex")

	if depthErr != nil {
		test.Fatalf("DepthByKind: %v", depthErr)
	}

	if depth != 1 {
		test.Errorf("reindex depth after RunBackground = %d, want 1 (drainer must not run)", depth)
	}
}
