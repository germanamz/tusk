# Advanced Filters — Design Spec

**Date:** 2026-04-05
**Initiative:** v0.6 Advanced Filters (ROADMAP.md)
**Scope:** Two sequential phases — quoted string support, then boolean operators.

---

## Current State

The filter system is a 3-stage pipeline:

```
User input → Lex() → []Token → Parse() → *FilterSet → Resolve() → *domain.TaskFilter → buildFilter() → SQL
```

- **Lexer** (`token.go`): Splits on whitespace. 4 token types: `TokenField`, `TokenTagInclude`, `TokenTagExclude`, `TokenText`.
- **Parser** (`parser.go`): Flat grammar — all terms implicitly AND'd. No nesting. `fieldValidators` map registers known fields. Returns `*FilterSet`.
- **Resolver** (`resolve.go`): Walks `FilterSet` fields, resolves DB lookups (parent/tree), injects default statuses `[pending, active]`. Returns `*domain.TaskFilter`.
- **SQL builder** (`sqlite/task.go`): `buildFilter()` maps `TaskFilter` fields to `WHERE` conditions, all AND'd.
- **MCP** (`mcp/tools.go`): Constructs `TaskFilter` directly from JSON params — bypasses the filter package entirely.

**Limitations addressed by this spec:**
1. No quoted strings — whitespace is the only delimiter, so multi-word title/description searches are impossible.
2. No `title:` or `description:` filter fields.
3. No boolean operators — all terms are implicitly AND'd with no way to express OR, NOT, or grouping.

---

## Phase 1: Quoted Strings + title/description Fields

### 1.1 Lexer: Quoted-Aware Scanner

Replace the whitespace-split approach in `Lex()` with a character-by-character scanner.

**Quoting rules:**
- When the scanner encounters `"`, it enters quoted mode — consuming all bytes (including whitespace) until the matching closing `"`.
- `\"` inside quotes is an escaped literal quote.
- An unclosed quote produces a `ParseError` with the opening position and message `"unclosed quoted string"`.
- Quotes are stripped from the resulting token value.

**Quoting works in two positions:**
- **Field values:** `title:"fix the bug"` → `TokenField{Key:"title", Value:"fix the bug"}`
- **Standalone:** `"fix the bug"` → `TokenText{Value:"fix the bug"}`

**Non-quoted behavior is identical to today.** The scanner still splits on whitespace outside of quotes. Existing expressions produce the same tokens.

### 1.2 New Filter Fields: title, description

Two new entries in `fieldValidators` (`parser.go`):

| Field         | Validator        | Accepted values     |
|---------------|------------------|---------------------|
| `title`       | `validateNonEmpty` | Any non-empty string |
| `description` | `validateNonEmpty` | Any non-empty string |

`validateNonEmpty` is a new trivial validator — rejects empty string, accepts everything else.

### 1.3 Resolver

Two new cases in `Resolve()`:
- `title` field → `tf.TitleContains = &value`
- `description` field → `tf.DescriptionContains = &value`

No DB lookups needed. Direct string assignment.

### 1.4 Domain: TaskFilter

Add two fields to `TaskFilter` (`domain/filter.go`):

```go
TitleContains       *string
DescriptionContains *string
```

### 1.5 SQL: buildFilter

Two new conditions in `buildFilter()` (`sqlite/task.go`):

- `TitleContains` → `LOWER(title) LIKE '%' || LOWER(?) || '%'`
- `DescriptionContains` → `LOWER(description) LIKE '%' || LOWER(?) || '%'`

Case-insensitive substring match using `LOWER()` on both sides.

### 1.6 MCP: tusk_task_list

Add `title` and `description` optional string parameters to `tusk_task_list` in `tools.go`. Map directly to `TaskFilter.TitleContains` and `TaskFilter.DescriptionContains`.

### 1.7 description Filter in CLI and MCP

The `description:` field works in both:
- **CLI:** `tusk list description:"auth middleware"` — parsed by the filter system.
- **MCP:** `description` parameter on `tusk_task_list` — mapped directly to `TaskFilter.DescriptionContains`.

---

## Phase 2: Boolean Operators (AND / OR / NOT + Parentheses)

### 2.1 New Token Types

Add to `token.go`:

| Token          | Lexed from | Notes |
|----------------|------------|-------|
| `TokenAnd`     | `AND`      | All-caps only. Lowercase `and` stays `TokenText`. |
| `TokenOr`      | `OR`       | All-caps only. |
| `TokenNot`     | `NOT`      | All-caps only. |
| `TokenLParen`  | `(`        | Implicit delimiter — no surrounding whitespace required. |
| `TokenRParen`  | `)`        | Same. |

**Parentheses** are treated as token boundaries by the character scanner. `(status:active)` lexes as three tokens: `LParen`, `Field{status,active}`, `RParen`.

**Keywords** are recognized only as standalone all-caps tokens. `title:"AND"` is a quoted string (Phase 1 handles this). `and` is `TokenText`.

### 2.2 Expression Tree AST

New types in `ast.go`:

