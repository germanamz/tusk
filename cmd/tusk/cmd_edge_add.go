package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newEdgeAddCmd() *cobra.Command {
	var (
		edgeType string
		source   string
		target   string
	)

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a typed edge from one node to another",
		Long: `Add a typed edge from one node to another by writing the edge into the
source node's frontmatter.

The edge kind must be declared in tusk.toml's ` + "`[edge-types.<name>]`" + `. The
source's node type must be in the edge's "from" list, and the target's
node type must be in the edge's "to" list.

What this command actually does:

  1. Reads the source file's current frontmatter.
  2. Adds the target under the edge-name key, respecting cardinality:
       * one-to-one / many-to-one: scalar string; rejects on conflict.
       * one-to-many / many-to-many: list; appends if absent (dedup).
  3. Atomically rewrites the file with the new frontmatter.
  4. Reindexes the source file so the new edge is queryable immediately.

Idempotent: adding an edge that already exists is a no-op. To replace a
single-target edge, run "tusk edge remove" first.

The change is durable: the edge lives in git-tracked markdown, not in the
index database. Running "rm .tusk/index.db && tusk reindex" recovers the
same graph state.`,
		Example: `  # Mark T-001 as blocking T-002
  tusk edge add --type blocks --source tickets/T-001 --target tickets/T-002

  # Add multiple edges as part of a script
  tusk edge add --type mentions --source tickets/T-003 --target notes/2026-05-16
  tusk edge add --type owned-by --source tickets/T-003 --target people/alice`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if edgeType == "" || source == "" || target == "" {
				return fmt.Errorf("--type, --source, and --target are required")
			}

			ws, loaded, resolveErr := resolveWorkspace()

			if resolveErr != nil {
				return resolveErr
			}

			store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			service := newNodeService(ws, store, loaded, nil, cmd.ErrOrStderr())

			if addErr := service.AddEdge(edgeType, source, target); addErr != nil {
				return addErr
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Added edge %s: %s → %s\n", edgeType, source, target)

			return nil
		},
	}

	addCmd.Flags().StringVar(&edgeType, "type", "", "edge type (must be declared in tusk.toml)")
	addCmd.Flags().StringVar(&source, "source", "", "source node id (workspace-relative path without extension)")
	addCmd.Flags().StringVar(&target, "target", "", "target node id")

	return addCmd
}
