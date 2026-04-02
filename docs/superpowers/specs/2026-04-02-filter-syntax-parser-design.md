# Filter Syntax Parser — Design Spec

**Date:** 2026-04-02
**Status:** Approved
**Roadmap item:** v0.1 — Filter syntax parser

---

## Overview

Replace the hand-rolled `parseArgs`/`buildTaskFilter` functions in `internal/tui/filter.go` with a formal filter parser in a new `internal/filter` package. The parser produces an AST that is converted to `domain.TaskFilter` via a separate resolution step. This design supports the current AND-only filter semantics while structuring the AST for future boolean operator extension.

### Goals

- Formal grammar with clear token and AST types
- Multi-error collection (report all syntax issues in one pass)
- Zero external dependencies
- Shared by TUI and MCP layers
- Extensible to boolean operators (`AND`/`OR`/`NOT`, parentheses) without a rewrite

### Non-goals

- Boolean operators (future milestone)
- Quoted string support (future milestone)
- Changes to `domain.TaskFilter` or the SQLite query builder

---

## Package Structure

```
internal/filter/
├── token.go         # Token types and Lexer
├── token_test.go    # Lexer tests
├── ast.go           # AST node types
├── parser.go        # Table-driven parser: tokens → AST
├── parser_test.go   # Parser and validator tests
├── resolve.go       # Resolver: AST → domain.TaskFilter (requires repos)
├── resolve_test.go  # Resolver tests with mock repos
├── errors.go        # ParseError type with multi-error collection
└── integration_test.go  # End-to-end string → domain.TaskFilter tests
```

---

## Public API

```go
package filter

// Parse takes a raw filter string and returns the AST plus any parse errors.
// Always returns a FilterSet (possibly empty) even when errors are present,
// so callers can use partial results if desired.
func Parse(input string) (*FilterSet, []ParseError)

// Resolver converts a parsed FilterSet into a domain.TaskFilter.
type Resolver struct { ... }

func NewResolver(projectRepo repository.ProjectRepository, taskRepo repository.TaskRepository) *Resolver

// Resolve converts the AST into a domain.TaskFilter. Resolution errors
// (e.g., project not found) are collected rather than failing fast.
func (r *Resolver) Resolve(ctx context.Context, fs *FilterSet) (*domain.TaskFilter, []error)
```

Consumers call `filter.Parse(rawString)` then `resolver.Resolve(ctx, ast)`.

---

## Lexer

The lexer scans the input string left-to-right and emits a flat list of tokens. It has no awareness of nesting or field semantics.

### Token types

| Token              | Pattern            | Example                          |
| ------------------ | ------------------ | -------------------------------- |
| `TokenField`       | `key:value`        | `status:active`, `priority:2..4` |
| `TokenTagInclude`  | `+word`            | `+api`, `+frontend`              |
| `TokenTagExclude`  | `-word`            | `-docs`, `-wip`                  |
| `TokenText`        | anything else      | `My`, `task`, `title`            |

### Rules

- Whitespace splits tokens.
- First `:` in a token splits field name from value. Colons in values are preserved (e.g., `due:2026-04-10T15:00:00Z`).
- `+` and `-` prefixes only count as tag markers when followed by a word character. A bare `+` or `-` is a lexer error.
- The lexer does not validate field names or values.
- Each token records its byte offset (`Pos`) in the original input.

---

## AST Nodes

```go
// FilterSet is the root node — a collection of filter terms, implicitly AND'd.
// Designed to later wrap a BoolExpr node for OR/NOT/grouping.
type FilterSet struct {
    Fields []FieldFilter
    Tags   []TagFilter
    Text   []string
}

// FieldFilter represents a key:value or key:min..max term.
type FieldFilter struct {
    Key   string // "status", "project", "priority", "due", "parent", "tree", "waiting"
    Value string // raw value string, unparsed
    Pos   int    // byte offset in input
}

// TagFilter represents +tag or -tag.
type TagFilter struct {
    Name    string
    Exclude bool // true for -tag, false for +tag
    Pos     int
}
```

`FieldFilter.Value` is kept as a raw string. The parser validates that the field name is known and the value shape is plausible, but typed parsing (dates, UUIDs, etc.) happens during resolution where repos are available.

---

## Table-Driven Parser

The parser iterates over the lexer's token list and builds the AST. Its core is a validation table keyed by field name.

### Field validation table

```go
var fieldValidators = map[string]func(value string) error{
    "status":   validateStatus,   // non-empty; comma-separated words
    "project":  validateProject,  // non-empty string
    "priority": validatePriority, // single (0-4 or name), or range (min..max)
    "due":      validateDue,      // date literal, relative keyword, or range
    "parent":   validateShortID,  // hex string
    "tree":     validateShortID,  // hex string
    "waiting":  validateBool,     // "true" or "false"
}
```

### Parse loop

1. Iterate over tokens.
2. `TokenTagInclude` / `TokenTagExclude` → append to `FilterSet.Tags`.
3. `TokenText` → append to `FilterSet.Text`.
4. `TokenField` → look up key in the validation table:
   - Key not found → collect `ParseError` ("unknown field").
   - Validator returns error → collect `ParseError` with details.
   - Valid → append to `FilterSet.Fields`.
