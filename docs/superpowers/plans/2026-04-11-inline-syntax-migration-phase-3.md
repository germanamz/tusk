# Inline Syntax Migration — Phase 3: Migrate All Consumers and Remove Bridge

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate every occurrence of `key:value` filter/inline syntax to `key=value` across the entire codebase (tests, help text, MCP descriptions), then remove the dual-separator bridge so only `=` is accepted.

**Architecture:** This is a mechanical migration followed by bridge removal. Tasks 1-3 update all test inputs and documentation strings from `:` to `=` — the bridge ensures both work during the transition. Task 4 removes the bridge code (3 tagged locations from Phase 2). Task 5 validates the full system.

**Tech Stack:** Go standard library only.

**Prerequisites:** Phase 2 must be complete. The dual-separator bridge must be in place (both `=` and `:` accepted as field separators).

---

## Inherits From

**Phase 1** created the `syntax/` package (`syntax/errors.go`, `syntax/ast.go`, `syntax/token.go`) with the clean `=`-only lexer.

**Phase 2** rewired `filter/` to re-export types from `syntax/` and introduced a dual-separator bridge at 3 locations:
1. `filter/token.go` `isFieldToken()` — accepts both `=` and `:` (line with `BRIDGE` comment)
2. `filter/parser.go` — `strings.Cut(tok.Value, "=")` with `:` fallback (line with `BRIDGE` comment)
3. `filter/parse_expr.go` — same dual split (line with `BRIDGE` comment)

All existing tests pass with `:` syntax. New `=` syntax also works. The codebase is in a state where both separators are accepted.

---

## Task 1: Update all filter package unit tests from `:` to `=`

Every test file in `filter/` uses `key:value` syntax in test inputs and expected values. Replace all occurrences with `key=value`. The bridge ensures these updated tests pass immediately.

**Files:**
- Modify: `filter/token_test.go` (~25 occurrences)
- Modify: `filter/parser_test.go` (~15 occurrences)
- Modify: `filter/parse_expr_test.go` (~6 occurrences)
- Modify: `filter/resolve_test.go`
- Modify: `filter/resolve_expr_test.go`
- Modify: `filter/resolve_uda_test.go` (~4 occurrences)
- Modify: `filter/integration_test.go` (~4 occurrences)
- Modify: `filter/validators_test.go` (if it contains colon-syntax inputs)

- [ ] **Step 1: Update `filter/token_test.go`**

This file has the most occurrences. Apply these replacements across the entire file:

**In test `input` strings and expected `Value` strings:**
- `"status:active"` → `"status=active"` (and all similar: `"status:pending"`, `"status:deleted"`, `"status:"`)
- `"project:backend"` → `"project=backend"`
- `"priority:3"` → `"priority=3"`, `"priority:2..4"` → `"priority=2..4"`
- `"due:today..friday"` → `"due=today..friday"`, `"due:2026-04-10T15:30:00Z"` → `"due=2026-04-10T15:30:00Z"`
- `"title:fix the bug"` → `"title=fix the bug"`, and similar quoted variants
- `"title:say \"hello\""` → `"title=say \"hello\""`
- `"title:step 1: do things"` → `"title=step 1: do things"` (colon preserved in value portion)

**In test case names:**
- `"field key:value"` → `"field key=value"`
- `"colon at start is text not field"` → `"equals at start is text not field"` with input `"=value"`

**Handle the `"field with colon in value"` test case:**
The original test had input `due:2026-04-10T15:30:00Z` testing that colons in values are preserved. With `=` as the separator, colons in values are naturally preserved. Update to `due=2026-04-10T15:30:00Z` — the colon in the timestamp is just part of the value. You may keep this test to verify colons are inert, or remove it if redundant.

**Handle the `"tag-like token with colon is a field"` test case:**
The original had `+api:test` → field. With the bridge still active, this still works. But once the bridge is removed (Task 4), `+api:test` will be a tag, not a field. Update this test case:
- Change to test `+api=test` → field token (additive modifier field)
- Add a separate test: `+api:test` → tag include token (colon is inert)

