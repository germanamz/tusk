---
type: package
title: internal/filter — filter grammar
import-path: github.com/germanamz/tusk/internal/filter
status: stable
last-touched-by: Plan 5
---

# internal/filter

TaskWarrior-flavored filter grammar. Lexer → AST → SQL compiler that powers `tusk node list <expr>` and the `tusk_query` MCP tool. Supports property predicates, edge traversal (`-> edge`, `<- edge`), boolean composition, and multi-hop paths.

## Public surface

- `Parse(input string) (*Node, error)` — string → AST.
- `Compile(*Node, manifest) (string, []any, error)` — AST → parameterized SQL.
- `Lexer`, token kinds, AST node types — internal but exposed for tests.

## Notes

The `+tag`/`-tag` shorthand in §10 of the master spec was dropped from v1.c. Composing the tags pack with the filter grammar uses explicit `tagged -> tag/<name>` predicates instead.
