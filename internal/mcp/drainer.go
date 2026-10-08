package mcp

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/germanamz/tusk/internal/embed"
)

// DrainerConfig configures RunDrainer.
type DrainerConfig struct {
	Server   *Server
	Interval time.Duration // default 2 * time.Second
	Logger   *slog.Logger  // optional; nil silences output
}

// RunDrainer loops on a ticker calling embed.DrainQueue until ctx cancels. Each
// tick it snapshots the runtime under a brief read-lock, then drains off the
// snapshot WITHOUT holding the lock so a concurrent reset's write-lock is never
// blocked by the (Ollama-bound) drain pass. When the runtime has no embedder
// configured, RunDrainer is a no-op but still respects ctx cancellation.
//
// While this daemon's [embeddings] settings are stale (another process
// recorded newer ones), every pass stops at once; that is logged once per
// episode, not on every tick.
func RunDrainer(ctx context.Context, config DrainerConfig) error {
	interval := config.Interval

	if interval <= 0 {
		interval = 2 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	superseded := false

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			rt := config.Server.snapshotRuntime() // brief read-lock; pass runs off the snapshot

			if rt.Embedder == nil {
				continue
			}

			drained, drainErr := drainTick(ctx, rt, config.Logger)

			if errors.Is(drainErr, embed.ErrSettingsSuperseded) {
				if !superseded && config.Logger != nil {
					config.Logger.Warn(drainErr.Error())
				}

				superseded = true

				continue
			}

			superseded = false

			if drainErr != nil && config.Logger != nil {
				config.Logger.Warn("drainer error", "err", drainErr) // includes the benign "database is closed" if a reset swapped mid-pass
			}

			if drained > 0 && config.Logger != nil {
				config.Logger.Info("drainer batch", "count", drained)
			}
		}
	}
}

// drainTick runs one embed drain pass off a runtime snapshot. The pass carries
// the snapshot's [embeddings] fingerprint, so a pass still running when a
// reload swaps in new settings stops at its next node instead of embedding
// the re-queued vault under the old ones; the next tick picks up the new
// snapshot.
func drainTick(ctx context.Context, rt *Runtime, logger *slog.Logger) (int, error) {
	var fingerprint string

	if rt.Manifest != nil && rt.Manifest.Embeddings.Provider != "" {
		fingerprint = embed.DocumentFingerprint(rt.Manifest.Embeddings)
	}

	return embed.DrainQueue(ctx, embed.DrainConfig{
		Root:             rt.Root,
		Nodes:            rt.Nodes,
		Queue:            rt.EmbedQueue,
		Embeddings:       rt.Embeddings,
		Embedder:         rt.Embedder,
		Chunker:          rt.Chunker,
		Workers:          rt.Workers,
		EmbedConcurrency: rt.Workers,
		TTL:              rt.LeaseTTL,
		Logger:           logger,
		Meta:             rt.Meta,
		Fingerprint:      fingerprint,
	})
}