But since the bridge is still active in this task, the second case would still be a field. **Mark this specific test case for update in Task 4** when the bridge is removed. For now, update the `+api:test` test to `+api=test`:

```go
{
    name:  "additive modifier field",
    input: "+api=test",
    want: []Token{
        {Type: TokenField, Value: "+api=test", Pos: 0},
    },
},
```

- [ ] **Step 2: Update `filter/parser_test.go`**

Replace all `key:value` patterns in test input strings with `key=value`. ~15 occurrences. Same mechanical replacement as above.

- [ ] **Step 3: Update `filter/parse_expr_test.go`**

Replace all `key:value` patterns. ~6 occurrences.

- [ ] **Step 4: Update `filter/resolve_test.go`, `filter/resolve_expr_test.go`, `filter/resolve_uda_test.go`, `filter/integration_test.go`**

Replace all `key:value` patterns in test inputs. For UDA tests: `uda.key:value` → `uda.key=value`.

- [ ] **Step 5: Run the full filter test suite**

Run: `go test -v ./filter/...`
Expected: ALL PASS (bridge accepts both; tests now use `=`)

- [ ] **Step 6: Commit**

```bash
git add filter/*_test.go
git commit -m "$(cat <<'EOF'
test(filter): update all test inputs from key:value to key=value

Mechanical replacement across all filter test files to match the new
= separator. Bridge ensures backward compat during transition.
EOF
)"
```

---

## Task 2: Update E2E tests — batch 1

The E2E tests in `tests/e2e/` pass CLI arguments with `key:value` syntax. Replace with `key=value`. Split into two batches by file count.

**Files:**
- Modify: `tests/e2e/filtering_test.go` (21 occurrences)
- Modify: `tests/e2e/hierarchy_test.go` (15 occurrences)
- Modify: `tests/e2e/propagation_test.go` (13 occurrences)
- Modify: `tests/e2e/task_lifecycle_test.go` (7 occurrences)

- [ ] **Step 1: Update `tests/e2e/filtering_test.go`**

Replace all filter-field patterns in `Args` string arrays. Examples:
- `"status:active"` → `"status=active"`
- `"status:pending,active"` → `"status=pending,active"`
- `"priority:3"` → `"priority=3"`
- `"priority:2..4"` → `"priority=2..4"`
- `"project:_default"` → `"project=_default"`
- `"parent:$0.short_id"` → `"parent=$0.short_id"`
- `"tree:$0.short_id"` → `"tree=$0.short_id"`
- `"claimed_by:agent-filter"` → `"claimed_by=agent-filter"`
- `"unclaimed:true"` → `"unclaimed=true"`

For quoted string fields: look for patterns like `title:"auth"` inside Go string literals. These appear as either:
- `"title:\"auth\""` → `"title=\"auth\""` (but note this changes to `title="auth"` which in a Go string is `"title=\"auth\""`)
- Or similar escaped patterns

The key replacement: the `:` immediately after the field name becomes `=`.

**Important:** Do NOT replace colons that are part of values (e.g., RFC3339 timestamps, time values). Only replace the separator colon between key and value.

- [ ] **Step 2: Update `tests/e2e/hierarchy_test.go`**

Replace `parent:` → `parent=`, `tree:` → `tree=`, `priority:` → `priority=`, `status:` → `status=`.

- [ ] **Step 3: Update `tests/e2e/propagation_test.go`**

Replace `parent:` → `parent=`, `status:` → `status=`, `priority:` → `priority=`.

- [ ] **Step 4: Update `tests/e2e/task_lifecycle_test.go`**

Replace `status:` → `status=`, `priority:` → `priority=`, `project:` → `project=`.

- [ ] **Step 5: Run these E2E tests**

