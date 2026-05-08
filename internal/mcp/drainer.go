package mcp

import (
	"context"
	"log/slog"
	"time"

	"github.com/germanamz/tusk/internal/embed"
)

// DrainerConfig configures RunDrainer.
type DrainerConfig struct {
	Runtime  *Runtime
	Interval time.Duration // default 2 * time.Second
	Logger   *slog.Logger  // optional; nil silences output
}

// RunDrainer loops on a ticker calling embed.DrainQueue until ctx cancels. When
// the runtime has no embedder configured, RunDrainer is a no-op but still
// respects ctx cancellation.
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
			if config.Runtime.Embedder == nil {
				continue
			}

			drained, drainErr := embed.DrainQueue(ctx, embed.DrainConfig{
				Root:       config.Runtime.Root,
				Nodes:      config.Runtime.Nodes,
				Queue:      config.Runtime.EmbedQueue,
				Embeddings: config.Runtime.Embeddings,
				Embedder:   config.Runtime.Embedder,
				Chunker:    config.Runtime.Chunker,
			})

			if drainErr != nil && config.Logger != nil {
				config.Logger.Warn("drainer error", "err", drainErr)
			}

			if drained > 0 && config.Logger != nil {
				config.Logger.Info("drainer batch", "count", drained)
			}
		}
	}
}
