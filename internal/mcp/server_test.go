package mcp_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// TestRunBackground_WorkersZeroSkipsWatcher confirms the T7.2 gate: when
// runtime.Workers == 0 the file watcher goroutine never starts, so an
// out-of-process write to the vault does not enqueue a reindex job.
func TestRunBackground_WorkersZeroSkipsWatcher(test *testing.T) {
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
		test.Fatalf("rt.Workers = %d, want 0", rt.Workers)
	}

	srv := mcp.NewServer(rt)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		done <- srv.RunBackground(ctx)
	}()

	time.Sleep(200 * time.Millisecond) // let any watcher boot if it were going to

	if mkErr := os.MkdirAll(filepath.Join(root, "notes"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	body := []byte("---\ntype: note\ntitle: external\n---\n\nbody\n")

	if writeErr := os.WriteFile(filepath.Join(root, "notes/external.md"), body, 0o644); writeErr != nil {
		test.Fatalf("write external: %v", writeErr)
	}

	time.Sleep(300 * time.Millisecond) // give a hypothetical watcher time to react

	cancel()

	select {
	case runErr := <-done:
		if runErr != nil && runErr != context.Canceled {
			test.Fatalf("RunBackground: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		test.Fatalf("RunBackground did not return after cancel")
	}

	depth, depthErr := rt.EmbedQueue.DepthByKind("reindex")

	if depthErr != nil {
		test.Fatalf("DepthByKind: %v", depthErr)
	}

	if depth != 0 {
		test.Errorf("reindex depth after external write = %d, want 0 (watcher must not run)", depth)
	}
}

// TestRunBackground_WorkersZeroEmitsWarn confirms the T7.3 startup warning:
// when runtime.Workers == 0, RunBackground emits a single WARN explaining that
// indexing is disabled in this instance. The epoch watchers still run (they
// always start regardless of workers), so RunBackground now blocks until the
// context cancels; this test cancels after a brief boot window to capture the
// WARN.
func TestRunBackground_WorkersZeroEmitsWarn(test *testing.T) {
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

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	rt, openErr := mcp.Open(root, mcp.WithLogger(logger))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	if rt.Workers != 0 {
		test.Fatalf("rt.Workers = %d, want 0", rt.Workers)
	}

	srv := mcp.NewServer(rt)

	// Epoch watchers always run, so RunBackground blocks until cancelled.
	// Cancel after a brief boot window — the WARN is emitted synchronously
	// (before any goroutine is spawned) so it appears immediately.
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		done <- srv.RunBackground(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case runErr := <-done:
		if runErr != nil && runErr != context.Canceled {
			test.Fatalf("RunBackground: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		test.Fatalf("RunBackground did not return after cancel")
	}

	if !strings.Contains(buf.String(), "embed workers disabled") {
		test.Errorf("log output missing WARN; got: %q", buf.String())
	}
}

// TestRunBackground_WorkersPositiveNoWarn confirms the WARN is absent when
// workers > 0: the drainers and watcher start instead.
func TestRunBackground_WorkersPositiveNoWarn(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	rt, openErr := mcp.Open(root, mcp.WithLogger(logger))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	if rt.Workers < 1 {
		test.Fatalf("rt.Workers = %d, want >= 1 (default resolves to max(1, NumCPU/2))", rt.Workers)
	}

	srv := mcp.NewServer(rt)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		done <- srv.RunBackground(ctx)
	}()

	time.Sleep(200 * time.Millisecond) // let the background goroutines boot

	cancel()

	select {
	case runErr := <-done:
		if runErr != nil && runErr != context.Canceled {
			test.Fatalf("RunBackground: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		test.Fatalf("RunBackground did not return after cancel")
	}

	if strings.Contains(buf.String(), "embed workers disabled") {
		test.Errorf("WARN should be absent when workers > 0; got: %q", buf.String())
	}
}
