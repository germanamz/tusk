package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
	"github.com/spf13/cobra"
)

func newNodeMoveCmd() *cobra.Command {
	moveCmd := &cobra.Command{
		Use:   "move <old-id> <new-rel-path>",
		Short: "Atomically rename a node and rewrite all referring edges",
		Long: `Atomically rename a node file and rewrite every edge that points at it.

The new path is workspace-relative. If you omit the file extension on the
target, the source file's extension (e.g. ".md") is inherited so the renamed
file keeps its on-disk extension. All other node files that reference the old
id are rewritten in the same transaction, so the index never observes a broken
state.`,
		Example: `  # Move a note into a subdirectory
  tusk node move notes/hello notes/intros/hello.md

  # Same move using the id-style target (extension inherited from source)
  tusk node move notes/hello notes/intros/hello

  # After move, confirm references were rewritten
  tusk edge list --to notes/intros/hello`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return loadErr
			}

			manifest.MergeBuiltinPacks(loaded)
			return withWorkspaceLock(ws, func() error {
				store, openErr := indexopen.OpenOrRebuild(cmd.Context(), indexopen.Config{
					IndexPath: ws.IndexPath,
					ReindexFactory: func(idx *index.Index) reindex.Config {
						return reindex.Config{
							Root:      ws.Root,
							Repo:      index.NewNodeRepo(idx),
							Edges:     index.NewEdgeRepo(idx),
							EdgeTypes: loaded.EdgeTypes,
						}
					},
					Logger: func(msg string) {
						_, _ = fmt.Fprintln(cmd.ErrOrStderr(), msg)
					},
				})

				if openErr != nil {
					return openErr
				}

				defer store.Close()

				plan, renameErr := node.Rename(ws.Root, index.NewNodeRepo(store), index.NewEdgeRepo(store), loaded.EdgeTypes, loaded.NodeTypes, args[0], args[1])

				if renameErr != nil {
					return renameErr
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Renamed %s → %s (rewrote %d referring file(s))\n", plan.OldID, plan.NewID, len(plan.AffectedFiles))

				return nil
			})
		},
	}

	return moveCmd
}
