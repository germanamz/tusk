package mcp

import (
	"context"
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
func RunDrainer(ctx context.Context, config DrainerConfig) error {
	interval := config.Interval

	if interval <= 0 {
		interval = 2 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			rt := config.Server.snapshotRuntime() // brief read-lock; pass runs off the snapshot

			if rt.Embedder == nil {
				continue
			}

			drained, drainErr := embed.DrainQueue(ctx, embed.DrainConfig{
				Root:       rt.Root,
				Nodes:      rt.Nodes,
				Queue:      rt.EmbedQueue,
				Embeddings: rt.Embeddings,
				Embedder:   rt.Embedder,
				Chunker:    rt.Chunker,
				Workers:    rt.Workers,
				TTL:        rt.LeaseTTL,
				Logger:     config.Logger,
			})

			if drainErr != nil && config.Logger != nil {
				config.Logger.Warn("drainer error", "err", drainErr) // includes the benign "database is closed" if a reset swapped mid-pass
			}

			if drained > 0 && config.Logger != nil {
				config.Logger.Info("drainer batch", "count", drained)
			}
		}
	}
}
