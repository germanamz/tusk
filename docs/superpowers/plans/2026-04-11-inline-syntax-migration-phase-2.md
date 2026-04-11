# Inline Syntax Migration — Phase 2: Rewire `filter/` with Dual-Separator Bridge

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewire the `filter/` package to use shared types from `syntax/`, and introduce a dual-separator bridge so both `=` (new) and `:` (legacy) are accepted as field separators. All existing tests pass unchanged.

**Architecture:** The `filter/` package replaces its locally defined types (Token, TokenType, ParseError, FilterSet, FieldFilter, TagFilter) with re-exports from `syntax/`. The `filter.Lex` function keeps its own implementation (not delegating to `syntax.Lex` yet) but updates `isFieldToken` to accept both `=` and `:`. The parsers (`parser.go`, `parse_expr.go`) split field tokens on `=` first, falling back to `:`. This bridge ensures 100% backward compatibility — all existing tests, CLI commands, E2E tests, and MCP tools continue working with `:` syntax while also accepting `=`.

**Tech Stack:** Go standard library only.

**Prerequisites:** Phase 1 must be complete. The `syntax/` package (`syntax/errors.go`, `syntax/ast.go`, `syntax/token.go`) must exist.

---

## Inherits From

**Phase 1** created the `syntax/` package with three files:
- `syntax/errors.go` — `ParseError` struct and `FormatErrors` function
- `syntax/ast.go` — `FilterSet`, `FieldFilter`, `TagFilter` types with helper methods
- `syntax/token.go` — `Token`, `TokenType`, `Lex()` function using `=` separator

These types have identical signatures to the ones currently defined in `filter/`. The re-export strategy uses Go type aliases (`type X = syntax.X`) so all existing consumer code compiles unchanged.

---

## Task 1: Rewire `filter/errors.go` and `filter/ast.go` to syntax re-exports

**Files:**
- Modify: `filter/errors.go`
- Modify: `filter/ast.go`

- [ ] **Step 1: Read the current files**

Read `filter/errors.go` and `filter/ast.go` to confirm their current contents match expectations.

`filter/errors.go` currently defines `ParseError` struct (with `Pos`, `Field`, `Message` fields), its `Error()` method, and `FormatErrors()`. These are identical to `syntax/errors.go`.

`filter/ast.go` currently defines `FilterSet`, `FieldFilter`, `TagFilter` types and helper methods (`HasField`, `GetField`, `IncludeTags`, `ExcludeTags`, `Title`). These are identical to `syntax/ast.go`.

- [ ] **Step 2: Replace `filter/errors.go` with re-exports**

Replace the entire contents of `filter/errors.go` with:

```go
package filter

import "github.com/germanamz/tusk/syntax"

// ParseError is a re-export of the shared syntax.ParseError type.
type ParseError = syntax.ParseError

// FormatErrors is a re-export of the shared syntax.FormatErrors function.
var FormatErrors = syntax.FormatErrors
```

- [ ] **Step 3: Replace `filter/ast.go` with re-exports**

Replace the entire contents of `filter/ast.go` with:

```go
package filter

import "github.com/germanamz/tusk/syntax"

// Re-export shared AST types so existing consumers compile unchanged.
type FilterSet = syntax.FilterSet
type FieldFilter = syntax.FieldFilter
type TagFilter = syntax.TagFilter
```

- [ ] **Step 4: Verify compilation**

Run: `go vet ./filter/...`
Expected: clean (no errors)

- [ ] **Step 5: Commit**

```bash
git add filter/errors.go filter/ast.go
git commit -m "$(cat <<'EOF'
refactor(filter): re-export ParseError, FilterSet, FieldFilter, TagFilter from syntax

Type aliases ensure all existing consumer code compiles unchanged.
The filter package no longer owns these type definitions.
EOF
)"
```

---

## Task 2: Update `filter/token.go` with dual-separator bridge

