---
type: package
title: internal/filter — filter grammar
import-path: github.com/germanamz/tusk/internal/filter
status: stable
---

# internal/filter

Filter grammar for the index. Lexer → AST → SQL compiler that powers `tusk node list <expr>` and the `tusk_query` MCP tool. Supports property predicates (`=`/`:`, `!=`, `<`, `<=`, `>`, `>=`, range `lo..hi`), edge traversal (`edge-type->`, `edge-type<-`), traversal shortcuts (`tree=id`, `parent=id`, `root=id`, plus their qualified forms `tree:<alias>=id`, `parent:<alias>=id`, `root:<alias>=id`), boolean composition (`AND`/`OR`/`NOT`/parens), and multi-hop paths.

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

### Traversal-shortcut hierarchy resolution

`tree=<id>`, `parent=<id>`, and `root=<id>` operate over an edge type declared as a hierarchy in `tusk.toml` (see `manifest` package docs). Resolution:

- `tree:<alias>=<id>` (qualified) walks the edge whose `hierarchy = "<alias>"`.
- `tree=<id>` (unqualified) walks the edge with `hierarchy-default = true`, or the sole hierarchy edge if only one is declared, or the bare `parent` edge under the back-compat synthesis. If none of these resolve, validation fails with the declared aliases listed.

Hierarchy resolution happens in `Validate` (against the manifest); `Compile` reads the resolved edge name from the AST and emits `type = ?` SQL bound to that name.
