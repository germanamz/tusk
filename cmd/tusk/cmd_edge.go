package main

import (
	"github.com/spf13/cobra"
)

func newEdgeCmd() *cobra.Command {
	edgeCmd := &cobra.Command{
		Use:   "edge",
		Short: "Manage edges between nodes (add, remove, list)",
		Long: `Manage edges between nodes.

An edge has a typed kind (declared in tusk.toml under [edge-types.<name>]),
a source node, and a target node.

Edges live in the source node's frontmatter. Any top-level frontmatter key
whose name matches a declared edge type becomes an edge after reindex.
Example: an edge type [edge-types.blocks] makes "blocks: tickets/T-002"
(or a list of target ids) on any allowed source type valid frontmatter.

  # tusk.toml
  [edge-types.blocks]
  from        = ["ticket"]
  to          = ["ticket"]
  cardinality = "many-to-many"

  # tickets/T-001.md
  ---
  type: ticket
  blocks:
    - tickets/T-002
    - tickets/T-003
  ---

"tusk edge add" is a CLI convenience: it mutates the source file's
frontmatter exactly as if you opened the file and added the key by hand,
then refreshes the index for that file. "tusk edge remove" is the
symmetric operation. The file remains the source of truth — a clean
"git pull && tusk reindex" reproduces the full edge graph from disk.

Edges of an edge type whose declaration includes ordered = "<prop>" sort
by the named property on each source node. Example:

  [edge-types.wbs-parent]
  from        = ["wbs-node"]
  to          = ["wbs-node"]
  cardinality = "many-to-one"
  ordered     = "order"
  hierarchy   = "wbs"

  # children sorted by their own "order: int" property under their parent.

Body wikilinks ([[path/to/target]]) materialize as edges of any edge type
the manifest declares with "wikilinks = true" — a navigational shorthand
useful for prose, distinct from typed frontmatter edges.

Use "tusk edge list" with --from/--to/--type to filter.`,
	}

	edgeCmd.AddCommand(newEdgeAddCmd())
	edgeCmd.AddCommand(newEdgeRemoveCmd())
	edgeCmd.AddCommand(newEdgeListCmd())

	return edgeCmd
}
