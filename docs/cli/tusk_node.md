---
title: tusk node
---

## tusk node

Manage individual nodes (create, get, list, modify, move, delete)

### Synopsis

Manage individual nodes in the workspace.

A node is one markdown file with YAML frontmatter declaring its type and
properties. Example:

  ---
  type: ticket
  title: Fix login bug
  priority: high
  blocks:
    - tickets/T-002
  ---

  # Fix login bug

  ...body...

Top-level frontmatter keys split into two namespaces, enforced by the
manifest at load time:

  * Property keys (e.g. priority, title) — declared in
    [node-types.<type>].properties in tusk.toml.
  * Edge keys (e.g. blocks) — names that match an [edge-types.<name>]
    declaration in tusk.toml. The value is a target node id (scalar) or
    list of target ids; reindex turns each into an indexed edge.

A few keys are reserved: "type" (required, picks the node-type schema),
"title", and any "status-property" declared by an active behavior pack.

Body wikilinks ([[path/to/target]]) materialize as implicit "references"
edges when the manifest declares that edge type.

The node subcommands are thin wrappers over the same internal service the
watcher and reindex use, so creating a node by CLI and saving a file in
your editor produce identical index state.

Use "tusk node create" to author a new file, "tusk node modify" to change
frontmatter properties, "tusk node move" to atomically rename a node and
rewrite all referring edges, and "tusk node list" to query the index with
the filter grammar.

### Options

```
  -h, --help   help for node
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first agent brain: index a markdown vault into a graph
* [tusk node create](tusk_node_create.md)	 - Create a new node file and index it
* [tusk node delete](tusk_node_delete.md)	 - Delete a node file and remove it from the index
* [tusk node get](tusk_node_get.md)	 - Print the markdown file for a node by id
* [tusk node list](tusk_node_list.md)	 - List nodes from the index, optionally filtering by expression
* [tusk node modify](tusk_node_modify.md)	 - Modify a node's frontmatter properties
* [tusk node move](tusk_node_move.md)	 - Atomically rename a node and rewrite all referring edges

