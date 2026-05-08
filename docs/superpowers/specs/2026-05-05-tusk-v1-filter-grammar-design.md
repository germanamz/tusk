---
type: spec
title: Plan 4 — Filter Grammar Spec
---

# Tusk v1 — Filter Grammar Design (Plan 4 sub-spec)

- **Status:** Draft
- **Date:** 2026-05-05
- **Author:** German Meza
- **Scope:** Implementation-shaped design for the structural filter grammar referenced by §10.1 and punted to a separate pass by §13.1 of the v1 rebuild design.
- **Successor of:** the brainstorm dialogue captured during Plan 4 setup.

This document is a sub-spec of `2026-05-05-tusk-v1-rebuild-design.md`. It refines §10.1 (filter grammar surface) into a concrete implementation plan: lexer tokens, parser productions, AST, manifest-aware validation, SQL compilation strategy, sort grammar, and CLI integration. The plan doc that follows (`2026-05-05-tusk-v1-4-filter-grammar.md`) implements this sub-spec.

---

## 1. Goal & Scope

Plan 4 delivers a TaskWarrior-flavored structural filter grammar, end-to-end:

- New package `internal/filter/` containing the lexer, parser, AST, manifest-aware validator, SQL compiler, and a separate sort-spec parser.
- New CLI command `tusk query <filter>` (structural-only in Plan 4; semantic flag lands in Plan 5).
- `tusk node list` accepts a positional filter expression. Plan 1b's `--type` flag is removed; tests are updated.
- `tusk edge list` is untouched (keeps `--from` / `--to` / `--type` flags).

### 1.1 In scope

Everything in §10.1 of the v1 rebuild design, **minus** the `+tag` / `-tag` shortcut (deferred to Plan 7 since the tag pack owns the path template):

- Property predicates with equality, comparators (`<`, `<=`, `>`, `>=`, `!=`), and ranges (`a..b`).
- Edge probes (`<edge>->`, `<edge><-`).
- Edge predicates (`<edge>->key=value`, `<edge><-key=value`) — 1-hop with target/source predicate.
- Multi-hop chains (`parent->parent->name="auth"`) bounded at depth 5.
- Traversal shortcuts (`tree=<id>`, `parent=<id>`, `root=<id>`) over the workspace's `parent` edge.
- Quoted string values (`"..."` and `'...'`) with `\"` / `\'` / `\\` escape.
- Boolean composition: `AND`, `OR`, `NOT`, parens. Whitespace acts as implicit `AND`.
- Sort spec: `+priority,-due,+modified` (TaskWarrior `+`/`-` style), parsed separately from the filter expression and fed via `--sort`.

### 1.2 Out of scope

- `+tag` / `-tag` shorthand (Plan 7).
- Semantic queries / `--semantic` flag (Plan 5).
- Pattern matching (`title~="auth"`), date keywords (`due=today`, `now-7d`).
- Configurable max-traversal-depth via manifest. Plan 4 hardcodes 5.
- `tusk node list` ↔ `tusk query` consolidation. Both commands ship.
- Multi-hop traversal beyond chained edge predicates (e.g., free-form Cypher-style path expressions).

## 2. Grammar Surface

### 2.1 Examples

```
# Equality, comparators, ranges
type=ticket
priority>=3
priority=2..4
created>=2026-04-01
status!=completed
title="Auth bug"
title='single-quoted is fine too'

# Edge probes (1-hop, no inner predicate)
blocks->          # has any outgoing blocks edge
blocks<-          # has any incoming blocks edge

# Edge predicates (1-hop, target/source predicate)
blocks->status=active
parent->type=project
blocks<-priority>=3

# Multi-hop chain (bounded depth 5)
parent->parent->name="auth"

# Traversal shortcuts (operate over the `parent` edge type)
tree=tickets/auth-epic        # all descendants
parent=tickets/auth-epic      # direct children
root=tickets/auth-epic        # all in same parent-tree

# Boolean composition
type=ticket AND status=active
type=ticket OR type=note
type=ticket AND NOT status=completed
(type=ticket AND priority>=3) OR (type=note AND created>=2026-04-01)

# Implicit AND between space-separated terms
type=ticket status=active priority>=3
# ≡ type=ticket AND status=active AND priority>=3

# Sort spec (separate parser, fed via --sort)
--sort +priority,-due,+modified
# + prefix or no prefix → ascending; - prefix → descending
```

