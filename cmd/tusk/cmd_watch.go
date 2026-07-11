package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/ignore"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/watcher"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch the workspace for external edits and keep the index in sync",
		Long: `Watch the workspace for filesystem changes and update the index in
real time.

The watcher debounces rapid edits, follows file moves via fsnotify, and
drains the embedding queue in the background. It uses the same internal
service as "tusk node create" / "modify", so files edited in vim, Obsidian,
or piped from an LLM produce identical index state.

Runs until interrupted (Ctrl-C). Pair with "tusk status" or "tusk doctor"
in another shell to observe progress.

Setting ` + "`[embeddings] workers = 0`" + ` (or ` + "`TUSK_EMBED_WORKERS=0`" + `) makes
"tusk watch" refuse to start; the watcher needs a drainer.`,
		Example: `  # Foreground: keep the index live while you author in any editor
  tusk watch

  # Background with a status pulse every 5 seconds
  tusk watch &
  watch -n 5 tusk status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			verbose, _ := cmd.Flags().GetBool("verbose")
			logger := newLogger(cmd.ErrOrStderr(), verbose)

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return loadErr
			}

			manifest.MergeBuiltinPacks(loaded)

			if resolveEmbedWorkers(loaded) == 0 {
				return fmt.Errorf(
					"tusk watch: embed workers disabled (workers=0); watch needs at least one worker. " +
						"To run a watcher-only instance, ensure another tusk instance is draining " +
						"this workspace's index, then unset TUSK_EMBED_WORKERS or set [embeddings] workers > 0 in tusk.toml")
			}

			store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			nodeRepo := index.NewNodeRepo(store)
			edgeRepo := index.NewEdgeRepo(store)
			metaRepo := index.NewMetaRepo(store)
			fileStateRepo := index.NewFileStateRepo(store)
			embedQueueRepo := index.NewEmbedQueueRepo(store)
			workflowDriftRepo := index.NewWorkflowDriftRepo(store)
			propertyDriftRepo := index.NewPropertyDriftRepo(store)

			engine, engineErr := newBehaviorEngine(loaded)

			if engineErr != nil {
				return engineErr
			}

			// Build the embedder so every watch-triggered reindex drains the
			// embedding queue inline (reindex.Run only drains when Embedder is set
			// and Workers > 0). Omitting it — the prior behavior — left `tusk watch`
			// enqueuing embeddings it never processed, so semantic search stayed
			// blind to watched content despite the --help promise (and the
			// workers=0 refusal that exists precisely because "the watcher needs a
			// drainer"). Mirrors cmd_reindex.go's wiring.
			embedder, chunker := embed.NewFromManifest(loaded.Embeddings, logger)

			var embeddingRepo *index.EmbeddingRepo

			if embedder != nil {
				embeddingRepo = index.NewEmbeddingRepo(store)
			}

			// buildReindexConfig centralizes the full validating reindex config so
			// the initial pass and every watch-triggered pass stay identical —
			// including the workflow/property validators, their drift logs, and the
			// embedding pipeline. Omitting these (the prior behavior) made
			// `tusk watch` index content while skipping validation and embedding, so
			// doctor drift and semantic results went stale until a manual
			// `tusk reindex`.
			buildReindexConfig := func() reindex.Config {
				return reindex.Config{
					Root:            ws.Root,
					Repo:            nodeRepo,
					Edges:           edgeRepo,
					EdgeTypes:       loaded.EdgeTypes,
					WorkspaceIgnore: loaded.Workspace.Ignore,
					EmbedQueue:      embedQueueRepo,
					EmbeddingRepo:   embeddingRepo,
					Embedder:        embedder,
					Chunker:         chunker,
					Meta:            metaRepo,
					FileStates:      fileStateRepo,
					Behaviors:       engine,
					DriftLog:        workflowDriftRepo,
					NodeTypes:       loaded.NodeTypes,
					PropertyDrift:   propertyDriftRepo,
					Logger:          logger,
					Manifest:        loaded,
					Workers:         resolveEmbedWorkers(loaded),
				}
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Initial reindex …")

			if _, runErr := reindex.Run(buildReindexConfig()); runErr != nil {
				// A transport error means only the embedding backend was
				// unreachable at startup — the node/edge index and validation
				// already committed. Don't let a down backend stop the watcher from
				// starting: its per-event reindex (and a manual `tusk reindex`) will
				// drain the queued embeddings once the backend returns, matching the
				// resilient MCP daemon. Any other error is a real failure and stays
				// fatal. (Pre-#681 no embedder was wired, so boot never touched the
				// backend; wiring the drainer must not make the backend a hard
				// startup dependency.)
				if !embed.IsTransportError(runErr) {
					return runErr
				}

				logger.Warn("initial reindex: embedding backend unreachable; starting watcher anyway, embeddings will drain when it returns",
					"err", runErr.Error())
			}

			logger.Info("watch started", "root", ws.Root)

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Watching for changes (Ctrl-C to stop)…")

			matcher, matcherErr := ignore.NewMatcher(ws.Root, loaded.Workspace.Ignore)

			if matcherErr != nil {
				return matcherErr
			}

			watcherInstance, newErr := watcher.New(ws.Root, matcher, logger)

			if newErr != nil {
				return newErr
			}

			defer watcherInstance.Close()

			parent := cmd.Context()

			if parent == nil {
				parent = context.Background()
			}

			ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
			defer cancel()

			handler := func(event watcher.WatchEvent) error {
				if event.Path == "" || event.Path == "." {
					return nil
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s\n", kindLabel(event.Kind), event.Path)

				logger.Debug("watch fs event", "kind", kindLabel(event.Kind), "path", event.Path)

				// Re-run the full reindex pass on EVERY event — create, modify,
				// rename, delete, and directory-level move alike. reindex.Run
				// reconciles the whole tree against disk (it ignores event.Path), so
				// it indexes files that arrive inside a moved-in directory; reaps
				// files moved or deleted out, which tombstones their file_state row
				// and deletes their node and edges; re-heals recorded ref drift; and
				// re-derives edges. This mirrors the MCP watcher
				// (internal/mcp/watch.go), which has always reindexed on every event.
				// The hand-rolled delete branch and stat/IsDir gate that used to live
				// here bypassed reindex.Run for exactly those cases, so watch-only
				// vaults silently missed dir move-ins/outs (#681-2), lost
				// delete-then-restore nodes to a stale file_state row (#681-3), and
				// skipped the #677 ref-drift heal on deletes (#681-4).
				//
				// Plan 8 polish: replace with single-file partial reindex.
				if _, runErr := reindex.Run(buildReindexConfig()); runErr != nil {
					logger.Warn("watch handler reindex failed", "path", event.Path, "err", runErr.Error())

					return runErr
				}

				return nil
			}

			return watcherInstance.Run(ctx, handler)
		},
	}

	return watchCmd
}

func kindLabel(kind watcher.EventKind) string {
	switch kind {
	case watcher.EventCreate:
		return "CREATE"
	case watcher.EventModify:
		return "MODIFY"
	case watcher.EventRename:
		return "RENAME"
	case watcher.EventDelete:
		return "DELETE"
	}

	return "?"
}
