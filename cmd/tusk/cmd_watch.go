package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

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
in another shell to observe progress.`,
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

			return withWorkspaceLock(ws, func() error {
				verbose, _ := cmd.Flags().GetBool("verbose")
				logger := newLogger(cmd.ErrOrStderr(), verbose)

				loaded, loadErr := manifest.Load(ws.ManifestPath)

				if loadErr != nil {
					return loadErr
				}

				manifest.MergeBuiltinPacks(loaded)
				store, openErr := index.Open(ws.IndexPath)

				if openErr != nil {
					return openErr
				}

				defer store.Close()

				nodeRepo := index.NewNodeRepo(store)
				edgeRepo := index.NewEdgeRepo(store)

				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Initial reindex …")

				if _, runErr := reindex.Run(reindex.Config{
					Root:            ws.Root,
					Repo:            nodeRepo,
					Edges:           edgeRepo,
					EdgeTypes:       loaded.EdgeTypes,
					WorkspaceIgnore: loaded.Workspace.Ignore,
					Logger:          logger,
				}); runErr != nil {
					return runErr
				}

				logger.Info("watch started", "root", ws.Root)

				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Watching for changes (Ctrl-C to stop)…")

				watcherInstance, newErr := watcher.New(ws.Root)

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

					if event.Kind == watcher.EventDelete {
						if delErr := nodeRepo.DeleteByPath(event.Path); delErr != nil {
							return delErr
						}

						return nil
					}

					absPath := filepath.Join(ws.Root, event.Path)

					stat, statErr := os.Stat(absPath)

					if statErr != nil {
						return nil // file already gone or unreadable
					}

					if stat.IsDir() {
						return nil
					}

					// Plan 3 ships full-tree reindex on each event for simplicity.
					// Plan 8 polish: replace with single-file partial reindex.
					_, runErr := reindex.Run(reindex.Config{
						Root:            ws.Root,
						Repo:            nodeRepo,
						Edges:           edgeRepo,
						EdgeTypes:       loaded.EdgeTypes,
						WorkspaceIgnore: loaded.Workspace.Ignore,
						Logger:          logger,
					})

					if runErr != nil {
						logger.Warn("watch handler reindex failed", "path", event.Path, "err", runErr.Error())

						return runErr
					}

					return nil
				}

				return watcherInstance.Run(ctx, handler)
			})
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
