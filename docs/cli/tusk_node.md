---
title: tusk node
---

## tusk node

Manage individual nodes (create, get, list, modify, move, delete)

### Synopsis

Manage individual nodes in the workspace.

A node is one markdown file with TOML frontmatter declaring its type and
properties. The node subcommands are thin wrappers over the same internal
service the watcher and reindex use, so creating a node by CLI and creating
one by saving a file in your editor produce identical index state.

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

