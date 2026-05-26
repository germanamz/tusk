# Query

`tusk_query` runs a structural filter, a semantic search, or both
together against the index.

## Three modes

**Structural (default).** The `filter` argument is a property /
edge-traversal expression. See `tusk_help(topic: "filter")`.

```
tusk_query(filter="type=ticket AND priority>=2 AND modified-since:7d")
```

**Semantic.** Set `semantic` to a natural-language query. The filter
acts as a pre-filter (`type=note` to limit to notes; `""` to search
everything). Requires `[embeddings]` in `tusk.toml`.

```
tusk_query(filter="type=note", semantic="cache invalidation strategies")
```

**Hybrid.** Structural narrows the candidate set, semantic ranks it.

```
tusk_query(filter="type=note AND kind=design",
           semantic="sqlite write contention")
```

## Common arguments

- `take` — limit to N rows.
- `skip` — paginate (requires `take`).
- `sort` — sort spec, e.g. `"+priority,-due"`.
- `include` — expand each row with `body` / `edges` / `properties` /
  `units` in one round-trip. For semantic results, `body` is the
  best-matching chunk.
- `fields` — project the rendered shape to a subset of fields.
- `format` — `"json"` (default) or `"compact"`.
- `min_score` — minimum similarity score for semantic results
  (MCP default 0.5). Lower this when a query returns no hits.

## Graph expansion

If `[query.graph-expansion]` is enabled in the manifest, `tusk_query`
walks N hops out from each semantic match along declared edge types
and blends the per-hop weight into the final score. Override per
call:

- `graph_expand` (bool) — force on/off, ignore manifest default.
- `hops` (1 or 2) — BFS depth.
- `graph_weight` ([0,1]) — per-hop weight.
- `graph_edge_types` ([string]) — edge types to walk.
- `explain` (bool) — include per-row `cosine_score` / `graph_score` /
  `final_score` / `distance` trace.

## When semantic returns nothing

In order of likelihood:

1. `min_score` too high — try `0.3` or `0.2`.
2. Workspace embeddings stale — `tusk_reindex` triggers re-embed of
   changed nodes; `tusk_status` shows queue depth.
3. `[embeddings]` not configured — `tusk_doctor` flags this.
