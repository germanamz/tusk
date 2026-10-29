---
type: package
title: internal/filter — filter grammar
import-path: github.com/germanamz/tusk/internal/filter
status: stable
---

# internal/filter

Filter grammar for the index. Lexer → AST → SQL compiler that powers `tusk node list <expr>` and the `tusk_query` MCP tool. Supports property predicates (`=`/`:`, `!=`, `<`, `<=`, `>`, `>=`, range `lo..hi`, presence `=*`/`!=*`, list membership `~=` and `=[a, b]`, relative dates `today-90d`/`now-48h`), glob patterns on `path` and `id` (`path=docs/product/*`), edge traversal (`edge-type->`, `edge-type<-`) whose inner term can be anything the grammar accepts, repeated hops (`edge-type->+`, `edge-type->{1,3}`), inverse labels (`depended-on-by->` for `depends-on<-`), traversal shortcuts (`tree=id`, `parent=id`, `root=id`, plus their qualified forms `tree:<alias>=id`, `parent:<alias>=id`, `root:<alias>=id`), path refs (`names-path=<path|glob>`), boolean composition (`AND`/`OR`/`NOT`/parens), and multi-hop paths.

## Public surface

- `NewParser(input string).Parse() (Expr, []ParseError)` — string → AST.
- `Validate(Expr, manifest.Manifest) []ValidationError` — resolves each property
  predicate's declared type against the manifest (within its conjunctive `type=`
  scope) and stamps `ResolvedType`/`EnumValues` onto the AST for the compiler.
- `ValidateStrict(Expr, manifest.Manifest) []ValidationError` — `Validate` plus
  checks for names the manifest can vouch for, at every depth: a `type=` value is
  a declared node type, a non-core property is declared on a node type in its
  `type=` scope (or on any type when the scope is empty), and an `=` / `!=` value
  of an `enum` or `list-of(enum)` property is one of its values. `Validate`
  accepts all three so ad-hoc queries can reach undeclared frontmatter;
  `internal/doctor` uses `ValidateStrict` for manifest rules, where a typo would
  match nothing forever. A property scoped only to undeclared types gets no
  property error, since the type error already covers it.
- `Compile(Expr, CompileOptions) (string, []any, error)` — AST → parameterized SQL.
  `CompileOptions.FilesOnly` wraps the WHERE clause as `(...) AND parent_id IS NULL`.
  `CompileOptions.Now` is the clock relative dates and `modified-since:<duration>`
  resolve against. The package never reads the clock itself: Compile fails when
  the filter needs one and `Now` is zero, and the driving adapters set it.
- `OuterTypes(Expr) []string` — the node-type names that `type=` equalities select
  at the outer level, through AND and OR but not under NOT or past an edge arrow.
  Doctor uses it to decide whether a rule opts into sub-units.
- `OuterNamesPaths(Expr) []*NamesPathPredicate` — the non-negated `names-path` predicates at the outer level, through AND and OR but not under NOT or past an edge arrow. `tusk query --include paths` narrows each row's listed paths to the refs these match (`NamesPathPredicate.Matches(edgeType, target)`).
- `SemanticRank(candidates, queryVector, fusion) []ScoredResult` — ranks chunk vectors by cosine similarity, one row per node at its best chunk. A candidate's `Lexical` (its `lexical.Scores` match) and `Title` (how much of its file's title the query names) raise its cosine as `1 − score = (1 − cosine) × (1 − fusion.Lexical × lexical) × (1 − fusion.Title × title)`: each match closes its share of the gap to 1, so it never lowers a score or pushes one past 1. The zero `Fusion` ranks by cosine alone. Each `ScoredResult` keeps the winning chunk's body, cosine, lexical and title scores for snippets and `--explain`.
- `RenderSnippetForQuery(body, query, maxRunes)` — the snippet window around the earliest query word in a chunk.
- `Lexer`, token kinds, AST node types — internal but exposed for tests.

Ordering (`<`, `<=`, `>`, `>=`) and range (`lo..hi`) operators are type-aware:
`int` compares numerically (integer affinity), `date`/`datetime` lexically (ISO
strings sort chronologically), and `enum` by declared order — the compiler
expands the operator into an `IN (...)` set over the satisfying member names,
accepting either a value name or a 0-based index as the bound. Resolution needs
the declared type, so the comparison falls back to integer affinity for an
undeclared property and errors when a name is declared on multiple node types
without a disambiguating `type=`.

## Notes

### Two-valued logic: NOT, != and missing properties

A comparison against a property a node doesn't have is NULL in SQL, and so is its `NOT`, so before #846 a node without `domain` matched neither `domain=billing` nor `NOT domain=billing`. The compiler now keeps every predicate two-valued, so `NOT x` is the exact complement of `x`. `NOT` compiles to `NOT ((x) IS TRUE)`, which reads NULL as false. `!=` compiles to the null-safe `IS NOT`, so `p!=v` means the same as `NOT p=v`. A qualified `type!=src:t` negates the whole `source = ? AND type = ?` pair the same way, so rows from another source pass. Ordering and ranges compare values, so they still skip a node without one.