```go
type Expr interface {
    exprNode()
}

type AndExpr struct { Children []Expr }
type OrExpr  struct { Children []Expr }
type NotExpr struct { Child Expr }

type TermExpr struct {
    Field *FieldFilter
    Tag   *TagFilter
    Text  string
}
```

`FilterSet` is retained for the input-building use case (`tusk add`, `tusk modify`) — it is not a query type.

### 2.3 Parser: Pratt Precedence Climbing

New function: `ParseExpr(input string) (Expr, []ParseError)`

**Precedence (highest → lowest):**
1. `NOT` (prefix, unary)
2. `AND` (explicit infix, left-associative) / implicit AND (adjacent terms)
3. `OR` (infix, left-associative)

**Grammar:**

```
expr     = or_expr
or_expr  = and_expr ("OR" and_expr)*
and_expr = unary (("AND")? unary)*
unary    = "NOT" unary | primary
primary  = "(" expr ")" | term
term     = field | tag_include | tag_exclude | text
```

Implicit AND between adjacent terms: `status:active +api` = `status:active AND +api`. This preserves existing filter semantics without requiring explicit `AND`.

Field validation runs on each `TermExpr` via the same `fieldValidators` map. Errors are collected, not fail-fast.

### 2.4 Domain: FilterExpr

New types in `domain/filter.go`:

```go
type FilterExpr interface {
    filterExpr()
}

type AndFilter  struct { Children []FilterExpr }
type OrFilter   struct { Children []FilterExpr }
type NotFilter  struct { Child FilterExpr }
type TermFilter struct { TaskFilter }
```

`TermFilter` embeds `TaskFilter` — each leaf holds the same resolved fields as today.

### 2.5 Resolver: ResolveExpr

New method: `Resolver.ResolveExpr(ctx context.Context, expr Expr) (domain.FilterExpr, []error)`

Recursive tree walk:
- `AndExpr` → `domain.AndFilter{Children}`
- `OrExpr` → `domain.OrFilter{Children}`
- `NotExpr` → `domain.NotFilter{Child}`
- `TermExpr` → `domain.TermFilter` (same resolution logic as today's per-field handling)

**Default status injection:** When the expression contains no explicit `status` term anywhere in the tree, the resolver wraps the user's expression in `AndFilter{TermFilter{Statuses: [pending, active]}, userExpr}`.

### 2.6 SQL: buildFilterExpr

New function: `buildFilterExpr(expr domain.FilterExpr) (ctePrefix string, where string, args []any)`

Recursive SQL generation:
- `AndFilter` → `(child1 AND child2 AND ...)`
- `OrFilter` → `(child1 OR child2 OR ...)`
- `NotFilter` → `NOT (child)`
- `TermFilter` → delegates to existing `buildFilter` logic for the embedded `TaskFilter`

**CTE handling:** If any `TermFilter` in the tree uses `RootID` (tree: filter), the recursive CTE is hoisted to the top of the query. Multiple tree filters across different branches get unique CTE aliases (`descendants_1`, `descendants_2`, etc.).

### 2.7 TaskRepository.List

`TaskRepo.List` switches from `TaskFilter` to `FilterExpr` as its parameter type. The interface in `repository/task.go` changes accordingly. Callers that construct `TaskFilter` directly (MCP) wrap it in `TermFilter{taskFilter}`.

### 2.8 CLI Integration

`runList` in `commands.go` switches from `Parse` → `ParseExpr`, then `ResolveExpr` → `List(FilterExpr)`.

`tusk add` and `tusk modify` continue using `Parse`/`FilterSet` for input extraction.

### 2.9 MCP Integration

Add an optional `filter` string parameter to `tusk_task_list`. When provided, it runs through `ParseExpr` → `ResolveExpr` → `List(FilterExpr)`, supporting the full boolean syntax. Existing structured parameters are wrapped in `TermFilter` for the same code path.

---

## Testing Strategy

### Phase 1

- **Lexer tests:** Quoted tokens (field values, standalone), escaped quotes, unclosed quotes, mixed quoted/unquoted.
- **Parser tests:** `title:` and `description:` fields, validation of empty values.
- **Resolver tests:** `TitleContains` and `DescriptionContains` mapping.
- **SQL tests:** `LIKE` condition generation, case-insensitive matching.
- **E2E tests:** `tusk list title:"some text"`, `tusk list description:"keyword"`.

### Phase 2

- **Lexer tests:** `AND`, `OR`, `NOT` keyword recognition, parentheses as delimiters, case sensitivity.
- **Parser tests:** Precedence — `a OR b AND c` = `a OR (b AND c)`. Implicit AND. Nested parens. `NOT` binding. Mismatched parens error. Empty parens error.
- **Resolver tests:** Tree walk producing correct `FilterExpr`. Default status injection with boolean expressions. Mixed boolean + DB-lookup fields (parent/tree inside OR branches).
- **SQL tests:** Nested `AND`/`OR`/`NOT` clause generation. CTE hoisting with multiple tree filters. Single term (degenerate case).
- **E2E tests:** `tusk list "status:active OR +urgent"`, `tusk list "NOT status:completed"`, `tusk list "(project:backend OR project:frontend) AND +api"`.
