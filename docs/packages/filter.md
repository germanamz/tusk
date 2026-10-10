---
type: package
title: internal/filter — filter grammar
import-path: github.com/germanamz/tusk/internal/filter
status: stable
---

# internal/filter

Filter grammar for the index. Lexer → AST → SQL compiler that powers `tusk node list <expr>` and the `tusk_query` MCP tool. Supports property predicates (`=`/`:`, `!=`, `<`, `<=`, `>`, `>=`, range `lo..hi`), glob patterns on `path` and `id` (`path=docs/product/*`), edge traversal (`edge-type->`, `edge-type<-`) whose inner term can be anything the grammar accepts, traversal shortcuts (`tree=id`, `parent=id`, `root=id`, plus their qualified forms `tree:<alias>=id`, `parent:<alias>=id`, `root:<alias>=id`), path refs (`names-path=<path|glob>`), boolean composition (`AND`/`OR`/`NOT`/parens), and multi-hop paths.

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
- `OuterTypes(Expr) []string` — the node-type names that `type=` equalities select
  at the outer level, through AND and OR but not under NOT or past an edge arrow.
  Doctor uses it to decide whether a rule opts into sub-units.
- `OuterNamesPaths(Expr) []*NamesPathPredicate` — the non-negated `names-path` predicates at the outer level, through AND and OR but not under NOT or past an edge arrow. `tusk query --include paths` narrows each row's listed paths to the refs these match (`NamesPathPredicate.Matches`).
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

The `+tag`/`-tag` shorthand in §10 of the master spec was dropped from v1.c. Composing the tags pack with the filter grammar uses explicit `tagged -> tag/<name>` predicates instead.

### Path patterns

A bare value holding `*` or `?` is a glob pattern. The lexer accepts both characters in bare values, and the parser decides where a pattern may appear: only on `path` or `id`, only with `=`/`:` or `!=`. Anywhere else (another property, an ordering operator, a range bound, a hierarchy shortcut's node id) is a parse error at the value, so `title=draft*` can't run as a literal comparison that silently matches nothing. A quoted value is never a pattern, which makes quoting the escape for a literal `*`.

`Compile` emits `<column> REGEXP ?` for `=` and `NOT (<column> REGEXP ?)` for `!=`, binding the anchored regex from [`pathglob.ToRegexp`](pathglob.md). The column carries the usual depth prefix, so a pattern works after an edge arrow, and `--semantic` scoping gets it for free because the semantic path ranks the ids this SQL returns. `compilePattern` is the only place the compiler spells "matches a regex". SQLite resolves `REGEXP` to the `regexp()` function that [`internal/index`](index.md) registers; a port to another engine would swap just that function's operator.

### Path refs

`names-path=<value>` matches the pages that name a workspace path in inline code, through the `path_refs` table that edge types with `paths = true` fill (see [`internal/pathref`](pathref.md)). `names-path` is a reserved key, like the hierarchy shortcuts: any comparison operator after it routes to its parser, and only `=`, `:` and `!=` are accepted, so `names-path>x` is an error rather than a silent property comparison.

A literal value is cleaned with `pathref.Clean` and matches a page whose refs name it or any directory above it, since a directory ref covers what it holds. The compiler expands the ancestors itself: `names-path=server/ledger/core.go` becomes `id IN (SELECT source_id FROM path_refs WHERE target IN (?, ?, ?))` bound to the path, `server/ledger` and `server`. A bare value with `*` or `?` is a glob over the named paths themselves, compiled through `compilePattern` to `target REGEXP ?` with no ancestor expansion; quoting makes the wildcards literal. `!=` wraps the membership in `NOT`. The id column carries the usual depth prefix, so the predicate works after an edge arrow. Refs belong to file rows, so a sub-unit row never matches `=` and always matches `!=` or `NOT names-path=`; a negated test wants a `type=` beside it when the result can hold sub-units.

`Validate` fails when no edge type sets `paths = true`, following the hierarchy-shortcut precedent, so a query in a vault that records no path refs gets an explanation instead of zero rows. It also rejects a literal that isn't workspace-relative (an absolute path, or one climbing out with `..`).

### Edge inner term

The arrow takes the single next term (`parseNot`), the same way `AND` binds: a predicate of any kind, `NOT <term>`, or a parenthesized group. `a-> x=1 y=2` is `(a-> x=1) AND y=2`; `a-> (x=1 y=2)` puts both on the target. `MaxTraversalDepth` counts hops nested in groups as well as bare chains.

`compileWhere` takes a depth: 0 is the outer `nodes` table (bare column names), and depth `d` is the target alias `n<d-1>` of the enclosing edge predicate. Every expression kind compiles at any depth, and the edge predicate parenthesizes its inner SQL so a disjunction stays inside the edge join. A shortcut inside an edge predicate never sets the default `ORDER BY`, since its ordering property belongs to the traversal target.

### Traversal-shortcut hierarchy resolution

`tree=<id>`, `parent=<id>`, and `root=<id>` operate over an edge type declared as a hierarchy in `tusk.toml` (see `manifest` package docs). Resolution:

- `tree:<alias>=<id>` (qualified) walks the edge whose `hierarchy = "<alias>"`.
- `tree=<id>` (unqualified) walks the edge with `hierarchy-default = true`, or the sole hierarchy edge if only one is declared, or the bare `parent` edge under the back-compat synthesis. If none of these resolve, validation fails with the declared aliases listed.

Hierarchy resolution happens in `Validate` (against the manifest); `Compile` reads the resolved edge name from the AST and emits `type = ?` SQL bound to that name.
