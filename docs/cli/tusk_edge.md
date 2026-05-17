---
title: tusk edge
---

## tusk edge

Manage edges between nodes (add, remove, list)

### Synopsis

Manage edges between nodes.

Edges have a typed kind (declared in tusk.toml's [edge_types] table), a
source node, and a target node. They can be declared inline in a node's
frontmatter (the file owns them) or added via "tusk edge add" (attributed
to a synthetic CLI source so they survive reindex of the involved files).

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