The key change: `isFieldToken` accepts both `=` (new, checked first) and `:` (legacy, fallback). The `Lex` function body and `scanQuoted` function stay unchanged — they already work correctly because they operate on the raw token string and delegate classification to `isFieldToken`.

**Files:**
- Modify: `filter/token.go`

- [ ] **Step 1: Read `filter/token.go`**

Confirm it currently defines:
- `TokenType` (int enum) and `String()` method (lines 6-43)
- `Token` struct (lines 46-50)
- `Lex()` function (lines 55-160)
- `scanQuoted()` function (lines 166-182)
- `isFieldToken()` function (lines 186-193)

- [ ] **Step 2: Remove local type definitions and add re-exports from syntax**

Replace lines 1-50 of `filter/token.go` (everything from the package declaration through the `Token` struct) with:

```go
package filter

import (
	"fmt"

	"github.com/germanamz/tusk/syntax"
)

// Re-export token types from syntax package.
type TokenType = syntax.TokenType
type Token = syntax.Token

const (
	TokenField      = syntax.TokenField
	TokenTagInclude = syntax.TokenTagInclude
	TokenTagExclude = syntax.TokenTagExclude
	TokenText       = syntax.TokenText
	TokenAnd        = syntax.TokenAnd
	TokenOr         = syntax.TokenOr
	TokenNot        = syntax.TokenNot
	TokenLParen     = syntax.TokenLParen
	TokenRParen     = syntax.TokenRParen
)
```

Keep the `Lex()` function body, `scanQuoted()`, and `isFieldToken()` as-is for now.

- [ ] **Step 3: Replace `isFieldToken` with dual-separator version**

Replace the current `isFieldToken` function (which only checks for `:`) with:

```go
// isFieldToken returns true if the raw token contains a field separator
// with a non-empty key.
// BRIDGE: accepts both = (new) and : (legacy). Remove : check in Phase 3.
func isFieldToken(raw string) bool {
	// Check = first (new syntax)
	for i := 0; i < len(raw); i++ {
		if raw[i] == '=' {
			return i > 0
		}
	}
	// BRIDGE: also accept : (legacy syntax)
	for i := 0; i < len(raw); i++ {
		if raw[i] == ':' {
			return i > 0
		}
	}
	return false
}
```

- [ ] **Step 4: Run filter tests to verify backward compatibility**

Run: `go test -v ./filter/...`
Expected: ALL PASS — every existing test uses `:` syntax which the bridge accepts

- [ ] **Step 5: Verify new `=` syntax also works**

Quick manual verification — add a temporary test or use `go test -run` with a modified input. The dual `isFieldToken` should accept `status=active` as a field token. This will be thoroughly tested after the parser is updated in Task 3.

- [ ] **Step 6: Commit**

```bash
git add filter/token.go
git commit -m "$(cat <<'EOF'
refactor(filter): re-export token types from syntax, add dual-separator bridge

isFieldToken now accepts both = (new) and : (legacy) as field
separators. The Lex function body is unchanged — it uses the
re-exported types transparently. Bridge will be removed in Phase 3.
EOF
)"
```

---

## Task 3: Update parsers with dual field-split logic

Both `parser.go` and `parse_expr.go` split field token values on `:`. Update them to try `=` first, falling back to `:`.

**Files:**
- Modify: `filter/parser.go:59`
- Modify: `filter/parse_expr.go:241`

- [ ] **Step 1: Update `filter/parser.go` field split**

In `filter/parser.go`, line 59, change:

```go
			key, value, _ := strings.Cut(tok.Value, ":")
```

to:

```go
			// BRIDGE: try = first (new syntax), fall back to : (legacy).
			// Remove fallback in Phase 3.
			key, value, found := strings.Cut(tok.Value, "=")
			if !found {
				key, value, _ = strings.Cut(tok.Value, ":")
			}
```

- [ ] **Step 2: Update `filter/parse_expr.go` field split**

In `filter/parse_expr.go`, line 241, change:

