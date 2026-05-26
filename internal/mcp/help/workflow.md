# Typical agent workflow

A productive loop against a Tusk workspace usually goes:

## 1. Orient

`tusk_status` returns node counts by type, edge count, embed queue
depth, and last reindex time. Cheap; call it once at session start.

`tusk_context` returns the workspace's pre-composed warm context if
the manifest declares a `[context]` block (pinned nodes + named
aliases). Use it for high-signal grounding.

## 2. Look up

`tusk_query` is the primary retrieval surface. Three modes:

- **Structural:** filter expression (`type=ticket AND priority=1`).
- **Semantic:** rank by cosine similarity to `semantic: "..."`.
- **Hybrid:** structural pre-filter, semantic ranking.

Use `include=["body", "edges", "properties"]` to fetch shape in one
round-trip. See `tusk_help(topic: "filter")` and
`tusk_help(topic: "query")`.

`tusk_node_list` and `tusk_edge_list` are narrower lookups when you
already know the type or endpoints.

## 3. Mutate

Prefer `tusk_node_create` / `tusk_node_modify` / `tusk_edge_add` over
writing files yourself. They normalize frontmatter, derive edges from
declared edge-types, and update the index atomically. `tusk_edge_add`
mutates the source node's `.md` frontmatter — there are no
out-of-band edge rows.

## 4. Reconcile

If you (or another process) edited files outside Tusk, call
`tusk_reindex` to bring the index up to date. Call `tusk_doctor` to
surface off-schema nodes, property drift, dangling edges, and embedding
queue health.

## 5. Recover

If `tusk_doctor` reports "off-schema node type X" or
`tusk_node_create` rejects an unknown type/edge, the manifest doesn't
declare it. Open `./tusk.toml` and add the declaration —
`tusk_help(topic: "node-types")` and `tusk_help(topic: "edge-types")`
show the shape. Then `tusk_reindex`.
