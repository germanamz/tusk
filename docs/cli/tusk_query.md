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
    hierarchy on an edge type in tusk.toml). Combine with AND, OR, NOT,
    and parens. (Both : and = bind property comparisons; pick whichever
    reads better.)
  * Semantic (--semantic STRING): nearest-neighbor search over
    Ollama embeddings. The positional filter still applies as a
    pre-filter; pass a permissive filter like 'type=note' to search
    a whole type, or '' to search everything.
  * Hybrid: structural filter narrows the candidate set, then
    --semantic ranks it by cosine similarity.

Use --sort to order by one or more keys (prefix +/-), --take N to limit
results, --skip M to paginate, and --json for machine-readable output.

```
tusk query <filter> [flags]
```

### Examples

```
  # Structural: all priority-1 tickets touched this week
  tusk query 'type=ticket AND priority=1 AND modified>=2026-05-09'

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
  -h, --help              help for query
      --json              emit structured JSON
      --semantic string   rank results by cosine similarity to this query string (requires [embeddings] in tusk.toml)
      --skip int          skip the first M rows (requires --take)
      --sort string       sort spec, e.g., +priority,-due,+modified
      --take int          limit results to N rows
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first agent brain: index a markdown vault into a graph