A lone unquoted `*` is `AnyValue`: `p=*` holds when the property has a value and `p!=*` when it is missing. The test runs `EXISTS` over `json_each` and asks for a non-null item. That way a scalar counts (it is its own single row), while absent, null and an empty list don't. An empty string is a value, matching how doctor treats a `required` property. On a core column, `*` means non-empty, except on `path` and `id`, where a lone `*` stays the glob it always was.

### Lists

`p~=v` (`OpContains`) holds when a list property has an item equal to `v`. `p=[a, b]` (`ListValue`) holds when it has every item, in any order, and `p!=[a, b]` is its complement. Each item compiles to `EXISTS (SELECT 1 FROM json_each(<row>.properties_json, '$."p"') AS item WHERE item.value = ?)`, compared in the declared item type (`CAST(... AS INTEGER)` for int items, a resolved relative date for date items, `1`/`0` for a bareword bool). A list value ANDs the items. An undeclared property gets the same SQL, which matches whether it holds a list or a single value, since `json_each` yields a scalar as one row.

`Validate` stamps `ResolvedType = "list-of"` and `ItemType` for a declared list. It rejects a plain `=`/`!=` with one value on a list, since that would compare the whole list and match nothing, and points at `~=` or `[a, ...]` instead. It also rejects an ordering or range on a list, and `~=` or a list value on a declared single value or a core column. `ValidateStrict` checks each item of a list-of(enum) against its values.

### Relative dates

A bare value `today`, `now`, or either with a signed offset (`today-90d`, `today+2w`, `now-48h`) is a relative date on a `date` or `datetime` property, or on a list of them. `relative_date.go` parses it (units `h`, `d`, `w`, `m` for months, `y`; up to six digits) and resolves it on the compile clock. `today` is local midnight of `CompileOptions.Now`'s location, and months and years clamp to the end of a shorter month. A date renders as `YYYY-MM-DD` in that location, and a datetime as RFC3339 in UTC, the stored forms they compare against as text. `Validate` leaves a relative literal in place for the compiler (`normalizeTemporal` skips it). It reports one that opens like a relative date but doesn't parse, and an ordering against one on a property not declared as a date or datetime. A quoted value is never a relative date, which keeps quoting the way to compare text literally. `RangeValue` carries `MinBareword`/`MaxBareword` for that reason.

The `+tag`/`-tag` shorthand in §10 of the master spec was dropped from v1.c. Composing the tags pack with the filter grammar uses explicit `tagged -> tag/<name>` predicates instead.

### Path patterns

