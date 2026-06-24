package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/germanamz/tusk/internal/epoch"
)

// EpochWatchConfig configures the epoch watchers (tick + fast).
type EpochWatchConfig struct {
	Server   *Server
	Interval time.Duration // default 2s
	LockTTL  time.Duration // default 5s; bound on awaiting the resetter
	Logger   *slog.Logger
}

// convergeFunc is one watcher's convergence body: re-read the sentinel and, if
// it advanced, reopen the index / reload the manifest. The two bodies
// (maybeReopenForEpoch vs maybeReloadManifestForEpoch) stay separate — only the
// watcher scaffolding below is shared. Returns whether a converge happened.
type convergeFunc func(ctx context.Context, lockTTL time.Duration) (bool, error)

// resolveLockTTL applies the 5s default for a non-positive TTL.
func resolveLockTTL(lockTTL time.Duration) time.Duration {
	if lockTTL <= 0 {
		return 5 * time.Second
	}
	return lockTTL
}

// runEpochTickWatcher polls a sentinel on a ticker, calling converge each tick.
// This is the deterministic backstop shared by the index and manifest tick
// watchers; the fsnotify fast-path (runEpochFastWatcher) calls the same
// converge early. label names the watcher in warning logs; onConverged, when
// non-nil, fires after a successful converge (the index watcher logs a reset
// notice; the manifest watcher passes nil).
func runEpochTickWatcher(
	ctx context.Context,
	config EpochWatchConfig,
	label string,
	converge convergeFunc,
	onConverged func(*slog.Logger),
) error {
	interval := config.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}

	lockTTL := resolveLockTTL(config.LockTTL)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			converged, err := converge(ctx, lockTTL)
			if err != nil && config.Logger != nil {
				config.Logger.Warn(label+" error", "err", err)
			}

			if converged && onConverged != nil && config.Logger != nil {
				onConverged(config.Logger)
			}
		}
	}
}

// runEpochFastWatcher watches the .tusk directory with fsnotify and calls
// converge immediately when the named sentinel changes — a low-latency fast
// path over the tick watcher, which remains the deterministic backstop. Errors
// are logged; the tick still guarantees eventual convergence. label names the
// watcher in setup/warning logs; filename is the sentinel base name to filter
// on (epoch.Index.Filename() / epoch.Manifest.Filename()).
func runEpochFastWatcher(
	ctx context.Context,
	config EpochWatchConfig,
	label string,
	filename string,
	converge convergeFunc,
) error {
	srv := config.Server

	srv.mu.RLock()
	root := srv.runtime.Root
	srv.mu.RUnlock()

	tuskDir := filepath.Join(root, ".tusk")

	if mkErr := os.MkdirAll(tuskDir, 0o755); mkErr != nil {
		return fmt.Errorf("mcp: %s ensure dir: %w", label, mkErr)
	}

	fsWatcher, newErr := fsnotify.NewWatcher()

	if newErr != nil {
		return fmt.Errorf("mcp: %s: %w", label, newErr)
	}

	defer func() { _ = fsWatcher.Close() }()

	if addErr := fsWatcher.Add(tuskDir); addErr != nil {
		return fmt.Errorf("mcp: %s add %s: %w", label, tuskDir, addErr)
	}

	lockTTL := resolveLockTTL(config.LockTTL)

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-fsWatcher.Events:
			if !ok {
				return nil
			}

			if filepath.Base(event.Name) != filename {
				continue
			}

			if _, convergeErr := converge(ctx, lockTTL); convergeErr != nil && config.Logger != nil {
				config.Logger.Warn(label+" error", "err", convergeErr)
			}
		case watchErr, ok := <-fsWatcher.Errors:
			if !ok {
				return nil
			}

			if config.Logger != nil {
				config.Logger.Warn(label+" error", "err", watchErr)
			}
		}
	}
}

// RunEpochWatcher polls .tusk/epoch on a ticker. When a foreign reset advances
// the epoch, it reopens this daemon's index handle so siblings converge onto the
// fresh DB. This is the deterministic backstop; RunIndexEpochFastWatcher adds an
// fsnotify fast-path that calls the same check early.
func RunEpochWatcher(ctx context.Context, config EpochWatchConfig) error {
	return runEpochTickWatcher(
		ctx,
		config,
		"epoch watcher reopen",
		config.Server.maybeReopenForEpoch,
		func(logger *slog.Logger) {
			logger.Info("index reset detected; reopened handle")
		},
	)
}

// RunIndexEpochFastWatcher watches the .tusk directory for changes to the epoch
// sentinel and calls maybeReopenForEpoch immediately on a change — a low-latency
// fast path over the RunEpochWatcher tick, which remains the deterministic
// backstop.
func RunIndexEpochFastWatcher(ctx context.Context, config EpochWatchConfig) error {
	return runEpochFastWatcher(
		ctx,
		config,
		"epoch fast-watch",
		epoch.Index.Filename(),
		config.Server.maybeReopenForEpoch,
	)
}

// RunManifestEpochWatcher polls .tusk/manifest-epoch on a ticker. When a foreign
// reload advances the manifest epoch, it reloads this daemon's manifest schema
// so siblings converge onto the fresh schema. This is the deterministic backstop;
// RunManifestEpochFastWatcher adds an fsnotify fast-path that calls the same
// check early.
func RunManifestEpochWatcher(ctx context.Context, config EpochWatchConfig) error {
	return runEpochTickWatcher(
		ctx,
		config,
		"manifest epoch watcher reload",
		config.Server.maybeReloadManifestForEpoch,
		nil,
	)
}

// RunManifestEpochFastWatcher watches the .tusk directory for changes to the
// manifest-epoch sentinel and calls maybeReloadManifestForEpoch immediately on a
// change — a low-latency fast path over the RunManifestEpochWatcher tick, which
// remains the deterministic backstop.
func RunManifestEpochFastWatcher(ctx context.Context, config EpochWatchConfig) error {
	return runEpochFastWatcher(
		ctx,
		config,
		"manifest epoch fast-watch",
		epoch.Manifest.Filename(),
		config.Server.maybeReloadManifestForEpoch,
	)
}