5. Return `FilterSet` + accumulated `[]ParseError`.

The parser never short-circuits. Each token is processed independently, so all errors are reported in one pass.

### Supported fields and value formats

| Field      | Accepted values                                                             |
| ---------- | --------------------------------------------------------------------------- |
| `status`   | Single or comma-separated: `active`, `pending,active`                       |
| `project`  | Name string: `backend`                                                      |
| `priority` | Single (`3`), named (`high`), or range (`2..4`, `low..high`)                |
| `due`      | Absolute (`2026-04-10`), relative (`today`, `tomorrow`, `thisweek`), weekday (`monday`), or range (`today..friday`) |
| `parent`   | Short ID hex string: `a3f8b2c1`                                            |
| `tree`     | Short ID hex string: `a3f8b2c1`                                            |
| `waiting`  | `true` or `false`                                                           |

---

## Error Handling

```go
// ParseError represents a single issue found during parsing.
type ParseError struct {
    Pos     int    // byte offset in input (-1 if not applicable)
    Field   string // field name, if relevant
    Message string // human-readable description
}

func (e ParseError) Error() string
```

Errors are collected into a `[]ParseError` slice. Example output for `tusk list foo:bar priority:xyz`:

```
unknown field "foo" at position 10
invalid priority "xyz" at position 18: expected 0-4 or none/low/medium/high/urgent
```

---

## Resolution Layer

The `Resolver` converts a validated `FilterSet` into a `domain.TaskFilter` by performing typed parsing and repository lookups.

```go
type Resolver struct {
    projectRepo repository.ProjectRepository
    taskRepo    repository.TaskRepository
}
```

### Resolution per field

| Field      | Resolution                                                        |
| ---------- | ----------------------------------------------------------------- |
| `status`   | Split comma-separated values → `filter.Statuses`                 |
| `project`  | `projectRepo.GetByName()` → `filter.ProjectID`                  |
| `priority` | Parse single/range/named → `filter.PriorityMin` / `PriorityMax` |
| `due`      | Parse dates/relative/range → `filter.DueAfter` / `DueBefore`    |
| `parent`   | `taskRepo.GetByShortID()` → `filter.ParentID`                   |
| `tree`     | `taskRepo.GetByShortID()` → `filter.RootID`                     |
| `waiting`  | Parse bool → `filter.WaitingOnly`                                |
| Tags       | Split include/exclude → `filter.Tags` / `filter.ExcludeTags`    |

### Defaults

- When no `status` field is present, defaults to `["pending", "active"]`.

### Error handling

Resolution errors (project not found, unknown short ID) are collected into `[]error` rather than failing fast, consistent with the parser's multi-error approach.

---

## Integration & Migration

### TUI (`internal/tui/commands.go`)

The `runList` handler changes from:

```
args → parseArgs() → buildTaskFilter() → taskSvc.List()
```

To:

```
strings.Join(args, " ") → filter.Parse() → resolver.Resolve() → taskSvc.List()
```

The `runAdd` command also uses `filter.Parse()` — `FilterSet.Text` provides the title, `FilterSet.Fields` provides priority/project/due/parent, and `FilterSet.Tags` provides tags.

### MCP (`internal/mcp/`)

MCP tools like `tusk_task_list` can accept a `filter` string parameter and run it through the same `filter.Parse()` + `resolver.Resolve()` pipeline. This is a future integration — the package just needs to be available.

### Code removed

- `parseArgs()` in `internal/tui/filter.go`
- `buildTaskFilter()` in `internal/tui/filter.go`
- `ParsedArgs` struct
- Associated tests (rewritten against the new `filter` package)

### Code moved

- `parsePriority()` and `parseDate()` move into `internal/filter/` as internal helpers used by validators and the resolver.

### Code unchanged

- `domain.TaskFilter` struct
- SQLite `buildFilter()` (consumes `domain.TaskFilter` as before)
- Service layer (`TaskService.List` is a pass-through)

---

## Test Strategy

### Lexer tests (`token_test.go`)

- Token output for various inputs
- Correct `Pos` tracking
- Edge cases: bare `+`/`-`, colons in values, empty input

### Parser tests (`parser_test.go`)

- AST shape for valid inputs
- Multi-error collection for invalid inputs (unknown fields, bad values)
- All field validators exercised

### Resolver tests (`resolve_test.go`)

- Mock repos: verify `domain.TaskFilter` output for each field type
- Default status behavior
- Error collection (project not found, bad short ID)

### Integration tests

- End-to-end from string → `domain.TaskFilter`
- Verify all existing `filter_test.go` scenarios still pass under the new parser

---

## Future Roadmap Items

The following are explicitly out of scope for this implementation but the design accommodates them:

- **Boolean operators** (`AND`/`OR`/`NOT`, parentheses) — upgrade the parser from table-driven to recursive descent; wrap `FilterSet` in a `BoolExpr` AST node. The lexer, token types, and resolver stay the same.
- **Quoted strings** — add a `TokenQuotedText` token type to the lexer for values/titles containing spaces or special characters (e.g., `"my task title"`, `project:"my project"`).
