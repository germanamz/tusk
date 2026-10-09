---
type: package
title: internal/filter — filter grammar
import-path: github.com/germanamz/tusk/internal/filter
status: stable
---

# internal/filter

Filter grammar for the index. Lexer → AST → SQL compiler that powers `tusk node list <expr>` and the `tusk_query` MCP tool. Supports property predicates (`=`/`:`, `!=`, `<`, `<=`, `>`, `>=`, range `lo..hi`), glob patterns on `path` and `id` (`path=docs/product/*`), edge traversal (`edge-type->`, `edge-type<-`) whose inner term can be anything the grammar accepts, traversal shortcuts (`tree=id`, `parent=id`, `root=id`, plus their qualified forms `tree:<alias>=id`, `parent:<alias>=id`, `root:<alias>=id`), boolean composition (`AND`/`OR`/`NOT`/parens), and multi-hop paths.

## Public surface

- `NewParser(input string).Parse() (Expr, []ParseError)` — string → AST.
- `Validate(Expr, manifest.Manifest) []ValidationError` — resolves each property
  predicate's declared type against the manifest (within its conjunctive `type=`
  scope) and stamps `ResolvedType`/`EnumValues` onto the AST for the compiler.
- `Compile(Expr, CompileOptions) (string, []any, error)` — AST → parameterized SQL.
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

### Edge inner term

The arrow takes the single next term (`parseNot`), the same way `AND` binds: a predicate of any kind, `NOT <term>`, or a parenthesized group. `a-> x=1 y=2` is `(a-> x=1) AND y=2`; `a-> (x=1 y=2)` puts both on the target. `MaxTraversalDepth` counts hops nested in groups as well as bare chains.

`compileWhere` takes a depth: 0 is the outer `nodes` table (bare column names), and depth `d` is the target alias `n<d-1>` of the enclosing edge predicate. Every expression kind compiles at any depth, and the edge predicate parenthesizes its inner SQL so a disjunction stays inside the edge join. A shortcut inside an edge predicate never sets the default `ORDER BY`, since its ordering property belongs to the traversal target.

### Traversal-shortcut hierarchy resolution

`tree=<id>`, `parent=<id>`, and `root=<id>` operate over an edge type declared as a hierarchy in `tusk.toml` (see `manifest` package docs). Resolution:

- `tree:<alias>=<id>` (qualified) walks the edge whose `hierarchy = "<alias>"`.
- `tree=<id>` (unqualified) walks the edge with `hierarchy-default = true`, or the sole hierarchy edge if only one is declared, or the bare `parent` edge under the back-compat synthesis. If none of these resolve, validation fails with the declared aliases listed.

Hierarchy resolution happens in `Validate` (against the manifest); `Compile` reads the resolved edge name from the AST and emits `type = ?` SQL bound to that name.
