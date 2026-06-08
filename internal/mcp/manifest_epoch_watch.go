package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/germanamz/tusk/internal/manifestepoch"
)

// RunManifestEpochWatcher polls .tusk/manifest-epoch on a ticker. When a foreign
// reload advances the manifest epoch, it reloads this daemon's manifest schema
// so siblings converge onto the fresh schema. This is the deterministic backstop;
// the fast-path adds an fsnotify fast-path that calls the same check early.
func RunManifestEpochWatcher(ctx context.Context, config EpochWatchConfig) error {
	interval := config.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}

	lockTTL := config.LockTTL
	if lockTTL <= 0 {
		lockTTL = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := config.Server.maybeReloadManifestForEpoch(ctx, lockTTL); err != nil && config.Logger != nil {
				config.Logger.Warn("manifest epoch watcher reload error", "err", err)
			}
		}
	}
}

// RunManifestEpochFastWatcher watches the .tusk directory for changes to the
// manifest-epoch sentinel and calls maybeReloadManifestForEpoch immediately on a
// change — a low-latency fast path over the RunManifestEpochWatcher tick, which
// remains the deterministic backstop. Errors are logged; the tick still guarantees
// eventual convergence.
func RunManifestEpochFastWatcher(ctx context.Context, config EpochWatchConfig) error {
	srv := config.Server

	srv.mu.RLock()
	root := srv.runtime.Root
	srv.mu.RUnlock()

	tuskDir := filepath.Join(root, ".tusk")

	if mkErr := os.MkdirAll(tuskDir, 0o755); mkErr != nil {
		return fmt.Errorf("mcp: manifest epoch fast-watch ensure dir: %w", mkErr)
	}

	fsWatcher, newErr := fsnotify.NewWatcher()

	if newErr != nil {
		return fmt.Errorf("mcp: manifest epoch fast-watch: %w", newErr)
	}

	defer func() { _ = fsWatcher.Close() }()

	if addErr := fsWatcher.Add(tuskDir); addErr != nil {
		return fmt.Errorf("mcp: manifest epoch fast-watch add %s: %w", tuskDir, addErr)
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

			if filepath.Base(event.Name) != manifestepoch.ManifestEpochFilename {
				continue
			}

			if _, reloadErr := srv.maybeReloadManifestForEpoch(ctx, lockTTL); reloadErr != nil && config.Logger != nil {
				config.Logger.Warn("manifest epoch fast-watch reload error", "err", reloadErr)
			}
		case watchErr, ok := <-fsWatcher.Errors:
			if !ok {
				return nil
			}

			if config.Logger != nil {
				config.Logger.Warn("manifest epoch fast-watch error", "err", watchErr)
			}
		}
	}
}
