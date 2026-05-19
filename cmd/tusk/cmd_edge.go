package main

import (
	"github.com/spf13/cobra"
)

func newEdgeCmd() *cobra.Command {
	edgeCmd := &cobra.Command{
		Use:   "edge",
		Short: "Manage edges between nodes (add, remove, list)",
		Long: `Manage edges between nodes.

Edges have a typed kind (declared in tusk.toml's [edge_types] table), a
source node, and a target node. They are declared inline in a node's
frontmatter (the file owns them); "tusk edge add" mutates that frontmatter
and refreshes the index for the affected source file.

Use "tusk edge list" with --from/--to/--type to filter.`,
	}

	edgeCmd.AddCommand(newEdgeAddCmd())
	edgeCmd.AddCommand(newEdgeRemoveCmd())
	edgeCmd.AddCommand(newEdgeListCmd())

	return edgeCmd
}

// cliSourcePath is the legacy synthetic source_path attributed to edges added
// via earlier versions of `tusk edge add`. Retained for the migration path
// that sweeps legacy rows; new writes always use the real source file path.
const cliSourcePath = "__cli__"
