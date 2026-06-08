package mcp

import (
	"context"
	"log/slog"
	"time"
)

// EpochWatchConfig configures RunEpochWatcher.
type EpochWatchConfig struct {
	Server   *Server
	Interval time.Duration // default 2s
	LockTTL  time.Duration // default 5s; bound on awaiting the resetter
	Logger   *slog.Logger
}

// RunEpochWatcher polls .tusk/epoch on a ticker. When a foreign reset advances
// the epoch, it reopens this daemon's index handle so siblings converge onto the
// fresh DB. This is the deterministic backstop; Phase 8 adds an fsnotify
// fast-path that calls the same check early.
func RunEpochWatcher(ctx context.Context, config EpochWatchConfig) error {
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
			reopened, err := config.Server.maybeReopenForEpoch(ctx, lockTTL)
			if err != nil && config.Logger != nil {
				config.Logger.Warn("epoch watcher reopen error", "err", err)
			}

			if reopened && config.Logger != nil {
				config.Logger.Info("index reset detected; reopened handle")
			}
		}
	}
}
