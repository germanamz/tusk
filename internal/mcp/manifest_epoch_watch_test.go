package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/epoch"
)

// Poll backstop: maybeReloadManifestForEpoch on every tick; no reload if epoch unchanged.
func TestManifestEpochWatcher_PollsOnTicker(test *testing.T) {
	srv := buildTestServer(test)
	root := srv.runtime.Root

	reloaded, err := srv.maybeReloadManifestForEpoch(context.Background(), 5*time.Second)
	if err != nil {
		test.Fatalf("maybeReloadManifestForEpoch (no change): %v", err)
	}
	if reloaded {
		test.Fatal("reloaded despite unchanged manifest epoch")
	}

	if _, bumpErr := epoch.Manifest.Bump(root); bumpErr != nil {
		test.Fatalf("bump: %v", bumpErr)
	}

	reloaded, err = srv.maybeReloadManifestForEpoch(context.Background(), 5*time.Second)
	if err != nil {
		test.Fatalf("maybeReloadManifestForEpoch (advanced): %v", err)
	}
	if !reloaded {
		test.Fatal("expected a reload after manifest-epoch advance")
	}
	if srv.seenManifestEpoch.Load() != 1 {
		test.Fatalf("expected seenManifestEpoch 1 after convergence, got %d", srv.seenManifestEpoch.Load())
	}
}

// Fast-path fsnotify: converges well before the 2s tick when manifest-epoch is bumped.
func TestManifestEpochFastWatcher_ReloadsPromptly(test *testing.T) {
	srv := buildTestServer(test)
	root := srv.runtime.Root

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = RunManifestEpochFastWatcher(ctx, EpochWatchConfig{Server: srv}) }()

	time.Sleep(150 * time.Millisecond)

	if _, err := epoch.Manifest.Bump(root); err != nil {
		test.Fatalf("bump: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.seenManifestEpoch.Load() == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	test.Fatalf("fast watcher did not converge; seenManifestEpoch=%d", srv.seenManifestEpoch.Load())
}

// Fast-path filter: non-manifest-epoch files in .tusk/ are ignored.
func TestManifestEpochFastWatcher_FiltersNonSentinelFiles(test *testing.T) {
	srv := buildTestServer(test)
	root := srv.runtime.Root

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = RunManifestEpochFastWatcher(ctx, EpochWatchConfig{Server: srv}) }()

	time.Sleep(150 * time.Millisecond)

	otherFile := filepath.Join(root, ".tusk", "some-other-file")
	if writeErr := os.WriteFile(otherFile, []byte("noise\n"), 0o644); writeErr != nil {
		test.Fatalf("write non-sentinel file: %v", writeErr)
	}

	time.Sleep(200 * time.Millisecond)

	if srv.seenManifestEpoch.Load() != 0 {
		test.Fatalf("fast watcher reacted to non-sentinel file; seenManifestEpoch=%d", srv.seenManifestEpoch.Load())
	}
}
