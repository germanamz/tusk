# Package Restructure — Phase 2: Interface Packages (repository, filter)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the two tier-1 packages (`repository`, `filter`) from `internal/` to top-level, making them importable by external Go programs.

**Architecture:** Pure mechanical refactor — `git mv` directories, rewrite import paths with `sed`, verify compilation and tests. No behavioral changes.

**Tech Stack:** Go (module `github.com/germanamz/tusk`), `git mv`, `sed`

**Prerequisites:** Phase 1 must be completed first. Phase 2 depends on `domain` and `config` already being at top-level.

---

## Inherits From

**Phase 1** moved these packages to top-level:
- `internal/domain/` → `domain/` — core types and sentinel errors
- `internal/config/` → `config/` — Viper-based config loading

The implementer should expect:
- `domain/` and `config/` exist as top-level directories
- All imports across the codebase reference `github.com/germanamz/tusk/domain` and `github.com/germanamz/tusk/config` (not `internal/domain` or `internal/config`)
- `internal/` still contains: `repository/`, `filter/`, `service/`, `sqlite/`, `inmem/`, `tui/`, `mcp/`
- All tests pass, binary builds cleanly

---

## Context

This phase moves the two tier-1 packages:

- `internal/repository/` (8 .go files) — interface definitions for all storage backends. Imports only `domain` (already top-level).
- `internal/filter/` (19 .go files) — 3-stage filter parser (Lexer → Parser → Resolver). Imports `domain` (already top-level) in production code. Test files also import `internal/sqlite` for test harness setup — **no action needed**, `internal/sqlite` still exists after this phase and those imports compile and work correctly. The implementer does not need to touch or worry about filter's sqlite test imports.

## Import Rewrite Technique

Same as Phase 1:

```bash
git mv internal/X X
find . -name '*.go' -exec sed -i '' 's|github.com/germanamz/tusk/internal/X|github.com/germanamz/tusk/X|g' {} +
```

## Packages that import repository (will have their imports rewritten)

- `internal/service/` — `task.go`, `player.go`, `project.go`, `relation.go`, `tag.go`, `workflow.go`, plus test files
- `internal/sqlite/` — `store.go`, plus test files
- `internal/inmem/` — `project.go`, `workflow.go`, plus test files

## Packages that import filter (will have their imports rewritten)

- `internal/tui/` — `app.go`, `commands.go`
- `internal/mcp/` — `tools.go`

---

### Task 1: Move repository package

**Files:**
- Move: `internal/repository/` → `repository/` (8 files: `annotation.go`, `player.go`, `project.go`, `relation.go`, `repository_test.go`, `tag.go`, `task.go`, `workflow.go`)
- Modify: all `.go` files across the repo that import `github.com/germanamz/tusk/internal/repository`

- [ ] **Step 1: Move the directory**

```bash
git mv internal/repository repository
```

- [ ] **Step 2: Rewrite all import paths**

```bash
find . -name '*.go' -exec sed -i '' 's|github.com/germanamz/tusk/internal/repository|github.com/germanamz/tusk/repository|g' {} +
```

- [ ] **Step 3: Verify build compiles**

```bash
go build ./...
```

Expected: clean build, zero errors.

- [ ] **Step 4: Run tests**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
refactor: move repository package to top-level

Move internal/repository → repository to make storage interfaces
importable by external Go programs. Update all import paths.

Part of v0.8 Package Restructure — Phase 2.
EOF
)"
```

---

### Task 2: Move filter package

**Files:**
- Move: `internal/filter/` → `filter/` (19 files: `ast.go`, `dates.go`, `dates_test.go`, `errors.go`, `expr.go`, `expr_test.go`, `integration_test.go`, `parse_expr.go`, `parse_expr_test.go`, `parser.go`, `parser_test.go`, `resolve.go`, `resolve_expr_test.go`, `resolve_test.go`, `resolve_uda_test.go`, `token.go`, `token_test.go`, `validators.go`, `validators_test.go`)
- Modify: all `.go` files across the repo that import `github.com/germanamz/tusk/internal/filter`

- [ ] **Step 1: Move the directory**

```bash
git mv internal/filter filter
```

- [ ] **Step 2: Rewrite all import paths**

```bash
find . -name '*.go' -exec sed -i '' 's|github.com/germanamz/tusk/internal/filter|github.com/germanamz/tusk/filter|g' {} +
```

- [ ] **Step 3: Verify build compiles**

```bash
go build ./...
```

Expected: clean build, zero errors.

- [ ] **Step 4: Run tests**

```bash
go test ./...
```

Expected: all tests pass. Note: `filter/integration_test.go` and `filter/resolve_test.go` import `internal/sqlite` which still exists at that path — this compiles and works correctly.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
refactor: move filter package to top-level

Move internal/filter → filter to make the filter parser importable
by external Go programs. Update all import paths.

Part of v0.8 Package Restructure — Phase 2.
EOF
)"
```

---

### Task 3: Verify no stale internal references for moved packages

**Files:**
- None modified — verification only

- [ ] **Step 1: Grep for stale repository imports**

```bash
grep -r 'github.com/germanamz/tusk/internal/repository' --include='*.go' .
```

Expected: zero matches.

- [ ] **Step 2: Grep for stale filter imports**

```bash
grep -r 'github.com/germanamz/tusk/internal/filter' --include='*.go' .
```

Expected: zero matches.

- [ ] **Step 3: Verify packages resolve from new locations**

```bash
go list github.com/germanamz/tusk/repository github.com/germanamz/tusk/filter
```

Expected output:
```
github.com/germanamz/tusk/repository
github.com/germanamz/tusk/filter
```

- [ ] **Step 4: Confirm remaining internal/ packages**

```bash
ls internal/
```

Expected output:
```
inmem   mcp     service sqlite  tui
```

Five packages remain in `internal/`. Phase 3 will move `service`, `sqlite`, and `inmem`.

---

### Task 4: Full test suite with race detector

**Files:**
- None modified — verification only

- [ ] **Step 1: Run tests with race detector**

```bash
go test -race ./...
```

Expected: all tests pass with zero race conditions.

- [ ] **Step 2: Build the binary and smoke test**

```bash
make build && bin/tusk version
```

Expected: version output prints successfully.

---

## User-Visible Behavior Preserved

- All CLI commands work identically
- MCP server starts and responds identically
- Filter syntax unchanged (`status:pending`, `+tag`, `priority:2..4`, etc.)
- All repository interface contracts unchanged
- `make build`, `make test`, `make test-race`, `make lint` all pass

## Changes Introduced

**Moved directories:**
- `internal/repository/` → `repository/` (8 files)
- `internal/filter/` → `filter/` (19 files)

**Modified files (import path rewrites only):**
- `internal/service/*.go` — `repository` import updated
- `internal/sqlite/*.go` — `repository` import updated
- `internal/inmem/*.go` — `repository` import updated
- `internal/tui/*.go` — `filter` import updated
- `internal/mcp/*.go` — `filter` import updated

**New dependencies:** None

**Schema migrations:** None

**Bridge code:** None

**Environment variables:** None
