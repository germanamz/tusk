package main

import "github.com/spf13/cobra"

func newEdgeCmd() *cobra.Command {
	edgeCmd := &cobra.Command{
		Use:   "edge",
		Short: "Manage edges between nodes (add, remove, list)",
		Long: `Manage edges between nodes.

Edges have a typed kind (declared in tusk.toml's [edge_types] table), a
source node, and a target node. They can be declared inline in a node's
frontmatter (the file owns them) or added via "tusk edge add" (attributed
to a synthetic CLI source so they survive reindex of the involved files).

Use "tusk edge list" with --from/--to/--kind to filter.`,
	}

	edgeCmd.AddCommand(newEdgeAddCmd())
	edgeCmd.AddCommand(newEdgeRemoveCmd())
	edgeCmd.AddCommand(newEdgeListCmd())

	return edgeCmd
}

// cliSourcePath is the synthetic source_path attributed to edges added via
// `tusk edge add`. Edges declared in frontmatter use the actual file path; the
// CLI marker keeps the two populations distinct so reindex's per-file UpsertAll
// doesn't clobber CLI-added edges.
const cliSourcePath = "__cli__"
