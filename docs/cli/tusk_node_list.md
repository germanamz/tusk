---
title: tusk node list
---

## tusk node list

List nodes from the index, optionally filtering by expression

### Synopsis

List nodes from the index, optionally filtering by expression.

The filter is a property and edge-traversal expression. Property
predicates use comparison operators (key=value, key:value, key!=value,
key<value, key<=value, key>value, key>=value); ranges use key=lo..hi.
Edge traversal uses edge-type-> or edge-type<- and may chain multi-hop.
Traversal shortcuts: tree=id, parent=id, root=id (qualified: tree:<alias>=id,
parent:<alias>=id, root:<alias>=id, where <alias> is set via hierarchy on an
edge type in tusk.toml). Recency shortcut: modified-since:<duration|ISO-date>
(e.g. modified-since:7d, modified-since:2026-05-23). Combine with AND, OR,
NOT, and parens. (Both : and = bind property comparisons; pick whichever
reads better.) Output is a tab-aligned table of id, type, title, path.

Use --sort to order by one or more keys (prefix +/-), --take N to limit,
and --skip M to paginate. For structural-and-semantic ranking, use
"tusk query" with --semantic.

```
tusk node list [filter] [flags]
```

### Examples

```
  # All open tickets, highest priority first
  tusk node list 'type=ticket AND status=open' --sort '-priority'

  # Page 2 of 20 most-recently-modified notes
  tusk node list type=note --sort '-modified' --take 20 --skip 20

  # Notes touched in the last 48 hours
  tusk node list 'type=note AND modified-since:48h'

  # Pipe a single id into "node get"
  tusk node list 'type=ticket AND priority=1' --take 1 | awk 'NR==2 {print $1}' | xargs tusk node get
```

### Options

```
  -h, --help          help for list
      --skip int      skip the first M rows (requires --take)
      --sort string   sort spec, e.g., +priority,-due,+modified
      --take int      limit results to N rows
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk node](tusk_node.md)	 - Manage individual nodes (create, get, list, modify, move, delete)

