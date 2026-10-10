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
  `units` / `paths` in one round-trip. For semantic results, `body` is
  the best-matching chunk. `units` lists each file's sub-unit outline on
  a structural query; semantic rows carry matched units without it.
  `paths` lists the workspace paths each page names (see "Path refs").
- `max_units` — keep at most N matched units per file. Defaults to 3
  on semantic rows; the `include=units` outline is uncapped unless set.
- `fields` — project the rendered shape to a subset of fields.
- `format` — `"json"` (default) or `"compact"`.
- `min_score` — minimum similarity score for semantic results
  (MCP default 0.5). Lower this when a query returns no hits.

## Matched units

A semantic row is a pointer into its file. Each row carries
`matched_units`, the passages that matched, best first. Every scored
passage folds into its innermost section, so one finding is one row:

```
{"id": "docs/ledger#S1.2", "type": "section", "heading": "Balances",
 "heading_level": 2, "lines": [11, 13], "score": 0.82,
 "snippet": "Balances are derived from entries, never stored."}
```

- `lines` is `[start, end]`, 1-based and inclusive, counted from the
  top of the file (frontmatter included). Open the file there instead
  of reading all of it. HTML units have no `lines`.
- `heading` names a section. The `snippet` is the passage that matched.
- A passage before the first heading is its own row (a `paragraph`,
  `list-item`, ...).
- `units_total` on the file row is how many units there were before
  `max_units` cut the list.

## Path refs

Before changing a source file, ask which pages describe it:

```
tusk_query(filter="names-path=server/ledger/core/service.go", include=["paths"])
```

Each row's `paths` lists the refs that matched:

```
{"path": "server/ledger/core/service.go", "edge_type": "describes", "exists": true,
 "mentions": [{"line": 40, "section": "technical/ledger#S2", "heading": "Core service"}]}
```

- `exists` checks the disk when the query runs. `false` means the page
  names a path that is gone; update the page.
- `line` counts from the top of the file; `section` and `heading` name
  the innermost section holding it. HTML pages have no lines.
- Without a `names-path` filter, `paths` lists every path the page names.

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
