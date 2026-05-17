---
title: tusk edge remove
---

## tusk edge remove

Remove a specific edge by source, kind, and target

### Synopsis

Remove a specific edge identified by its source, kind, and target.

Only edges attributed to the CLI ("__cli__" source path) can be removed
this way. Edges declared in a node's frontmatter must be removed by
editing the node and reindexing.

```
tusk edge remove [flags]
```

### Examples

```
  # Remove a blocks edge added via "edge add"
  tusk edge remove --type blocks --source tickets/T-001 --target tickets/T-002
```

### Options

```
  -h, --help            help for remove
      --source string   source node id
      --target string   target node id
      --type string     edge type
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk edge](tusk_edge.md)	 - Manage edges between nodes (add, remove, list)