### 2.2 Production grammar (EBNF-flavored)

```
expr        := or_expr
or_expr     := and_expr (OR and_expr)*
and_expr    := not_expr ((AND | implicit_and) not_expr)*
not_expr    := NOT not_expr | atom
atom        := '(' expr ')' | predicate
predicate   := traversal_shortcut | edge_predicate | property_predicate

edge_predicate     := IDENT (ARROW_OUT | ARROW_IN) (edge_predicate | property_predicate)?
property_predicate := IDENT op value
                    | IDENT '=' value DOTDOT value
op          := EQ | NE | LT | LE | GT | GE
value       := STRING | BARE_VALUE
traversal_shortcut := ('tree' | 'parent' | 'root') '=' value
```

**Operator precedence**, low → high: `OR`, `AND` (incl. implicit), `NOT`, atom.

`implicit_and` is not a token — it's a grammar construct: when `and_expr` parses one `not_expr` and the next token starts another atom (without an explicit `AND`), the parser treats it as `AND`.

## 3. Lexer (`internal/filter/lexer.go`)

A hand-rolled UTF-8 scanner emitting `Token{Kind, Value, Pos}`. Pos is the byte offset, used for column-aware errors.

### 3.1 Token kinds

| Kind | Matches | Notes |
|---|---|---|
| `IDENT` | `[a-zA-Z_][a-zA-Z0-9_-]*` | Property names, edge type names, traversal shortcut keywords. Keyword recognition (`AND` / `OR` / `NOT`) is a post-lex pass on `IDENT`s with case-insensitive comparison. |
| `STRING` | `"..."` or `'...'` with `\"` / `\'` / `\\` escape | Quote type is recorded for round-trip; both produce a unified `STRING` token. |
| `BARE_VALUE` | `[a-zA-Z0-9_/.\-:]+` (alphanumeric + path/date chars) | Used for unquoted values in value position (paths, dates, integers, IDs). The lexer emits `BARE_VALUE` when the parser is in value-expecting state; otherwise it would parse as `IDENT` (the parser disambiguates by context). |
| `EQ NE LT LE GT GE` | `=` `!=` `<` `<=` `>` `>=` | Maximal-munch: `<=` wins over `<`. |
| `ARROW_OUT ARROW_IN` | `->` `<-` | Maximal-munch: `<-` wins over `<` and `-`. |
| `DOTDOT` | `..` | Maximal-munch: `..` wins over `.`. |
| `LPAREN RPAREN` | `(` `)` | |
| `AND OR NOT` | Case-insensitive keyword match on `IDENT` | Resolved post-lex. |
| `EOF` | end-of-input | |

### 3.2 Lexing rules

1. Skip ASCII whitespace (`' '`, `'\t'`, `'\n'`, `'\r'`).
2. Maximal-munch over multi-character operators (`<=`, `>=`, `!=`, `->`, `<-`, `..`) before single-character variants.
3. After matching `STRING`, advance past the closing quote and the escape sequence. Unknown escape sequences surface as a parse-time error (with token position).
4. `BARE_VALUE` lex is lazy: the lexer emits an `IDENT` for the leading identifier-shaped run, and the parser explicitly requests a value lex when it expects one. This avoids ambiguity between `IDENT` (left of `=`) and `BARE_VALUE` (right of `=`).
5. Whitespace separates tokens; the parser knows that two consecutive predicates without an explicit `AND` form an implicit `AND`.

### 3.3 Error format

```
filter: unexpected character 'γ' at column 17 — expected operator after identifier
```

Errors carry `Pos` so callers can render the offending column and a caret.

## 4. Parser & AST (`internal/filter/parser.go`, `internal/filter/ast.go`)

