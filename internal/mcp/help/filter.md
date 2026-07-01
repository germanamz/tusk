# Filter grammar

Used by `tusk_query` (the `filter` argument), `tusk_node_list` (the
`type` argument is a sugared subset), and the
`tusk_run`-dispatched aliases.

## Property predicates

```
key=value             # equality
key:value             # equality (alternate; same as =)
key!=value            # inequality
key<value             # less-than
key<=value
key>value
key>=value
key=lo..hi            # range, inclusive
```

Examples: `type=ticket`, `priority>=2`, `estimate=1..5`,
`status!=done`.

## Typed comparisons

Ordering (`<`, `<=`, `>`, `>=`) and range (`lo..hi`) compare by the
property's declared type:

- `int` — numeric.
- `date` / `datetime` — chronological (ISO values already sort that way).
- `enum` — by declared order. The bound is a value name or a 0-based
  index, so `priority>=medium` and `priority>=2` mean the same thing.

These resolve the property against its declared type, so add
`type=<node-type>` when a name is declared on more than one type
(e.g. `type=plan status>=shipped`). An out-of-range value or an
unparseable date errors rather than silently matching nothing.

## Boolean composition

`AND`, `OR`, `NOT`, parentheses. Examples:

```
type=ticket AND priority>=2 AND NOT status=done
(type=note OR type=design) AND modified-since:7d
```

## Edge traversal

```
edge-type->     # follow outgoing edges
edge-type<-     # follow incoming edges
```

Chain for multi-hop: `mentions-> tagged-> type=tag`.

## Traversal shortcuts (hierarchy)

For edge types declared with `hierarchy = "<alias>"`:

```
tree=<id>             # all descendants of <id> (transitive)
parent=<id>           # immediate children of <id>
root=<id>             # the root of <id>'s tree
```

Qualified forms target a specific hierarchy:
`tree:<alias>=<id>`, `parent:<alias>=<id>`, `root:<alias>=<id>`.

Unqualified `tree=`/`parent=`/`root=` walks the edge declared with
`hierarchy-default = true`, or the sole hierarchy edge if only one
exists. Validation fails with the declared aliases listed otherwise.

## Recency

```
modified-since:7d                # last 7 days
modified-since:24h
modified-since:2026-05-23        # absolute ISO date
```

## Reference

The package docs live at `internal/filter` in the repository; the
public surface is `NewParser(input).Parse() (Expr, []ParseError)`,
`Validate(Expr, manifest) []ValidationError`, and
`Compile(Expr, CompileOptions) (string, []any, error)`.