Run: `go test -v ./tests/e2e -run "TestFiltering|TestHierarchy|TestPropagation|TestTaskLifecycle"`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add tests/e2e/filtering_test.go tests/e2e/hierarchy_test.go tests/e2e/propagation_test.go tests/e2e/task_lifecycle_test.go
git commit -m "$(cat <<'EOF'
test(e2e): migrate filter syntax to key=value — batch 1

Updates filtering, hierarchy, propagation, and task lifecycle E2E
tests from key:value to key=value separator.
EOF
)"
```

---

## Task 3: Update E2E tests batch 2 + CLI help text + MCP descriptions

**Files:**
- Modify: `tests/e2e/urgency_test.go` (7 occurrences)
- Modify: `tests/e2e/output_format_test.go` (7 occurrences)
- Modify: `tests/e2e/task_queue_test.go` (5 occurrences)
- Modify: `tests/e2e/player_test.go` (2 occurrences)
- Modify: `tests/e2e/error_handling_test.go` (2 occurrences)
- Modify: `internal/tui/commands.go:20,29` — `Use:` strings
- Modify: `internal/tui/commands_test.go:353` — comment
- Modify: `internal/mcp/server.go:230,521,535` — description strings

- [ ] **Step 1: Update remaining E2E test files**

`tests/e2e/urgency_test.go`: Replace `priority:` → `priority=`.
`tests/e2e/output_format_test.go`: Replace `priority:` → `priority=`.
`tests/e2e/task_queue_test.go`: Replace `priority:` → `priority=`.
`tests/e2e/player_test.go`: Replace `claimed_by:` → `claimed_by=`, `unclaimed:` → `unclaimed=`.
`tests/e2e/error_handling_test.go`: Replace `priority:` → `priority=`, `project:` → `project=`.

- [ ] **Step 2: Update CLI help text**

In `internal/tui/commands.go`:

Line 20 — change:
```go
Use:   "add [title] [key:value...] [+tag...]",
```
to:
```go
Use:   "add [title] [key=value...] [+tag...]",
```

Line 29 — change:
```go
Use:   "modify <short_id> [key:value...]",
```
to:
```go
Use:   "modify <short_id> [key=value...]",
```

- [ ] **Step 3: Update CLI test comment**

In `internal/tui/commands_test.go`, find the comment (around line 353):
```go
// Only key:value args, no title words
```
Change to:
```go
// Only key=value args, no title words
```

Also update any test input strings in this file that use `key:value` syntax.

- [ ] **Step 4: Update MCP tool description examples**

In `internal/mcp/server.go`:

Line 230 — change:
```go
mcp.Description("Filter expression with AND/OR/NOT/parentheses support (e.g. 'status:active OR +urgent'). When provided, other filter parameters are ignored."),
```
to:
```go
mcp.Description("Filter expression with AND/OR/NOT/parentheses support (e.g. 'status=active OR +urgent'). When provided, other filter parameters are ignored."),
```

Line 521 — change:
```go
mcp.Description("Boolean filter expression (e.g. 'project:backend AND +api')"),
```
to:
```go
mcp.Description("Boolean filter expression (e.g. 'project=backend AND +api')"),
```

Line 535 — change:
```go
mcp.Description("Optional boolean filter to narrow candidates (e.g. 'project:backend')"),
```
to:
```go
mcp.Description("Optional boolean filter to narrow candidates (e.g. 'project=backend')"),
```

- [ ] **Step 5: Run full test suite**

Run: `make test`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add tests/e2e/urgency_test.go tests/e2e/output_format_test.go tests/e2e/task_queue_test.go tests/e2e/player_test.go tests/e2e/error_handling_test.go internal/tui/commands.go internal/tui/commands_test.go internal/mcp/server.go
git commit -m "$(cat <<'EOF'
refactor: migrate remaining E2E tests, CLI help, and MCP descriptions to key=value

Completes the consumer-side migration from : to = separator across
E2E tests (batch 2), CLI command help text, and MCP tool descriptions.
EOF
)"
```

