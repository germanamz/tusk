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

- `take` — limit to N rows. Semantic queries return 5 files unless set.
- `skip` — paginate (requires `take`).
- `sort` — sort spec, e.g. `"+priority,-due"`.
- `include` — expand each row with `body` / `edges` / `properties` /
  `units` / `paths` in one round-trip. On semantic results, `body`
  gives every row's best unit its `text` (by default only the top row's
  has it). `units` lists each file's sub-unit outline on
  a structural query; semantic rows carry matched units without it.
  `paths` lists the workspace paths each page names (see "Path refs").
- `max_units` — keep at most N matched units per file. Defaults to 2
  on semantic rows; the `include=units` outline is uncapped unless set.
- `fields` — keep only these keys of each row, in order:
  `["id","properties.owner"]`. Names: `id`, `type`, `path`, `title`,
  `parent_id`, `score`, `snippet`, `body`, `properties`, `edges`,
  `paths`, `matched_units` (or `units`), and `properties.<name>` for one
  property. An expandable name loads without `include`;
  `matched_units` brings `units_total`, and `explain` scores stay. An
  unknown name is an error.
- `format` — `"json"` (default) or `"compact"`.
- `count` — `true` returns `{"count": N}`, the total of every match, in
  place of the rows. `take` and `skip` don't apply; no `semantic`.
- `group_by` — `"type"` or a property name: counts per value,
  largest first, as `{"count": N, "group_by": "...", "groups":
  [{"value": ..., "count": n}]}`. A list property counts a node under
  each item; no value (missing, null, empty list) is `"value": null`.
- `min_score` — minimum similarity score for semantic results
  (MCP default 0.5). Lower this when a query returns no hits.

## Matched units

A semantic row is a pointer into its file. Each row carries
`matched_units`, the passages that matched, best first. Every scored
passage folds into its innermost section, so one finding is one row:

```
{"id": "docs/ledger#S1.2", "heading": "Balances", "lines": [11, 13],
 "score": 0.82, "snippet": "Balances are derived from entries, never stored."}
```

- `lines` is `[start, end]`, 1-based and inclusive, counted from the
  top of the file (frontmatter included). Open the file there instead
  of reading all of it. HTML units have no `lines`.
- `heading` names a section. The `snippet` is the passage that matched.
- A passage before the first heading is its own row, with no `heading`;
  its id names it (`#P1` is the first paragraph, `#L2` the second list
  item).
- `units_total` on the file row is how many units there were before
  `max_units` cut the list.
- The top row's best unit carries `text`, its section's own markdown
  read from the file (no heading line, no subsections), in place of the
  `snippet`, and `text_lines`, the lines that text covers. Answer from
  it before reading the file. A section over 2 KB comes as a window of
  whole paragraphs and items around the passage that matched; read on
  from `text_lines` for the rest. `include=["body"]` gives every row's
  best unit its text. A file edited since it was indexed gets no text.
- Scores are rounded to three decimals.

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
- `names-path:<edge-type>=<path>` narrows to one paths edge type's refs.
- For one page, `tusk_node_get(id=..., include=["paths"])` returns the
  same list.

## Word matches

If `[query] lexical-weight` is set in the manifest, a semantic query's
words count too: each passage's similarity score is raised by how well
it matches the query's words (BM25), so a passage that uses the exact
terms can outrank one whose vector is only a little closer. The raise
closes that share of the gap to 1, so a score never drops and never
passes 1, and whatever cleared `min_score` before still clears it. Queries that name the identifiers, flags or phrases you're
after benefit most. `explain=true` adds `lexical_score` (0 to 1) next to
`cosine_score`.

## Title matches

A query naming a file's title (or its file name when untitled) lifts
every passage of that file, so `which topic does ledger-sync consume`
finds `ledger-sync` over notes that only mention it. `[query]
title-weight` sets how much (default 0.5, 0 = off); `explain=true` adds
`title_score` (share of the title's words the query names, 0 to 1).

## Demoting results

`[query.type-weights]` (`note = 0.8`) and `[query.path-weights]`
(`"incidents/**" = 0.6`) multiply a file's final score, passages
included, before `min_score`. `explain=true` adds `weight`.

## Explain

`explain=true` adds per row: `cosine_score` and `final_score` (the score
it ranked by) always; `lexical_score`, `title_score`, `weight` when set
in the manifest; `graph_score` and `distance` when graph expansion ran.

## Graph expansion

If `[query.graph-expansion]` is enabled in the manifest, `tusk_query`
walks N hops out from each semantic match along declared edge types
and blends the per-hop weight into the final score. Override per
call:

- `graph_expand` (bool) — force on/off, ignore manifest default.
- `hops` (1 or 2) — BFS depth.
- `graph_weight` ([0,1]) — per-hop weight.
- `graph_edge_types` ([string]) — edge types to walk.
- `explain` (bool) — per-row score trace; see Explain.

## When semantic returns nothing

In order of likelihood:

1. `min_score` too high — try `0.3` or `0.2`.
2. Workspace embeddings stale — `tusk_reindex` triggers re-embed of
   changed nodes; `tusk_status` shows queue depth.
3. `[embeddings]` not configured — `tusk_doctor` flags this.
