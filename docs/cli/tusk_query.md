---
title: tusk query
---

## tusk query

Run a structural, semantic, or hybrid query against the index

### Synopsis

Run a query against the index.

Three modes, all driven by the same command:

  * Structural (default): the filter argument is a property and
    edge-traversal expression. Property predicates use comparison
    operators — = and : (equal), != (not equal), < <= > >= (ordering) —
    written directly after the key (e.g. status=open, priority>=2);
    ranges use key=lo..hi. Ordering and range operators compare by the
    property's declared type: int numerically, date/datetime
    chronologically, enum by declared order (a value name or a 0-based
    index). path and id also take glob patterns with = and !=: * stays
    inside one folder, ? is one character, and a ** segment crosses
    folders (e.g. path=docs/product/*, path=docs/**, id=**/index); quote
    the value to match a literal *. Edge traversal uses edge-type-> (outgoing) or edge-type<-
    (incoming) and may chain multi-hop. The one term after the arrow (a
    predicate, NOT term, or parenthesized group) constrains the linked
    node, e.g. blocks-> (status=open OR status=wip). Traversal shortcuts:
    tree=id, parent=id, root=id, each optionally qualified by a hierarchy
    alias (e.g. tree:wbs=id) set via hierarchy on an edge type in
    tusk.toml. Recency shortcut: modified-since: a duration or ISO date
    (e.g. modified-since:7d, modified-since:2026-05-23). Path refs:
    names-path=<path> finds the pages that name a workspace path in
    inline code, or a directory above it; names-path=<glob> matches the
    named paths themselves (e.g. names-path=server/**). It needs an edge
    type with paths = true in tusk.toml. Combine with AND, OR, NOT, and
    parens. (Both : and = bind property comparisons; pick whichever
    reads better.)
  * Semantic (--semantic STRING): nearest-neighbor search over
    Ollama embeddings. The positional filter still applies as a
    pre-filter; pass a permissive filter like 'type=note' to search
    a whole type, or '' to search everything.
  * Hybrid: structural filter narrows the candidate set, then
    --semantic ranks it by cosine similarity.

Use --sort to order by one or more keys (prefix +/-), --take N to limit
results, --skip M to paginate. Use --include to expand each row with
body, edges, properties, units or paths (comma-separated; for semantic
results body is the best-matching chunk). Use --fields to project the rendered shape.
Use --format to pick compact or JSON output: with no --include/--fields
the default is the tab-aligned table, and once a shape flag is set it is
compact at a TTY and JSON when piped. --json (or --format json) forces
JSON regardless.

Matched units: semantic rows always carry matched_units, the passages that
matched. Each scored passage folds into its innermost section, so one finding
is one row: the section's id, heading, and line range, with the passage's
score and snippet. A passage before the first heading is its own row.
--include units lists every file's sub-unit outline on structural queries.
Every unit carries lines [start, end] (1-based, inclusive, counted from the
top of the file; absent for HTML), numbered under [workspace] line-numbering.
--max-units N keeps the first N units per file (best first on semantic rows,
document order on the outline); units_total reports how many there were.

Path refs: --include paths lists the workspace paths each page names in
inline code, one entry per path and edge type, with every line it is named
on, the innermost section holding that line, and exists, a live check of the
disk. Only spans whose first segment exists at the workspace root count as
paths. Under a names-path filter the list keeps the refs the filter matched.

Sub-unit addresses: a sub-unit's id appends a structural address to the file
id, e.g. notes/doc#S1.2P3 (paragraph 3 of section 1.2) or notes/doc#S1.1T1R0C0
(a table cell). Addresses stay stable under in-place edits and shift only when
the document is restructured.

```
tusk query <filter> [flags]
```

### Examples

```
  # Structural: all priority-1 tickets touched in the last week
  tusk query 'type=ticket AND priority=1 AND modified-since:7d'

  # Expand bodies and edges in one round-trip
  tusk query 'type=ticket' --include body,edges

  # Which pages describe this file (or a directory above it), and where
  tusk query 'names-path=server/ledger/core/service.go' --include paths

  # Pure semantic over all notes
  tusk query 'type=note' --semantic 'cache invalidation strategies'

  # Hybrid: filter to design notes, then rank by similarity
  tusk query 'type=note AND kind=design' --semantic 'sqlite write contention'

  # Pipe top match into "node get"
  tusk query 'type=note' --semantic 'auth flow' --json --take 1 \
    | jq -r '.[0].id' \
    | xargs tusk node get
```

### Options

```
      --explain               include a per-row score-contribution trace (cosine/graph/final/distance) in the response when graph expansion is active
      --fields strings        project rendered rows to these fields (comma-separated)
      --format string         output format: compact|json (default: tab-aligned table; with --include/--fields, compact for TTY and json when piped)
      --graph-edges strings   comma-separated edge-type names used by the graph expander; omit to inherit manifest
      --graph-expand          enable graph-expanded retrieval for this call (overrides [query.graph-expansion] enabled=false)
      --graph-weight float    per-hop weight applied to expanded candidates in [0,1]; omit to inherit manifest (default inherit)
  -h, --help                  help for query
      --hops int              graph-expansion BFS depth (1 or 2; omit to inherit manifest)
      --include strings       expand rows: body|edges|properties|units|paths (comma-separated; units lists each file's sub-units, paths the workspace paths each page names)
      --json                  emit structured JSON (sugar for --format json)
      --max-units int         keep at most N matched units per file (0 = all); units_total reports the count before the cut
      --min-score float       drop semantic results below this similarity score (default 0 = no filter; MCP tusk_query defaults to 0.5). When graph expansion is active, this filters the blended final score, not the bare cosine.
      --no-graph-expand       disable graph-expanded retrieval for this call (beats [query.graph-expansion] enabled=true)
      --semantic string       rank results by cosine similarity to this query string (requires [embeddings] in tusk.toml)
      --skip int              skip the first M rows (requires --take)
      --sort string           sort spec, e.g., +priority,-due,+modified
      --take int              limit results to N rows
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

