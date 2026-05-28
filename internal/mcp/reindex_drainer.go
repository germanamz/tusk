package mcp

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/germanamz/tusk/internal/reindex"
)

// ReindexDrainerConfig configures RunReindexDrainer.
type ReindexDrainerConfig struct {
	Runtime  *Runtime
	Interval time.Duration // default 2 * time.Second
	Logger   *slog.Logger  // optional; nil silences output
}

// RunReindexDrainer loops on a ticker calling reindex.DrainReindexQueue until
// ctx cancels. The generation stamp comes from the runtime's MetaRepo on each
// tick so newly-enqueued reindex jobs from concurrent walks are processed
// against the correct gen.
func RunReindexDrainer(ctx context.Context, config ReindexDrainerConfig) error {
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
			var gen int64

			if stored, getErr := config.Runtime.Meta.Get("reindex_gen"); getErr == nil && stored != "" {
				gen, _ = strconv.ParseInt(stored, 10, 64)
			}

			report, drainErr := reindex.DrainReindexQueue(ctx, reindex.WorkerConfig{
				Root:          config.Runtime.Root,
				Repo:          config.Runtime.Nodes,
				Edges:         config.Runtime.Edges,
				EdgeTypes:     config.Runtime.Manifest.EdgeTypes,
				EmbedQueue:    config.Runtime.EmbedQueue,
				FileStates:    config.Runtime.FileState,
				Manifest:      config.Runtime.Manifest,
				Behaviors:     config.Runtime.BehaviorEngine,
				DriftLog:      config.Runtime.WorkflowDrift,
				NodeTypes:     config.Runtime.Manifest.NodeTypes,
				PropertyDrift: config.Runtime.PropertyDrift,
				Logger:        config.Logger,
				Workers:       config.Runtime.Workers,
				TTL:           config.Runtime.LeaseTTL,
				Generation:    gen,
			})

			if drainErr != nil && config.Logger != nil {
				config.Logger.Warn("reindex drainer error", "err", drainErr)
			}

			if report.Indexed > 0 && config.Logger != nil {
				config.Logger.Info("reindex drainer batch",
					"indexed", report.Indexed,
					"skipped", report.Skipped,
				)
			}
		}
	}
}