A bare value holding `*` or `?` is a glob pattern. The lexer accepts both characters in bare values, and the parser decides where a pattern may appear: only on `path` or `id`, only with `=`/`:` or `!=`. Anywhere else (another property, an ordering operator, a range bound, a hierarchy shortcut's node id) is a parse error at the value, so `title=draft*` can't run as a literal comparison that silently matches nothing. A quoted value is never a pattern, which makes quoting the escape for a literal `*`.

`Compile` emits `<column> REGEXP ?` for `=` and `NOT (<column> REGEXP ?)` for `!=`, binding the anchored regex from [`pathglob.ToRegexp`](pathglob.md). The column carries the usual depth prefix, so a pattern works after an edge arrow, and `--semantic` scoping gets it for free because the semantic path ranks the ids this SQL returns. `compilePattern` is the only place the compiler spells "matches a regex". SQLite resolves `REGEXP` to the `regexp()` function that [`internal/index`](index.md) registers; a port to another engine would swap just that function's operator.

### Path refs

`names-path=<value>` matches the pages that name a workspace path (in inline code, a link, or a paths-only frontmatter value), through the `path_refs` table that edge types with `paths = true` fill (see [`internal/pathref`](pathref.md)). `names-path` is a reserved key, like the hierarchy shortcuts: any comparison operator after it routes to its parser, and only `=`, `:` and `!=` are accepted, so `names-path>x` is an error rather than a silent property comparison.

`names-path` matches refs of every paths edge type. The qualified form `names-path:<edge-type>=<value>` (and `names-path:<edge-type>!=<value>`) matches only that type's refs, the way `tree:<alias>=` picks a hierarchy. The parser probes for it the same way: after `:`, an identifier followed by `=` or `!=` is the edge type, and anything else is the value, so `names-path:server/x.go` still means `names-path=server/x.go`. A stored path never holds a `=`, so reading `names-path:a=b` as qualified loses no match. The compiled subquery gains `AND type = ?`.

A literal value is cleaned with `pathref.Clean` and matches a page whose refs name it or any directory above it, since a directory ref covers what it holds. The compiler expands the ancestors itself: `names-path=server/ledger/core.go` becomes `id IN (SELECT source_id FROM path_refs WHERE target IN (?, ?, ?))` bound to the path, `server/ledger` and `server`. A bare value with `*` or `?` is a glob over the named paths themselves, compiled through `compilePattern` to `target REGEXP ?` with no ancestor expansion; quoting makes the wildcards literal. `!=` wraps the membership in `NOT`. The id column carries the usual depth prefix, so the predicate works after an edge arrow. Refs belong to file rows, so a sub-unit row never matches `=` and always matches `!=` or `NOT names-path=`; a negated test wants a `type=` beside it when the result can hold sub-units.

`Validate` fails when no edge type sets `paths = true`, following the hierarchy-shortcut precedent, so a query in a vault that records no path refs gets an explanation instead of zero rows. It also rejects a literal that isn't workspace-relative (an absolute path, or one climbing out with `..`), and a qualified edge type that isn't declared or doesn't set `paths = true`, suggesting a near spelling.

### Edge inner term

The arrow takes the single next term (`parseNot`), the same way `AND` binds: a predicate of any kind, `NOT <term>`, or a parenthesized group. `a-> x=1 y=2` is `(a-> x=1) AND y=2`; `a-> (x=1 y=2)` puts both on the target. `MaxTraversalDepth` counts hops nested in groups as well as bare chains.

`compileWhere` takes a depth: 0 is the outer `nodes` table (bare column names), and depth `d` is the target alias `n<d-1>` of the enclosing edge predicate. Every expression kind compiles at any depth, and the edge predicate parenthesizes its inner SQL so a disjunction stays inside the edge join. A shortcut inside an edge predicate never sets the default `ORDER BY`, since its ordering property belongs to the traversal target. The edge predicate reads `edges INDEXED BY` the index of the row it hangs off, `edges_source_target_idx` going out and `edges_target_idx` coming in: left to itself the planner probed by the edge type, a few values, and a filter arrow walked most of the edges table per row (#838).

### Traversal-shortcut hierarchy resolution

`tree=<id>`, `parent=<id>`, and `root=<id>` operate over an edge type declared as a hierarchy in `tusk.toml` (see `manifest` package docs). Resolution:

- `tree:<alias>=<id>` (qualified) walks the edge whose `hierarchy = "<alias>"`.
- `tree=<id>` (unqualified) walks the edge with `hierarchy-default = true`, or the sole hierarchy edge if only one is declared, or the bare `parent` edge under the back-compat synthesis. If none of these resolve, validation fails with the declared aliases listed.

Hierarchy resolution happens in `Validate` (against the manifest), which also copies the edge type's `parent-is-source` and `max-depth` onto the shortcut; `Compile` reads them from the AST and emits `type = ?` SQL bound to the resolved name. By default the child names its parent, so an edge's source is the child; `parent-is-source` swaps which column holds the parent (`hierarchyColumns`). `tree=` and `root=` are recursive CTEs joined with `UNION`, so a node the walk reaches twice adds a row the CTE has already seen and a cycle ends it. With no `max-depth` they walk the whole tree; with one they stop that many levels out. `root=` collects the ancestors with no parent of their own (plus the topmost ones `max-depth` reached) and walks down from them and from the seed, so a seed on a loop with no root still returns its own subtree, and a node with two roots returns both trees.

### Repeated hops

The lexer hands the arrow's suffix to the parser in the arrow token's `Value`: `+`, or a braced count up to the closing `}`. `parseHopCount` turns it into `EdgePredicate.MinHops`/`MaxHops`: `->+` is {1, 0} (no upper bound), `->{2}` {2, 2}, `->{1,3}` {1, 3}, `->{2,}` {2, 0}; a plain arrow, and `->{1}`, is one hop. Counts run from 1 to `MaxHopCount` (50), and the suffix must touch the arrow. A repeated arrow counts as one hop toward `MaxTraversalDepth`.

`compileRepeatedEdge` emits one uncorrelated recursive CTE per repeated arrow, so it runs once per query rather than per row. It starts one edge away from the nodes the inner term matches (from every edge of the type when there is none) and steps toward the row through the far end's index. The CTE carries `(node_id, depth)` under `UNION`: a bounded walk counts depth up to the maximum, and an unbounded one counts only up to the minimum (`MIN(depth + 1, m)`), so its rows stay finite and a cycle ends the walk. A row matches when it is in the CTE at `depth >= m`. The inner term compiles inside the CTE at depth+1; its params, and any CTE it needs, go ahead of the WHERE params (#704).

### Inverse labels

`Validate` rewrites an edge predicate whose type is an `inverse` label (`manifest.InverseOf`) to the edge type that declares it, keeping a source qualifier, and flips its direction. A declared edge type wins over an inverse of the same name, which the manifest loader rejects anyway. The "not declared" hint suggests inverse labels as well as edge types.
