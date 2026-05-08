package main

import "github.com/spf13/cobra"

func newEdgeCmd() *cobra.Command {
	edgeCmd := &cobra.Command{
		Use:   "edge",
		Short: "Manage edges (add, remove, list)",
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
