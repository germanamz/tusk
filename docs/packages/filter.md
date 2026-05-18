---
type: package
title: internal/filter — filter grammar
import-path: github.com/germanamz/tusk/internal/filter
status: stable
---

# internal/filter

Filter grammar for the index. Lexer → AST → SQL compiler that powers `tusk node list <expr>` and the `tusk_query` MCP tool. Supports property predicates (`=`/`:`, `!=`, `<`, `<=`, `>`, `>=`, range `lo..hi`), edge traversal (`edge-type->`, `edge-type<-`), traversal shortcuts (`tree=id`, `parent=id`, `root=id`, plus their qualified forms `tree:<alias>=id`, `parent:<alias>=id`, `root:<alias>=id`), boolean composition (`AND`/`OR`/`NOT`/parens), and multi-hop paths.

## Public surface

- `Parse(input string) (*Node, error)` — string → AST.
- `Compile(*Node, manifest) (string, []any, error)` — AST → parameterized SQL.
- `Lexer`, token kinds, AST node types — internal but exposed for tests.

## Notes

The `+tag`/`-tag` shorthand in §10 of the master spec was dropped from v1.c. Composing the tags pack with the filter grammar uses explicit `tagged -> tag/<name>` predicates instead.

### Traversal-shortcut hierarchy resolution

`tree=<id>`, `parent=<id>`, and `root=<id>` operate over an edge type declared as a hierarchy in `tusk.toml` (see `manifest` package docs). Resolution:

- `tree:<alias>=<id>` (qualified) walks the edge whose `hierarchy = "<alias>"`.
- `tree=<id>` (unqualified) walks the edge with `hierarchy-default = true`, or the sole hierarchy edge if only one is declared, or the bare `parent` edge under the back-compat synthesis. If none of these resolve, validation fails with the declared aliases listed.

Hierarchy resolution happens in `Validate` (against the manifest); `Compile` reads the resolved edge name from the AST and emits `type = ?` SQL bound to that name.
