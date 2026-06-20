# Tusk overview

Tusk is a **local** indexer. It runs in the working directory the MCP
server was launched from — there is no remote service. Every tool call
operates on:

- `./` — the workspace root: markdown files (YAML frontmatter) and
  HTML files (`<meta name="tusk:type">`).
- `./tusk.toml` — the manifest, which declares node types, edge types,
  packs, embeddings, query defaults, and context aliases.
- `./.tusk/index.db` — the SQLite index. Mutating tools update it in
  the same call; external file edits require `tusk_reindex`.

## The 14 tools, by purpose

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

If a tool fails because a node/edge type is not declared, the fix is to
edit `./tusk.toml`. There is no MCP tool for this — open the file
directly. See `tusk_help(topic: "manifest")`, `node-types`, `edge-types`.

## Deep dives

Call `tusk_help(topic: "<name>")` for: `workflow`, `node-types`,
`edge-types`, `manifest`, `filter`, `query`, `packs`.
