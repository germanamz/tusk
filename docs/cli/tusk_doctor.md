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
    manifest declaration). Drift for a deleted or renamed node is not
    reported — it is an orphan with no repair path.
  * Dangling edges (edges whose target node no longer exists).
  * Embedding queue depth, and embed-retry rows (a failing embedder that
    keeps re-enqueueing) with their attempt count and last error.
  * Sub-unit pane: per-kind counts, deduped sub-units, oversize payloads.
  * Graph-expansion pane: the resolved [query.graph-expansion] settings,
    unknown edge types (valid but undeclared — the walker skips them),
    invalid edge types (malformed refs that break every --semantic query),
    and no-op warnings when the feature is enabled with weight=0 or an
    empty edge-types list.

Doctor also auto-migrates any legacy "__cli__" / "__mcp__" edge rows in the
index back into the source node's markdown frontmatter — pass --no-migrate
for a diagnostic-only run. A legacy row whose edge type is no longer declared
in tusk.toml cannot be migrated; it is reported as skipped and left in place.

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
  tusk pack add kanban
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

