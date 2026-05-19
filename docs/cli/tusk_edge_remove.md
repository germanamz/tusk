---
title: tusk edge remove
---

## tusk edge remove

Remove a specific edge by source, kind, and target

### Synopsis

Remove a specific edge identified by its source, kind, and target.

The edge is removed from the source node's markdown frontmatter and the
index is updated to match. Any legacy "__cli__" or "__mcp__" rows for the
same (type, source, target) triple are also cleared from the index as a
back-compatibility sweep — those rows are remnants of pre-frontmatter
"tusk edge add" / "tusk_edge_add" MCP calls. "tusk doctor" auto-migrates
any remaining legacy rows back into source frontmatter (pass --no-migrate
to opt out).

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

