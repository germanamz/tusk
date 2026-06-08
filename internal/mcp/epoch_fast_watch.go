package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/germanamz/tusk/internal/indexepoch"
)

// RunIndexEpochFastWatcher watches the .tusk directory for changes to the epoch
// sentinel and calls maybeReopenForEpoch immediately on a change — a low-latency
// fast path over the RunEpochWatcher tick, which remains the deterministic
// backstop. Errors are logged; the tick still guarantees eventual convergence.
func RunIndexEpochFastWatcher(ctx context.Context, config EpochWatchConfig) error {
	srv := config.Server

	srv.mu.RLock()
	root := srv.runtime.Root
	srv.mu.RUnlock()

	tuskDir := filepath.Join(root, ".tusk")

	if mkErr := os.MkdirAll(tuskDir, 0o755); mkErr != nil {
		return fmt.Errorf("mcp: epoch fast-watch ensure dir: %w", mkErr)
	}

	fsWatcher, newErr := fsnotify.NewWatcher()

	if newErr != nil {
		return fmt.Errorf("mcp: epoch fast-watch: %w", newErr)
	}

	defer func() { _ = fsWatcher.Close() }()

	if addErr := fsWatcher.Add(tuskDir); addErr != nil {
		return fmt.Errorf("mcp: epoch fast-watch add %s: %w", tuskDir, addErr)
	}

	lockTTL := config.LockTTL
	if lockTTL <= 0 {
		lockTTL = 5 * time.Second
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-fsWatcher.Events:
			if !ok {
				return nil
			}

			if filepath.Base(event.Name) != indexepoch.EpochFilename {
				continue
			}

			if _, reopenErr := srv.maybeReopenForEpoch(ctx, lockTTL); reopenErr != nil && config.Logger != nil {
				config.Logger.Warn("epoch fast-watch reopen error", "err", reopenErr)
			}
		case watchErr, ok := <-fsWatcher.Errors:
			if !ok {
				return nil
			}

			if config.Logger != nil {
				config.Logger.Warn("epoch fast-watch error", "err", watchErr)
			}
		}
	}
}
