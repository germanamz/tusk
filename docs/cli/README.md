# Tusk CLI reference

This reference is generated from the Cobra help text in `cmd/tusk/`.
Edit the help strings, then run `make docs`.

- **[Workflows](workflows.md)** — multi-command recipes (bootstrap, ingest,
  query, MCP wiring, health checks).

## Commands

- [`tusk`](tusk.md) — Local-first memory for agents: index a markdown vault into a graph
  - [`tusk context`](tusk_context.md) — Compose a warm-context digest from the manifest [context] block
  - [`tusk doctor`](tusk_doctor.md) — Surface validation warnings, dangling edges, and index health issues
  - [`tusk edge`](tusk_edge.md) — Manage edges between nodes (add, remove, list)
    - [`tusk edge add`](tusk_edge_add.md) — Add a typed edge from one node to another
    - [`tusk edge list`](tusk_edge_list.md) — List edges, optionally filtered by source, target, or kind
    - [`tusk edge remove`](tusk_edge_remove.md) — Remove a specific edge by source, kind, and target
  - [`tusk init`](tusk_init.md) — Initialize a Tusk workspace in the current directory
  - [`tusk mcp`](tusk_mcp.md) — Run the long-running MCP server (stdio or SSE)
  - [`tusk node`](tusk_node.md) — Manage individual nodes (create, get, list, modify, move, delete)
    - [`tusk node create`](tusk_node_create.md) — Create a new node file and index it
    - [`tusk node delete`](tusk_node_delete.md) — Delete a node file and remove it from the index
    - [`tusk node get`](tusk_node_get.md) — Print the markdown file for a node by id
    - [`tusk node list`](tusk_node_list.md) — List nodes from the index, optionally filtering by expression
    - [`tusk node modify`](tusk_node_modify.md) — Modify a node's frontmatter properties
    - [`tusk node move`](tusk_node_move.md) — Atomically rename a node and rewrite all referring edges
  - [`tusk pack`](tusk_pack.md) — Install and manage built-in type packs
    - [`tusk pack add`](tusk_pack_add.md) — Copy a built-in type pack's declarations into tusk.toml
  - [`tusk query`](tusk_query.md) — Run a structural, semantic, or hybrid query against the index
  - [`tusk reindex`](tusk_reindex.md) — Walk the workspace and bring the index up to date with disk
  - [`tusk reset`](tusk_reset.md) — Drop the local index and rebuild it from source files
  - [`tusk run`](tusk_run.md) — Run a manifest-declared alias by name
  - [`tusk status`](tusk_status.md) — Print a one-screen workspace summary
  - [`tusk watch`](tusk_watch.md) — Watch the workspace for external edits and keep the index in sync
