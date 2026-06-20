---
title: tusk doctor
---

## tusk doctor

Surface validation warnings, dangling edges, and index health issues

### Synopsis

Run health checks against the workspace and index.

Doctor reports:
  * Off-schema nodes (type not declared in tusk.toml).
  * Property drift (frontmatter values whose type does not match the
    manifest declaration).
  * Dangling edges (edges whose target node no longer exists).
  * Embedding queue depth and last-reindex timestamp.
  * Sub-unit pane: per-kind counts, deduped sub-units, oversize payloads.
  * Graph-expansion pane: the resolved [query.graph-expansion] settings,
    unknown edge types referenced from the block, and a no-op warning
    when the feature is enabled with weight=0.

Doctor also auto-migrates any legacy "__cli__" / "__mcp__" edge rows in the
index back into the source node's markdown frontmatter — pass --no-migrate
for a diagnostic-only run.

Sub-unit addresses: sub-units are indexed under structural addresses appended
to the file id, e.g. notes/doc#S1.2P3. The "deduped sub-units" count is the
number of content groups shared by two or more sub-units (embedded once, then
shared). Sections are aggregated from their descendants, never embedded, so
they are not flagged as missing embeddings.

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

  # Diagnostic-only run; do not migrate legacy edge rows
  tusk doctor --no-migrate
```

### Options

```
  -h, --help         help for doctor
      --no-migrate   skip auto-migration of legacy __cli__/__mcp__ edge rows (diagnostic-only run)
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

