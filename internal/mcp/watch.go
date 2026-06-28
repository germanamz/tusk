package mcp

import (
	"context"
	"log/slog"

	"github.com/germanamz/tusk/internal/ignore"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/watcher"
)

// WatchConfig configures RunWatcher.
type WatchConfig struct {
	Server *Server
	Logger *slog.Logger
}

// RunWatcher boots an fsnotify watcher rooted at the runtime's Root and reacts
// to every debounced event by re-running the full reindex pass. The watcher
// snapshots the runtime under a brief read-lock — once for the root at boot and
// again per event — then runs the reindex pass off the snapshot WITHOUT holding
// the lock. Plan 6 mirrors Plan 3's full-tree reindex strategy; single-file
// partial reindex lands in Plan 8.
func RunWatcher(ctx context.Context, config WatchConfig) error {
	boot := config.Server.snapshotRuntime() // brief read-lock; watcher boots off the snapshot
	root := boot.Root

	// Mirror the reindex walker's ignore rules so index writes under .tusk/ (and
	// .git/ activity) never trip the watcher into a self-sustaining reindex loop.
	matcher, matcherErr := ignore.NewMatcher(root, boot.Manifest.Workspace.Ignore)

	if matcherErr != nil {
		return matcherErr
	}

	instance, newErr := watcher.New(root, matcher)

	if newErr != nil {
		return newErr
	}

	defer instance.Close()

	handler := func(event watcher.WatchEvent) error {
		if event.Path == "" || event.Path == "." {
			return nil
		}

		rt := config.Server.snapshotRuntime() // re-snapshot per event; pass runs off the snapshot

		// Serialize the reindex walk/enqueue against an originating reload's reindex
		// (a later phase) so generation stamping never interleaves old and new schema.
		//
		// Async: true means this pass only walks and enqueues reindex jobs; it does
		// NOT drain them inline. The workflow/property validators (and their drift
		// logs) run in the per-file drain, which here is RunReindexDrainer — and that
		// path DOES set Behaviors/DriftLog/NodeTypes/PropertyDrift. So validation and
		// doctor-drift upkeep happen for daemon-observed edits; adding those fields to
		// this Async config would be dead weight (the inline drain never runs).
		config.Server.reindexMu.Lock()
		_, runErr := reindex.Run(reindex.Config{
			Root:            rt.Root,
			Repo:            rt.Nodes,
			Edges:           rt.Edges,
			EdgeTypes:       rt.Manifest.EdgeTypes,
			WorkspaceIgnore: rt.Manifest.Workspace.Ignore,
			EmbedQueue:      rt.EmbedQueue,
			EmbeddingRepo:   rt.Embeddings,
			Embedder:        rt.Embedder,
			Chunker:         rt.Chunker,
			Meta:            rt.Meta,
			FileStates:      rt.FileState,
			Logger:          config.Logger,
			Async:           true,
		})
		config.Server.reindexMu.Unlock()

		if runErr != nil && config.Logger != nil {
			config.Logger.Warn("watcher reindex error", "err", runErr, "path", event.Path)
		}

		return nil
	}

	return instance.Run(ctx, handler)
}
