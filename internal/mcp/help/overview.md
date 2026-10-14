# Tusk overview

Tusk is a **local** indexer over one vault: the folder holding
`tusk.toml` that the server found when it started (its absolute path is
in the server instructions). There is no remote service. Every tool call
operates on:

- the workspace root: markdown files (YAML frontmatter) and HTML files
  (`<meta name="tusk:type">`).
- `tusk.toml` at the root — the manifest, which declares node types,
  edge types, packs, embeddings, query defaults, and context aliases.
- `.tusk/index.db` under the root — the SQLite index. Mutating tools
  update it in the same call; external file edits require `tusk_reindex`.

## The core tools, by purpose

**Inspect:** `tusk_status`, `tusk_node_list`, `tusk_node_get`,
`tusk_edge_list`, `tusk_query`, `tusk_context`.

**Mutate (writes to `.md` frontmatter on disk):** `tusk_node_create`,
`tusk_node_modify`, `tusk_node_move`, `tusk_node_delete`,
`tusk_edge_add`, `tusk_edge_remove`.

**Reconcile:** `tusk_reindex` (after external edits),
`tusk_doctor` (validation + health: off-schema nodes, dangling edges,
property drift, embed queue).

**Compose:** `tusk_run` (invokes a manifest-declared alias).

## Schema is editable

If a tool fails because a node/edge type is not declared, you have two
in-MCP options: call `tusk_pack_add` to merge a built-in pack's types, or
edit the root's `tusk.toml` in your editor (the one task that uses a file edit, not
the shell) and then call `tusk_reload` to pick up the change. See
`tusk_help(topic: "manifest")`, `node-types`, `edge-types`.

## Deep dives

Call `tusk_help(topic: "<name>")` for: `workflow`, `node-types`,
`edge-types`, `manifest`, `filter`, `query`, `packs`.
