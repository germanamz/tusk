---
title: tusk doctor
---

## tusk doctor

Surface validation warnings, dangling edges, and index health issues

### Synopsis

Run read-only health checks against the workspace and index.

Doctor reports:
  * Off-schema nodes (type not declared in tusk.toml).
  * Property drift (frontmatter values whose type does not match the
    manifest declaration).
  * Dangling edges (edges whose target node no longer exists).
  * Embedding queue depth and last-reindex timestamp.

Doctor never modifies state, so it is safe to run while "tusk watch"
is active.

```
tusk doctor [flags]
```

### Examples

```
  # Health snapshot after a manifest change
  tusk pack add gtd
  tusk doctor

  # Quick check before starting an MCP session
  tusk doctor && tusk mcp
```

### Options

```
  -h, --help   help for doctor
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first agent brain: index a markdown vault into a graph

