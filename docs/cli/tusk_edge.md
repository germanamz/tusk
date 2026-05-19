---
title: tusk edge
---

## tusk edge

Manage edges between nodes (add, remove, list)

### Synopsis

Manage edges between nodes.

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

Body wikilinks ([[path/to/target]]) become implicit "references" edges
when the manifest declares an [edge-types.references] type — a navigational
shorthand useful for prose, distinct from typed frontmatter edges.

Use "tusk edge list" with --from/--to/--type to filter.

### Options

```
  -h, --help   help for edge
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first agent brain: index a markdown vault into a graph
* [tusk edge add](tusk_edge_add.md)	 - Add a typed edge from one node to another
* [tusk edge list](tusk_edge_list.md)	 - List edges, optionally filtered by source, target, or kind
* [tusk edge remove](tusk_edge_remove.md)	 - Remove a specific edge by source, kind, and target

