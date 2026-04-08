# Package Restructure — Phase 1: Foundational Packages (domain, config)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the two zero-dependency packages (`domain`, `config`) from `internal/` to top-level, making them importable by external Go programs.

**Architecture:** Pure mechanical refactor — `git mv` directories, rewrite import paths with `sed`, verify compilation and tests. No behavioral changes.

**Tech Stack:** Go (module `github.com/germanamz/tusk`), `git mv`, `sed`

**Prerequisites:** None. This phase operates on the base codebase.

---

## Context

The Go module is `github.com/germanamz/tusk`. All core packages currently live under `internal/`, making them inaccessible to external importers. This phase moves the two foundational packages that have **zero internal dependencies**:

- `internal/domain/` (13 .go files) — core types, sentinel errors, no internal imports
- `internal/config/` (2 .go files) — Viper-based config loading, no internal imports

After this phase, both packages are importable as `github.com/germanamz/tusk/domain` and `github.com/germanamz/tusk/config`. All other packages remain in `internal/` and continue to compile because their import paths for domain/config get rewritten to the new locations.

## Import Rewrite Technique

Every task uses the same mechanical pattern:

```bash
# Move directory
git mv internal/X X

# Rewrite all import paths across the entire repo
find . -name '*.go' -exec sed -i '' 's|github.com/germanamz/tusk/internal/X|github.com/germanamz/tusk/X|g' {} +
```

This is safe because Go import paths are plain strings — no partial matches are possible (e.g., `internal/domain` won't accidentally match `internal/domain_utils` because no such package exists).

## Packages that import domain (will have their imports rewritten)

- `internal/repository/` — all 8 files
- `internal/filter/` — `parse_expr.go`, `parser.go`, `resolve.go`, plus test files
- `internal/service/` — all 15 files
- `internal/sqlite/` — all 15 files
- `internal/inmem/` — `project.go`, `workflow.go`, plus test files
- `internal/tui/` — `render.go`, `commands.go`, `tree.go`, `tag.go`, `uda.go`, `styles.go`, plus test files
- `internal/mcp/` — `errors.go`, `errors_test.go`

## Packages that import config (will have their imports rewritten)

- `internal/inmem/` — `project.go`, `workflow.go`, plus test files
- `internal/service/` — test files only (`workflow_test.go`, `project_test.go`, `tag_test.go`, `relation_test.go`, `task_test.go`, `task_claim_test.go`)
- `internal/tui/` — `app.go`, `styles_test.go`, `app_test.go`, `commands_test.go`
- `internal/mcp/` — `server.go`, `server_test.go`
- `cmd/tusk/main.go`

---

### Task 1: Move domain package

**Files:**
- Move: `internal/domain/` → `domain/` (13 files: `annotation.go`, `domain_test.go`, `errors.go`, `filter.go`, `player.go`, `project_settings.go`, `project_settings_test.go`, `project.go`, `relation.go`, `tag.go`, `task.go`, `task_test.go`, `workflow.go`)
- Modify: all `.go` files across the repo that import `github.com/germanamz/tusk/internal/domain`

- [ ] **Step 1: Move the directory**

```bash
git mv internal/domain domain
```

- [ ] **Step 2: Rewrite all import paths**

```bash
find . -name '*.go' -exec sed -i '' 's|github.com/germanamz/tusk/internal/domain|github.com/germanamz/tusk/domain|g' {} +
```

- [ ] **Step 3: Verify build compiles**

```bash
go build ./...
```

Expected: clean build, zero errors. Every file that imported `internal/domain` now imports `domain`.

- [ ] **Step 4: Run tests**

```bash
go test ./...
```

Expected: all tests pass. This is a pure import path change.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
refactor: move domain package to top-level

Move internal/domain → domain to make core types importable by
external Go programs. Update all import paths across the codebase.

Part of v0.8 Package Restructure — Phase 1.
EOF
)"
```

---

### Task 2: Move config package

**Files:**
- Move: `internal/config/` → `config/` (2 files: `config.go`, `config_test.go`)
- Modify: all `.go` files across the repo that import `github.com/germanamz/tusk/internal/config`

- [ ] **Step 1: Move the directory**

```bash
git mv internal/config config
```

- [ ] **Step 2: Rewrite all import paths**

```bash
find . -name '*.go' -exec sed -i '' 's|github.com/germanamz/tusk/internal/config|github.com/germanamz/tusk/config|g' {} +
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
refactor: move config package to top-level

Move internal/config → config to make configuration types importable
by external Go programs. Update all import paths across the codebase.

Part of v0.8 Package Restructure — Phase 1.
EOF
)"
```

---

### Task 3: Verify no stale internal references for moved packages

This task ensures the import rewrites were exhaustive. No code should reference the old paths.

**Files:**
- None modified — verification only

- [ ] **Step 1: Grep for stale domain imports**

```bash
grep -r 'github.com/germanamz/tusk/internal/domain' --include='*.go' .
```

Expected: zero matches.

- [ ] **Step 2: Grep for stale config imports**

```bash
grep -r 'github.com/germanamz/tusk/internal/config' --include='*.go' .
```

Expected: zero matches.

- [ ] **Step 3: Verify domain package resolves from new location**

```bash
go list github.com/germanamz/tusk/domain
```

Expected output: `github.com/germanamz/tusk/domain`

- [ ] **Step 4: Verify config package resolves from new location**

```bash
go list github.com/germanamz/tusk/config
```

Expected output: `github.com/germanamz/tusk/config`

---

### Task 4: Full test suite with race detector

Final confidence check that the move introduced no subtle issues.

**Files:**
- None modified — verification only

- [ ] **Step 1: Run tests with race detector**

```bash
go test -race ./...
```

Expected: all tests pass with zero race conditions detected.

- [ ] **Step 2: Build the binary and run a smoke test**

```bash
make build && bin/tusk version
```

Expected: version output prints successfully, confirming the binary links correctly.

---

## User-Visible Behavior Preserved

- All CLI commands work identically (`tusk add`, `tusk list`, `tusk info`, etc.)
- MCP server starts and responds to tool calls identically
- All configuration loading from `~/.config/tusk/config.toml` works identically
- All SQLite database operations work identically
- `make build`, `make test`, `make test-race`, `make lint` all pass

## Changes Introduced

**New files:** None (moved, not created)

**Moved directories:**
- `internal/domain/` → `domain/` (13 files)
- `internal/config/` → `config/` (2 files)

**Modified files (import path rewrites only):**
- `internal/repository/*.go` — `domain` import updated
- `internal/filter/*.go` — `domain` import updated
- `internal/service/*.go` — `domain` and `config` imports updated
- `internal/sqlite/*.go` — `domain` import updated
- `internal/inmem/*.go` — `domain` and `config` imports updated
- `internal/tui/*.go` — `domain` and `config` imports updated
- `internal/mcp/*.go` — `domain` and `config` imports updated
- `cmd/tusk/main.go` — `config` import updated

**New dependencies:** None

**Schema migrations:** None

**Bridge code:** None

**Environment variables:** None
