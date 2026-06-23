package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/epoch"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reset"
)

// TestCLIReset_LiveDaemonConverges pins the feature's headline cross-surface
// guarantee, which no single phase covered in isolation: a CLI-path reset
// (reset.Perform from a separate handle — exactly what `tusk reset` does, bumping
// .tusk/epoch) is detected by a LIVE mcp daemon whose RunBackground epoch
// watchers are actually ticking, which reopens onto the fresh index and keeps
// serving rather than being stranded on the orphaned (ghost) handle.
func TestCLIReset_LiveDaemonConverges(test *testing.T) {
	root := setupServerWorkspace(test)
	daemon := newServerForRoot(test, root)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = daemon.RunBackground(ctx) }()

	// Let the background watchers register their fsnotify watch / first tick.
	time.Sleep(150 * time.Millisecond)

	indexPath := filepath.Join(root, ".tusk", "index.db")

	// Simulate a CLI-side reset: a separate handle drops + recreates the index and
	// bumps .tusk/epoch, exactly as cmd_reset.go does via reset.Perform.
	result, resetErr := reset.Perform(context.Background(), reset.Config{
		Root:      root,
		IndexPath: indexPath,
		LockTTL:   5 * time.Second,
		Reopen:    func() (*index.Index, error) { return index.Open(indexPath) },
	})
	if resetErr != nil {
		test.Fatalf("CLI-path reset: %v", resetErr)
	}

	_ = result.Store.Close() // the CLI process exits, releasing its handle

	if result.Epoch == 0 {
		test.Fatalf("expected the CLI reset to bump the epoch")
	}

	// The daemon's epoch watcher (fast-path or the tick backstop) must detect the
	// bump and converge onto the fresh handle.
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if daemon.seenEpoch.Load() >= result.Epoch {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if seen := daemon.seenEpoch.Load(); seen < result.Epoch {
		test.Fatalf("daemon did not converge after CLI reset: seenEpoch=%d want>=%d", seen, result.Epoch)
	}

	if onDisk, _ := epoch.Index.Read(root); onDisk != result.Epoch {
		test.Fatalf("on-disk epoch %d != reset epoch %d", onDisk, result.Epoch)
	}

	// The daemon serves against the fresh handle — never a closed/ghost-DB error.
	if listResult, listErr := daemon.HandleToolCall(context.Background(), nodeListRequest()); listErr != nil || listResult.IsError {
		test.Fatalf("daemon served an error after CLI reset (ghost/closed DB?): err=%v result=%s", listErr, textOf(listResult))
	}

	// Stop the background goroutines, then deterministically repopulate the fresh
	// index and confirm the converged daemon reflects the workspace.
	cancel()
	time.Sleep(50 * time.Millisecond)
	rebuildIndex(test, daemon)

	listResult, listErr := daemon.HandleToolCall(context.Background(), nodeListRequest())
	if listErr != nil || listResult.IsError {
		test.Fatalf("node list after rebuild: err=%v result=%s", listErr, textOf(listResult))
	}

	if !strings.Contains(textOf(listResult), seededNodeID(test)) {
		test.Fatalf("converged daemon missing the seeded node; got: %s", textOf(listResult))
	}
}
