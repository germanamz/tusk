---
title: tusk edge remove
---

## tusk edge remove

Remove a specific edge by source, kind, and target

### Synopsis

Remove a specific edge identified by its source, kind, and target.

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
any remaining sentinel rows on its next run.

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

