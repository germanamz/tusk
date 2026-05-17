---
type: package
title: internal/filter — filter grammar
import-path: github.com/germanamz/tusk/internal/filter
status: stable
---

# internal/filter

Filter grammar for the index. Lexer → AST → SQL compiler that powers `tusk node list <expr>` and the `tusk_query` MCP tool. Supports property predicates (`=`/`:`, `!=`, `<`, `<=`, `>`, `>=`, range `lo..hi`), edge traversal (`edge-type->`, `edge-type<-`), traversal shortcuts (`tree=id`, `parent=id`, `root=id`), boolean composition (`AND`/`OR`/`NOT`/parens), and multi-hop paths.

## Public surface

- `Parse(input string) (*Node, error)` — string → AST.
- `Compile(*Node, manifest) (string, []any, error)` — AST → parameterized SQL.
- `Lexer`, token kinds, AST node types — internal but exposed for tests.

## Notes

The `+tag`/`-tag` shorthand in §10 of the master spec was dropped from v1.c. Composing the tags pack with the filter grammar uses explicit `tagged -> tag/<name>` predicates instead.
