package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newEdgeRemoveCmd() *cobra.Command {
	var (
		edgeType string
		source   string
		target   string
	)

	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a specific edge by source, kind, and target",
		Long: `Remove a specific edge identified by its source, kind, and target.

The edge is removed from the source node's markdown frontmatter and the
index is updated to match. Removal is idempotent — removing an edge that
isn't there succeeds with no-op.

For multi-target edges (many-to-many, one-to-many) only the named target
is removed; sibling targets in the same list are preserved. When the
last target is removed, the edge-name key is dropped from frontmatter
entirely.

Legacy "__cli__" and "__mcp__" sentinel rows from pre-frontmatter
versions of "tusk edge add" / "tusk_edge_add" MCP calls are also swept
for the same (type, source, target) triple. "tusk doctor" auto-migrates
any remaining sentinel rows on its next run.`,
		Example: `  # Remove a blocks edge added via "edge add"
  tusk edge remove --type blocks --source tickets/T-001 --target tickets/T-002`,
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

			if removeErr := service.RemoveEdge(edgeType, source, target); removeErr != nil {
				return removeErr
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed edge %s: %s → %s\n", edgeType, source, target)

			return nil
		},
	}

	removeCmd.Flags().StringVar(&edgeType, "type", "", "edge type")
	removeCmd.Flags().StringVar(&source, "source", "", "source node id")
	removeCmd.Flags().StringVar(&target, "target", "", "target node id")

	return removeCmd
}