```go
		key, value, _ := strings.Cut(tok.Value, ":")
```

to:

```go
		// BRIDGE: try = first (new syntax), fall back to : (legacy).
		// Remove fallback in Phase 3.
		key, value, found := strings.Cut(tok.Value, "=")
		if !found {
			key, value, _ = strings.Cut(tok.Value, ":")
		}
```

- [ ] **Step 3: Run the full filter test suite**

Run: `go test -v ./filter/...`
Expected: ALL PASS — legacy `:` syntax still works via bridge

- [ ] **Step 4: Run the full project test suite**

Run: `make test`
Expected: ALL PASS (includes E2E tests that use `:` syntax)

- [ ] **Step 5: Commit**

```bash
git add filter/parser.go filter/parse_expr.go
git commit -m "$(cat <<'EOF'
refactor(filter): dual field split — try = first, fall back to :

Both parser.go and parse_expr.go now split field tokens on = first,
with a : fallback for backward compatibility. Bridge code tagged for
removal in Phase 3.
EOF
)"
```

---

## Task 4: Verify complete backward compatibility

Run the full test suite including E2E tests to confirm the bridge preserves all existing behavior.

**Files:** None modified — verification only.

- [ ] **Step 1: Run unit tests with race detector**

Run: `make test-race`
Expected: ALL PASS

- [ ] **Step 2: Run E2E tests**

Run: `make test-e2e`
Expected: ALL PASS

- [ ] **Step 3: Run linter**

Run: `make lint`
Expected: clean

- [ ] **Step 4: Run vet**

Run: `make vet`
Expected: clean

- [ ] **Step 5: Build binary and smoke test with both syntaxes**

Run: `make build`

Test legacy syntax still works:
```bash
./bin/tusk add "Bridge test task" priority:3 +bridge-test
./bin/tusk list status:pending +bridge-test
```

Test new syntax also works:
```bash
./bin/tusk add "Bridge test task 2" priority=3 +bridge-test2
./bin/tusk list status=pending +bridge-test2
```

Clean up:
```bash
./bin/tusk delete $(./bin/tusk list +bridge-test --output json | jq -r '.[0].short_id') 2>/dev/null || true
./bin/tusk delete $(./bin/tusk list +bridge-test2 --output json | jq -r '.[0].short_id') 2>/dev/null || true
```

Both should produce identical results.

- [ ] **Step 6: Commit (only if fixes were needed)**

If any issues were found and fixed in earlier steps, commit the fixes:

```bash
git add -A
git commit -m "$(cat <<'EOF'
fix(filter): address issues found during bridge backward-compat verification
EOF
)"
```

---

## Changes Introduced

| Category | Details |
|----------|---------|
| **New files** | None |
| **Modified files** | `filter/errors.go`, `filter/ast.go`, `filter/token.go`, `filter/parser.go`, `filter/parse_expr.go` |
| **New dependencies** | `filter/` now imports `github.com/germanamz/tusk/syntax` |
| **New interfaces** | None |
| **Schema migrations** | None |
| **Bridge code** | 3 locations, all tagged for removal in **Phase 3**: |
| | 1. `filter/token.go` `isFieldToken()` — `:` check after `=` check |
| | 2. `filter/parser.go:59` — `strings.Cut(tok.Value, ":")` fallback |
| | 3. `filter/parse_expr.go:241` — `strings.Cut(tok.Value, ":")` fallback |

**User-visible behaviors preserved (acceptance criteria):**
- `tusk add "Task" priority:3 project:_default +api` — works
- `tusk list status:active +api` — works
- `tusk list status:pending,active` — works
- `tusk list priority:2..4` — works
- `tusk modify <id> priority:4 due:tomorrow` — works
- `tusk list (status:active OR +urgent)` — works
- `tusk list title:"some text"` — works
- `tusk available project:backend` — works
- `tusk pop priority:3` — works
- All E2E tests pass unchanged
- **NEW:** all of the above also work with `=` instead of `:`
