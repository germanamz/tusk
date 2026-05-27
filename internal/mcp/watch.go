package mcp

import (
	"context"
	"log/slog"

	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/watcher"
)

// WatchConfig configures RunWatcher.
type WatchConfig struct {
	Runtime *Runtime
	Logger  *slog.Logger
}

// RunWatcher boots an fsnotify watcher rooted at runtime.Root and reacts to
// every debounced event by re-running the full reindex pass. Plan 6 mirrors
// Plan 3's full-tree reindex strategy; single-file partial reindex lands in
// Plan 8.
func RunWatcher(ctx context.Context, config WatchConfig) error {
	instance, newErr := watcher.New(config.Runtime.Root)

	if newErr != nil {
		return newErr
	}

	defer instance.Close()

	handler := func(event watcher.WatchEvent) error {
		if event.Path == "" || event.Path == "." {
			return nil
		}

		_, runErr := reindex.Run(reindex.Config{
			Root:            config.Runtime.Root,
			Repo:            config.Runtime.Nodes,
			Edges:           config.Runtime.Edges,
			EdgeTypes:       config.Runtime.Manifest.EdgeTypes,
			WorkspaceIgnore: config.Runtime.Manifest.Workspace.Ignore,
			EmbedQueue:      config.Runtime.EmbedQueue,
			EmbeddingRepo:   config.Runtime.Embeddings,
			Embedder:        config.Runtime.Embedder,
			Chunker:         config.Runtime.Chunker,
			Meta:            config.Runtime.Meta,
			FileStates:      config.Runtime.FileState,
			Logger:          config.Logger,
			Async:           true,
		})

		if runErr != nil && config.Logger != nil {
			config.Logger.Warn("watcher reindex error", "err", runErr, "path", event.Path)
		}

		return nil
	}

	return instance.Run(ctx, handler)
}