### 4.1 Parser strategy

Recursive descent following the grammar in §2.2. One-token lookahead. The `Parser` type carries the input string, the token stream, and a small error accumulator (so a single parse can surface multiple syntax errors when reasonable).

### 4.2 AST node types

```go
package filter

// Expr is the root node interface.
type Expr interface{ exprNode() }

// Boolean composition.
type OrExpr  struct{ Left, Right Expr; Pos int }
type AndExpr struct{ Left, Right Expr; Pos int }
type NotExpr struct{ Inner Expr; Pos int }

// Predicates.
type PropertyPredicate struct {
	Property string
	Op       Op       // EQ, NE, LT, LE, GT, GE, RANGE
	Value    Value
	Pos      int
}

type EdgePredicate struct {
	EdgeType  string
	Direction Direction // OUTGOING, INCOMING
	Inner     Expr      // nil = probe-only; otherwise PropertyPredicate or another EdgePredicate (multi-hop chain)
	Pos       int
}

type TraversalShortcut struct {
	Kind   ShortcutKind // TREE, PARENT_OF, ROOT
	NodeID string
	Pos    int
}

// Values.
type Value interface{ valueNode() }
type StringValue struct{ V string }
type RangeValue  struct{ Min, Max string } // string form; type-coerced at compile time
```

`Pos` on each node tracks the byte offset where it began in the input — used by the validator for actionable error messages.

### 4.3 Multi-hop chains

`parent->parent->name="auth"` parses as:

```
EdgePredicate{
  EdgeType: "parent", Direction: OUTGOING,
  Inner: EdgePredicate{
    EdgeType: "parent", Direction: OUTGOING,
    Inner: PropertyPredicate{Property: "name", Op: EQ, Value: StringValue{"auth"}},
  },
}
```

The parser counts depth as it recurses and rejects depth > 5 with a hint to either flatten the chain or split into multiple queries.

## 5. Manifest-Aware Validation (`internal/filter/validate.go`)

After parse, before compile:

```go
func Validate(ast Expr, manifest manifest.Manifest) []ValidationError
```

### 5.1 Checks

- **Unknown edge type:** `EdgePredicate.EdgeType` must exist in `manifest.EdgeTypes`. Error with token position and a list of valid edge types.
- **Traversal shortcut requires `parent` edge type:** `tree=` / `parent=` / `root=` shortcuts compile against `parent` edges. If `parent` is not declared in `manifest.EdgeTypes`, error with a hint to add it or use explicit `<edge>-> ...` form.
- **Multi-hop depth > 5:** error with column, hint to split.

### 5.2 Non-checks

Unknown *property* names are NOT errors. Properties live in `properties_json` and may legitimately not be declared in the manifest (the type system is loose by design). The compiler emits `json_extract` and the row matches nothing if the property is missing — silent and consistent with how SQL handles missing JSON keys.

### 5.3 Error format

```go
type ValidationError struct {
	Pos     int
	Message string
	Hint    string // optional
}
```

CLI renders errors as:

```
filter: edge type "blockz" is not declared
  at column 17:
    type=ticket blockz->status=active
                    ^
  hint: did you mean "blocks"?
```

