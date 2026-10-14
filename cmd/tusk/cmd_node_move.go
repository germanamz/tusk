package main

import (
	"fmt"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/leaseconfig"
	"github.com/germanamz/tusk/internal/node"
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
state.

Every wikilink keeps naming the note it named. A relative link
([[../old-id]]) is re-rendered from where its file lives after the move, the
moved note's own links included. A file-name link ([[old-name]]) stays as
written while the name still finds the note, becomes [[new-name]] when the
move renames the file, and becomes the full id when another note already has
that name. Links to that other note by name are rewritten to its full id too,
and their files count as referring files.`,
		Example: `  # Move a note into a subdirectory
  tusk node move notes/hello notes/intros/hello.md

  # Same move using the id-style target (extension inherited from source)
  tusk node move notes/hello notes/intros/hello

  # After move, confirm references were rewritten
  tusk edge list --to notes/intros/hello`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, loaded, resolveErr := resolveWorkspace(cmd)

			if resolveErr != nil {
				return resolveErr
			}

			store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			plan, renameErr := node.Rename(
				ws.Root,
				index.NewNodeRepo(store),
				index.NewEdgeRepo(store),
				index.NewFileStateRepo(store),
				index.WorkerID(),
				leaseconfig.Resolve(loaded.Lease.TTLSeconds),
				loaded.EdgeTypes,
				loaded.NodeTypes,
				index.NewPropertyDriftRepo(store),
				loaded.LineNumbering(),
				loaded.LinkCase(),
				args[0],
				args[1],
			)

			if renameErr != nil {
				return renameErr
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Renamed %s → %s (rewrote %d referring file(s))\n", plan.OldID, plan.NewID, len(plan.AffectedFiles))

			return nil
		},
	}

	return moveCmd
}
