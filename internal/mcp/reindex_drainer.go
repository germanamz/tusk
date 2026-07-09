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
	Server   *Server
	Interval time.Duration // default 2 * time.Second
	Logger   *slog.Logger  // optional; nil silences output
}

// RunReindexDrainer loops on a ticker calling reindex.DrainReindexQueue until
// ctx cancels. Each tick it snapshots the runtime under a brief read-lock, then
// runs the drain pass off the snapshot WITHOUT holding the lock. The generation
// stamp comes from the snapshot's MetaRepo on each tick so newly-enqueued
// reindex jobs from concurrent walks are processed against the correct gen.
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
			rt := config.Server.snapshotRuntime() // brief read-lock; pass runs off the snapshot

			var gen int64

			if stored, getErr := rt.Meta.Get("reindex_gen"); getErr == nil && stored != "" {
				gen, _ = strconv.ParseInt(stored, 10, 64)
			}

			workerCfg := reindex.WorkerConfig{
				Root:          rt.Root,
				Repo:          rt.Nodes,
				Edges:         rt.Edges,
				EdgeTypes:     rt.Manifest.EdgeTypes,
				EmbedQueue:    rt.EmbedQueue,
				FileStates:    rt.FileState,
				Manifest:      rt.Manifest,
				Behaviors:     rt.BehaviorEngine,
				DriftLog:      rt.WorkflowDrift,
				NodeTypes:     rt.Manifest.NodeTypes,
				PropertyDrift: rt.PropertyDrift,
				Logger:        config.Logger,
				Workers:       rt.Workers,
				TTL:           rt.LeaseTTL,
				WorkerID:      rt.WorkerID,
				Generation:    gen,
			}

			report, drainErr := reindex.DrainReindexQueue(ctx, workerCfg)

			if drainErr != nil && config.Logger != nil {
				config.Logger.Warn("reindex drainer error", "err", drainErr)
			}

			if report.Indexed > 0 && config.Logger != nil {
				config.Logger.Info("reindex drainer batch",
					"indexed", report.Indexed,
					"skipped", report.Skipped,
				)
			}

			// A productive tick may have added the node a recorded ref-drift
			// row was waiting for; retry those rows now. Idle ticks skip the
			// heal so genuinely broken refs don't re-process every interval.
			if report.Indexed > 0 {
				healReport, healErr := reindex.HealRefDrift(ctx, workerCfg)

				if healErr != nil && config.Logger != nil {
					config.Logger.Warn("reindex drainer heal error", "err", healErr)
				}

				if healReport.Healed > 0 && config.Logger != nil {
					config.Logger.Info("reindex drainer ref heal",
						"attempted", healReport.Attempted,
						"healed", healReport.Healed,
					)
				}
			}
		}
	}
}