---

## Task 4: Remove dual-separator bridge

All consumers now use `=`. Remove the `:` fallback from all three bridge locations. Then replace `filter/token.go`'s own `Lex` with a delegation to `syntax.Lex`.

**Files:**
- Modify: `filter/token.go` — replace with thin delegation
- Modify: `filter/parser.go` — remove `:` fallback
- Modify: `filter/parse_expr.go` — remove `:` fallback
- Modify: `filter/token_test.go` — update any tests that relied on `:` bridge behavior

- [ ] **Step 1: Replace `filter/token.go` with delegation to `syntax.Lex`**

Replace the entire contents of `filter/token.go` with:

```go
package filter

import "github.com/germanamz/tusk/syntax"

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

// Lex delegates to the shared syntax lexer.
func Lex(input string) ([]Token, []ParseError) {
	return syntax.Lex(input)
}
```

This removes:
- The bridge `isFieldToken` function (dual `:` and `=` check)
- The local `Lex` function body (~80 lines)
- The local `scanQuoted` function (~15 lines)
- The `fmt` import (no longer needed)

- [ ] **Step 2: Remove `:` fallback from `filter/parser.go`**

Find the bridge code in `filter/parser.go` (in the `TokenField` case, around line 59):

```go
			// BRIDGE: try = first (new syntax), fall back to : (legacy).
			// Remove fallback in Phase 3.
			key, value, found := strings.Cut(tok.Value, "=")
			if !found {
				key, value, _ = strings.Cut(tok.Value, ":")
			}
```

Replace with:

```go
			key, value, _ := strings.Cut(tok.Value, "=")
```

- [ ] **Step 3: Remove `:` fallback from `filter/parse_expr.go`**

Find the bridge code in `filter/parse_expr.go` (in the `TokenField` case, around line 241):

```go
		// BRIDGE: try = first (new syntax), fall back to : (legacy).
		// Remove fallback in Phase 3.
		key, value, found := strings.Cut(tok.Value, "=")
		if !found {
			key, value, _ = strings.Cut(tok.Value, ":")
		}
```

Replace with:

```go
		key, value, _ := strings.Cut(tok.Value, "=")
```

- [ ] **Step 4: Update `filter/token_test.go` for post-bridge behavior**

After bridge removal, `:` is no longer recognized as a field separator. Check `filter/token_test.go` for any test cases that still rely on `:` being a field separator.

Key test case to update — if there's still a test like `"tag-like token with colon is a field"` with `+api:test` → field, change it:

```go
{
    name:  "tag with colon is a tag not a field",
    input: "+api:test",
    want: []Token{
        {Type: TokenTagInclude, Value: "+api:test", Pos: 0},
    },
},
```

Also, if there's a `"field with colon in value"` test using `due:...` as input, change it to `due=...` (should already be done in Task 1, but double-check).

Verify there are no test cases with input containing `:` as a field separator. Search for the pattern `[a-z_]+:` in test input strings.

- [ ] **Step 5: Run full filter test suite**

Run: `go test -v ./filter/...`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add filter/token.go filter/parser.go filter/parse_expr.go filter/token_test.go
git commit -m "$(cat <<'EOF'
refactor(filter): remove dual-separator bridge, delegate Lex to syntax

