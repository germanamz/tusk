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
key=*                 # has a value
key!=*                # missing
key~=item             # a list property holds item
key=[a, b]            # a list property holds a and b, in any order
```

Examples: `type=ticket`, `priority>=2`, `estimate=1..5`,
`status!=done`, `owner!=*`, `labels~=bug`.

`NOT key=value` and `key!=value` are the exact opposite of
`key=value`, so they also match a node that lacks the property:
`status!=done` returns the tickets with no status too. Write
`status=* AND status!=done` to keep only tickets that set one.

## Missing properties

`key=*` matches a node whose `key` has a value, and `key!=*` (or
`NOT key=*`) matches one where it is missing. An absent key and a null
are missing. An empty string is a value, as it is for `required`. A list
needs at least one item, so `labels: []` is missing. On `path` and `id`
a lone `*` keeps its glob meaning (below).

```
type=service AND domain!=*          # services with no domain
type=ticket AND NOT (owner=* AND due=*)
```

## Lists

A `list-of` property is tested item by item. `~=` asks for one item and
`[a, b]` for all of them, in any order:

```
labels~=bug                # holds bug
labels=[bug, urgent]       # holds both (and maybe more)
labels!=[bug, urgent]      # lacks at least one of them, or has no labels
NOT labels~=bug            # no bug label, empty and missing lists included
```

Items in `[a, b]` are bare or quoted values, comma-separated. A list
item is compared in the declared item type, so `scores~=5` works on a
`list-of` int. A frontmatter key that isn't declared works with both
forms whether it holds a list or a single value (`tags~=x`). On a
declared list, a plain `labels=bug` is an error that points at `~=`,
and `<`, `>` and ranges don't apply. `~=` and `[a, b]` on a declared
single value are errors too.

An unquoted value can hold letters, digits, `-`, `_`, `/`, `.` and `:`,
plus the path-pattern wildcards `*` and `?` (below). Quote anything else:
`title="Auth bug"`, `title="café"`. A character the grammar doesn't
recognize is a parse error naming it and its column, so
`type=note & status=open` fails instead of running as `type=note`. Join
predicates with a space or `AND`, not `&`.

## Path patterns

`path` and `id` take glob patterns, so a filter can select a directory:

```
path=docs/product/*        # files directly in docs/product/
path=docs/product/**       # everything under docs/product/, at any depth
id=docs/*/index            # an index in each immediate subfolder of docs/
id=**/readme               # every readme, the root one included
path!=archive/**           # everything outside archive/
type=note AND path=docs/product/*
```

The pattern matches the whole value, from the vault root. `*` matches
anything except `/`, so it stays inside one folder, and `?` matches one
character other than `/`. A `**` segment crosses folders: `a/**/b`
matches `a/b` and `a/x/y/b`, and a trailing `/**` matches everything
inside the folder. These are the wildcard rules `[workspace] ignore`
uses. There are no `[...]` character classes.

Sub-unit rows (paragraphs, sections) share their file's path and extend
its id with `#…`, so a pattern matches them along with the file. Add
`type=<node-type>` to keep to files, as in the last example.

Patterns work with `=`, `:` and `!=`, including after an edge arrow
(`references-> path=docs/**`) and as the scope of a semantic query.
Wildcards on any other property (apart from a lone `*`, which means
"has a value"), with `<`/`>`, in a `lo..hi` range or in
`tree=`/`parent=`/`root=` are an error. Only an unquoted value is a
pattern, so quote it to match a literal `*`: `path="odd/file*.md"`.

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

## Relative dates

A `date` or `datetime` property compares against a date counted from
the day the query runs, so a rule like "not reviewed in 90 days" keeps
working without edits:

```
reviewed<today-90d          # more than 90 days ago
due<=today+2w               # due within two weeks
reviewed=today-1y..today    # in the last year
seen>=now-48h               # a datetime in the last 48 hours
```

`today` is the local calendar date and `now` the current instant. The
offset is a number and a unit: `h` hours, `d` days, `w` weeks, `m`
months, `y` years (`m` is months; there is no minutes unit). A month or
year back from the 31st lands on the last day of a shorter month. On a
`datetime`, `today` means local midnight. The literal works with every
operator, inside `[a, b]` on a list of dates, and in `[rule]` filters,
where it resolves each time doctor runs. Quote it to compare the text
literally. On a property that isn't declared as a date, an ordering
against `today` is an error.

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

On its own, the arrow matches nodes with at least one such edge. The
term right after it constrains the node on the other end, and it can be
any term the grammar accepts: a property predicate, `NOT <term>`, a
parenthesized group, `modified-since:`, a hierarchy shortcut, or another
traversal.

```
references-> type=spec                          # links to a spec
references-> NOT domain=product                 # links to a non-product node
references-> (domain=technical OR domain=wip)   # links to either domain
references-> modified-since:7d                  # links to something changed this week
references-> tree=docs/area                     # links into that subtree
```

The arrow takes one term, the way `AND` binds. `blocks-> status=open
priority=high` finds high-priority nodes that block an open node. To put
both conditions on the blocked node, group them:
`blocks-> (status=open priority=high)`. For a `NOT` on the outer node
after a bare traversal, write `blocks-> AND NOT status=done`.

Chain for multi-hop: `mentions-> tagged-> type=tag`. A chain can be at
most 5 hops deep, counting hops nested inside groups.

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

`modified-since:` reads the file's modification time. To compare a
date stored in frontmatter, use a relative date (above).

## Path refs

When an edge type sets `paths = true`, pages record the workspace paths
they name in inline code, links and images, or (under a paths-only type)
a frontmatter value. `names-path` finds those pages:

```
names-path=server/ledger/core/service.go   # names the file, or a directory above it
names-path=server/ledger/                  # names that directory (or server/)
names-path=server/**                       # names anything under server/ (a glob)
type=technical AND NOT names-path=server/** # technical pages that name no server path
names-path:describes=server/ledger/        # only refs of the describes edge type
```

A literal path also matches a page that names a directory above it,
since a directory ref covers what it holds. A glob (`*`, `?`, `**`)
matches the named paths themselves; quote the value to keep `*` literal.
`!=` negates. `names-path` matches refs of every paths edge type;
`names-path:<edge-type>=` (or `!=`) matches one type's refs only. Refs belong to file rows, so every sub-unit row passes
`!=` and `NOT names-path=`; pair a negated test with `type=`. A glob
that starts with a wildcard can also match spans
that only look like paths (`node.Node`), so anchor it in a real folder.
Add `include: ["paths"]` to see where each page names them.

## Reference

The package docs live at `internal/filter` in the repository; the
public surface is `NewParser(input).Parse() (Expr, []ParseError)`,
`Validate(Expr, manifest) []ValidationError`, and
`Compile(Expr, CompileOptions) (string, []any, error)`.