(Hint generation uses Levenshtein on the manifest's edge type names; trivially cheap.)

## 6. SQL Compilation (`internal/filter/compile.go`)

The compiler walks the validated AST and emits `(sql string, params []any)` for execution against the SQLite index.

### 6.1 Output shape

Final query template:

```sql
SELECT id, type, path, title, properties_json, last_mtime, last_size, last_checksum
FROM nodes
WHERE <where_clause>
ORDER BY <sort_keys>      -- empty if no --sort
LIMIT <take> OFFSET <skip>  -- LIMIT only when --take given; OFFSET only when --skip given
```

### 6.2 Predicate compilation

| AST node | SQL fragment |
|---|---|
| `PropertyPredicate{property, op, value}` for core columns (`id`, `type`, `path`, `title`) | `<col> <sql_op> ?` |
| `PropertyPredicate{property, op, value}` for non-core | `json_extract(properties_json, '$.<property>') <sql_op> ?` |
| Numeric comparators on JSON | `CAST(json_extract(...) AS INTEGER) <sql_op> ?` (auto-applied for `<`, `<=`, `>`, `>=`) |
| `RangeValue` | `<expr> BETWEEN ? AND ?` |
| `EdgePredicate{type, OUTGOING, nil}` (probe) | `EXISTS (SELECT 1 FROM edges WHERE source_id = nodes.id AND type = ?)` |
| `EdgePredicate{type, INCOMING, nil}` | `EXISTS (SELECT 1 FROM edges WHERE target_id = nodes.id AND type = ?)` |
| `EdgePredicate{type, OUTGOING, inner}` | `EXISTS (SELECT 1 FROM edges JOIN nodes target_n ON target_n.id = edges.target_id WHERE edges.source_id = nodes.id AND edges.type = ? AND <compile inner against target_n>)` |
| `EdgePredicate` chain (multi-hop) | Nested EXISTS subqueries, one JOIN per hop, each renaming `nodes` and `edges` aliases (`e1`, `e2`, `n1`, `n2`, …). |
| `TraversalShortcut{PARENT_OF, X}` | `EXISTS (SELECT 1 FROM edges WHERE source_id = nodes.id AND type = 'parent' AND target_id = ?)` |
| `TraversalShortcut{TREE, X}` | Recursive CTE descending parent edges from `X`, JOINed against `nodes`; depth bounded at 5. |
| `TraversalShortcut{ROOT, X}` | Recursive CTE up from `X` to find the root, then `TREE` semantics from that root. Two CTEs combined in a `WITH` block. |
| `OrExpr` | `(<left>) OR (<right>)` |
| `AndExpr` | `(<left>) AND (<right>)` |
| `NotExpr` | `NOT (<inner>)` |

### 6.3 Recursive CTE shape (used by `tree=`, `root=`)

```sql
WITH RECURSIVE descendants AS (
    SELECT target_id, 1 AS depth
    FROM edges
    WHERE source_id = ? AND type = 'parent'
    UNION ALL
    SELECT edges.target_id, descendants.depth + 1
    FROM descendants
    JOIN edges ON edges.source_id = descendants.target_id
    WHERE edges.type = 'parent' AND descendants.depth < 5
)
SELECT ... FROM nodes WHERE id IN (SELECT target_id FROM descendants)
```

The compiler chooses a unique CTE name when more than one shortcut is composed.

### 6.4 Why monolithic SQL

- One round-trip to SQLite — fast for typical workspaces.
- The query planner can optimize JOINs and CTEs together.
- Result projection happens once, not via Go-side composition.
- Spec §13.1 explicitly mandates: "Compilation target = parameterized SQL with recursive CTEs for traversal."

A hybrid (SQL for structural, Go for traversal) was considered and rejected — the complexity savings on the SQL side don't outweigh the performance and simplicity loss.

## 7. Sort Grammar (`internal/filter/sort.go`)

Tiny separate parser. Spec format: comma-separated keys, each prefixed with `+` (ascending, also default if no prefix) or `-` (descending).

```go
type SortKey struct {
	Property   string
	Descending bool
}

// ParseSort accepts "+priority,-due,modified" and emits []SortKey.
// Whitespace around commas is tolerated; an empty string returns an empty slice.
func ParseSort(spec string) ([]SortKey, error)
```

The compiler in §6 receives `[]SortKey` and emits `ORDER BY` with the same property-handling logic (core columns vs `json_extract`, with `CAST` for known-numeric properties when the manifest declares the type).

For Plan 4, the manifest doesn't yet carry per-property type information for nodes (Plan 7 introduces type packs that declare property types). So sort uses lexicographic ordering on `json_extract`'s text result for non-core properties. This is a known limitation — fine for v1, refined later.

## 8. CLI Integration

### 8.1 `tusk query <filter>` (new command)

`cmd/tusk/cmd_query.go`:

```bash
tusk query 'type=ticket status=active priority>=3'
tusk query 'parent->name="auth"' --sort -priority,+due --take 10
tusk query 'type=ticket' --take 25 --skip 50    # paginated: rows 51-75
tusk query 'type=ticket' --json
```

Flags:

- `--sort <spec>` — sort keys (see §7).
- `--take <N>` — limit results to N rows (LIMIT in SQL).
- `--skip <M>` — skip the first M rows (OFFSET in SQL). Requires `--take` (SQLite OFFSET without LIMIT is undefined behavior; the CLI surfaces a clear error).
- `--json` — emit structured JSON.

`tusk query` (no positional argument) errors with usage hint.
`tusk query ''` returns all nodes (filter compiles to `WHERE TRUE`).

### 8.2 `tusk node list` (updated)

Removes the `--type` flag from Plan 1b. Accepts a positional filter expression with the same grammar:

```bash
tusk node list                             # all nodes (no filter)
tusk node list 'type=ticket'               # equivalent to old --type ticket
tusk node list 'type=ticket' --sort -priority --take 10
tusk node list 'type=ticket' --take 25 --skip 50
```

Plan 1b's `cmd_node_list_test.go` is updated to use positional filters. Migration affects 2 test cases.

### 8.3 `tusk edge list` (untouched)

Plan 4 does not modify edge listing. `--from` / `--to` / `--type` flags remain.

## 9. Testing Strategy

Each pipeline stage gets its own table-driven test suite:

- **`lexer_test.go`** — `(input, expected []Token)` cases. Covers quoted strings (both kinds), escape sequences, maximal-munch (`<=` vs `<-`, `..` vs `.`), keyword case-insensitivity, error cases (unterminated string, invalid escape).
- **`parser_test.go`** — `(input, expected AST)` using a small AST-printer for golden comparison. Covers precedence, implicit AND, multi-hop, traversal shortcuts, parens.
- **`validate_test.go`** — `(AST, manifest, expected []ValidationError)`. Unknown edge type, missing `parent` edge for shortcuts, depth > 5.
- **`compile_test.go`** — `(AST, expected SQL string + params)`. Exact SQL match (anchored with `?` placeholders + `params` slice).
- **`sort_test.go`** — `(spec, expected []SortKey)` and error cases.
- **End-to-end** in `cmd/tusk/`: workspace fixture + filter expression + assertion on returned IDs. One file per command (`cmd_query_test.go`, `cmd_node_list_test.go`'s rewrite). Covers integration of all four pipeline stages plus CLI plumbing.

## 10. Open Questions / Residuals

1. **JSON-property type coercion.** Numeric comparators auto-`CAST` to INTEGER. Mixed-type properties (a property that's sometimes a string, sometimes an int) are rare in well-typed manifests; if hit, the SQL produces a coercion error which the CLI surfaces verbatim. A doctor warning surfaces this in Plan 8.

2. **`tree=` / `root=` over a non-`parent` edge.** Plan 4 hardcodes the edge type name `parent`. If a workspace uses a different edge name for hierarchy (`contains`, `parent_of`, etc.), the shortcuts won't work. Configurable via manifest is a future polish (`[workspace] hierarchy-edge = "parent"`).

3. **Implicit-AND vs `+tag`/`-tag` collision (Plan 7).** `type=ticket -auth` would parse today as `type=ticket AND -auth` where `-auth` is a value-position token; Plan 7's tag exclusion shorthand will need a parser hook. Plan 4 surfaces this as a known follow-up.

4. **Empty-result discrimination.** `tusk query 'type=widget'` (no widgets exist) returns the same exit code as `tusk query 'completely-bogus-filter'` after validation — both are valid empty results. CLI exit code is 0 in both cases. If the user wants "no results = exit 1", they can `--require-result` (out of scope for Plan 4).

5. **Performance on large workspaces.** The recursive CTE for `tree=` is bounded at depth 5, but a hub node with thousands of descendants could still produce slow queries. Plan 4 ships without query timeouts; observability lands in Plan 8 (`tusk doctor` shows slow queries).
