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
    operators (key=value, key:value, key!=value, key<value, key<=value,
    key>value, key>=value); ranges use key=lo..hi. Edge traversal uses
    edge-type-> or edge-type<- and may chain multi-hop. Traversal
    shortcuts: tree=id, parent=id, root=id (qualified: tree:<alias>=id,
    parent:<alias>=id, root:<alias>=id, where <alias> is set via
    hierarchy on an edge type in tusk.toml). Recency shortcut:
    modified-since:<duration|ISO-date> (e.g. modified-since:7d,
    modified-since:2026-05-23). Combine with AND, OR, NOT, and parens.
    (Both : and = bind property comparisons; pick whichever reads
    better.)
  * Semantic (--semantic STRING): nearest-neighbor search over
    Ollama embeddings. The positional filter still applies as a
    pre-filter; pass a permissive filter like 'type=note' to search
    a whole type, or '' to search everything.
  * Hybrid: structural filter narrows the candidate set, then
    --semantic ranks it by cosine similarity.

Use --sort to order by one or more keys (prefix +/-), --take N to limit
results, --skip M to paginate. Use --include to expand each row with
body, edges, or properties (comma-separated; for semantic results body
is the best-matching chunk). Use --fields to project the rendered shape.
Use --format to pick compact or JSON output (default: compact for TTY,
JSON otherwise); --json is sugar for --format json.

```
tusk query <filter> [flags]
```

### Examples

```
  # Structural: all priority-1 tickets touched in the last week
  tusk query 'type=ticket AND priority=1 AND modified-since:7d'

  # Expand bodies and edges in one round-trip
  tusk query 'type=ticket' --include body,edges

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
      --format string         output format: compact|json (default: compact for TTY, json otherwise)
      --graph-edges strings   comma-separated edge-type names used by the graph expander; omit to inherit manifest
      --graph-expand          enable graph-expanded retrieval for this call (overrides [query.graph-expansion] enabled=false)
      --graph-weight float    per-hop weight applied to expanded candidates ([0,1]; <0 = inherit manifest) (default -1)
  -h, --help                  help for query
      --hops int              graph-expansion BFS depth (1 or 2; 0 = inherit manifest)
      --include strings       expand rows: body|edges|properties|units (comma-separated; units lists each file's sub-units)
      --json                  emit structured JSON (sugar for --format json)
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

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown vault into a graph