The filter package now fully delegates tokenization to syntax.Lex
which uses = as the only field separator. The : fallback in parser
and parse_expr is removed. Colons are now inert in the lexer and
available for use as value modifiers (e.g., transition sequences).
EOF
)"
```

---

## Task 5: Full validation

**Files:** None modified — verification only.

- [ ] **Step 1: Run unit tests with race detector**

Run: `make test-race`
Expected: ALL PASS

- [ ] **Step 2: Run E2E tests**

Run: `make test-e2e`
Expected: ALL PASS

- [ ] **Step 3: Run linter and vet**

Run: `make lint && make vet`
Expected: clean

- [ ] **Step 4: Search for stale `:` syntax in Go source files**

Run a grep across all Go files for filter-field patterns still using `:`:

```bash
grep -rn '"status:' --include='*.go' . | grep -v '_test.go' | grep -v 'docs/' | grep -v 'ROADMAP' | grep -v 'PRODUCT'
grep -rn '"priority:' --include='*.go' . | grep -v '_test.go' | grep -v 'docs/'
grep -rn '"project:' --include='*.go' . | grep -v '_test.go' | grep -v 'docs/'
grep -rn '"parent:' --include='*.go' . | grep -v '_test.go' | grep -v 'docs/'
grep -rn 'key:value' --include='*.go' . | grep -v 'docs/'
```

Any matches in non-doc, non-test files are stale references that need updating. Fix them.

Also check test files to make sure no `:` separator patterns remain:

```bash
grep -rn '"status:' --include='*_test.go' .
grep -rn '"priority:' --include='*_test.go' .
grep -rn '"project:' --include='*_test.go' .
```

Expected: no matches (all migrated to `=`)

- [ ] **Step 5: Build and smoke test**

Run: `make build`

```bash
./bin/tusk add "Final validation task" priority=3 project=_default +smoke-test
./bin/tusk list status=pending +smoke-test
./bin/tusk list priority=2..4
./bin/tusk list status=pending,active
```

Verify old `:` syntax is rejected (should be parsed as text, not a field):
```bash
./bin/tusk list status:active
```
Expected: this should NOT filter by status — `status:active` is now a text token. The list should show tasks with default status filter (pending, active) — the `status:active` term is free text and should cause an error in the expression parser ("free text not supported in filter expressions") or be silently ignored depending on context.

- [ ] **Step 6: Commit any remaining fixes**

```bash
git add -A
git commit -m "$(cat <<'EOF'
fix: address stale key:value references found during final validation
EOF
)"
```

Skip this step if no fixes were needed.

---

## Changes Introduced

| Category | Details |
|----------|---------|
| **New files** | None |
| **Modified files** | `filter/token.go`, `filter/parser.go`, `filter/parse_expr.go`, `filter/token_test.go`, `filter/parser_test.go`, `filter/parse_expr_test.go`, `filter/resolve_test.go`, `filter/resolve_expr_test.go`, `filter/resolve_uda_test.go`, `filter/integration_test.go`, `internal/tui/commands.go`, `internal/tui/commands_test.go`, `internal/mcp/server.go`, `tests/e2e/filtering_test.go`, `tests/e2e/hierarchy_test.go`, `tests/e2e/propagation_test.go`, `tests/e2e/task_lifecycle_test.go`, `tests/e2e/urgency_test.go`, `tests/e2e/output_format_test.go`, `tests/e2e/task_queue_test.go`, `tests/e2e/player_test.go`, `tests/e2e/error_handling_test.go` |
| **Removed bridge code** | 3 locations from Phase 2 removed: `filter/token.go` isFieldToken `:` check, `filter/parser.go` `:` fallback, `filter/parse_expr.go` `:` fallback |
| **New dependencies** | None |
| **Schema migrations** | None |

**User-visible behavior changes:**
- **BREAKING:** `key:value` filter syntax no longer works. All inline syntax now uses `key=value` (e.g., `status=active`, `priority=3`, `project=backend`)
- `:` is now available for use in field values (e.g., `transition=pending:active` for workflow transitions in future Workflow Management CLI initiative)
- `+`/`-` with `=` is now the additive/subtractive modifier syntax (e.g., `+status=review`, `-status=review`)
- **NEW:** `()` immediately after a value (no whitespace) is now a group modifier, not a boolean grouping operator. `status=pending(initial)` is a single field token with value `pending(initial)`. `(status=active)` with space before `(` remains boolean grouping. This enables future workflow/project inline syntax — no existing commands or tests are affected since no current usage places `(` immediately after a value.
- All other behavior is identical — same fields, same operators, same tags, same quoted strings
