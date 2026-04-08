# Package Restructure — Phase 3: Service & Storage Packages (service, sqlite, inmem)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the three tier-2 packages (`service`, `sqlite`, `inmem`) from `internal/` to top-level, completing the package restructure initiative. After this phase, `internal/` contains only `tui/` and `mcp/`.

**Architecture:** Pure mechanical refactor — `git mv` directories, rewrite import paths with `sed`, verify compilation and tests. No behavioral changes.

**Tech Stack:** Go (module `github.com/germanamz/tusk`), `git mv`, `sed`

**Prerequisites:** Phase 1 and Phase 2 must both be completed first. Phase 3 depends on `domain`, `config`, `repository`, and `filter` already being at top-level.

---

## Inherits From

**Phase 1** moved to top-level:
- `internal/domain/` → `domain/` — core types and sentinel errors
- `internal/config/` → `config/` — Viper-based config loading

**Phase 2** moved to top-level:
- `internal/repository/` → `repository/` — storage interface definitions
- `internal/filter/` → `filter/` — filter parser (Lexer → Parser → Resolver)

The implementer should expect:
- `domain/`, `config/`, `repository/`, `filter/` exist as top-level directories
- All imports across the codebase reference these at their new top-level paths
- `internal/` contains: `service/`, `sqlite/`, `inmem/`, `tui/`, `mcp/`
- All tests pass, binary builds cleanly

---

## Context

This phase moves the three tier-2 packages — the ones that depend on tier-0 and tier-1 packages (all already moved):

- `internal/service/` (15 .go files) — business logic (TaskService, TagService, RelationService, ProjectService, WorkflowService, PlayerService, UrgencyEngine). Imports `domain` and `repository` in production code. Test files also import `config`, `inmem`, and `sqlite` (all moving in this same phase).
- `internal/sqlite/` (15 .go files) — SQLite implementations of repository interfaces. Imports `domain` and `repository`.
- `internal/inmem/` (4 .go files) — in-memory implementations of project and workflow repositories, backed by config. Imports `config`, `domain`, and `repository`.

**Cross-references within this phase:** `service` test files import `sqlite` and `inmem`. `filter` test files (already at `filter/`) import `sqlite`. Since all three packages move in this phase, the order matters: move `sqlite` and `inmem` before `service` so that when `service` imports are rewritten, all targets exist. Actually, since `git mv` + `sed` is atomic per package and `sed` rewrites globally, any order works — but we move `sqlite` first for clarity.

## Import Rewrite Technique

Same as Phases 1–2:

```bash
git mv internal/X X
find . -name '*.go' -exec sed -i '' 's|github.com/germanamz/tusk/internal/X|github.com/germanamz/tusk/X|g' {} +
```

## Consumers of each package (will have their imports rewritten)

**service** is imported by:
- `internal/tui/` — `app.go`, `commands_test.go`
- `internal/mcp/` — `server.go`, `handlers_test.go`
- `cmd/tusk/main.go`

**sqlite** is imported by:
- `internal/service/` test files — `tag_test.go`, `relation_test.go`, `task_test.go`, `task_claim_test.go`, `player_test.go`
- `filter/` test files — `integration_test.go`, `resolve_test.go` (already at top-level from Phase 2)
- `internal/tui/` — `commands_test.go`
- `internal/mcp/` — `handlers_test.go`
- `cmd/tusk/main.go`

**inmem** is imported by:
- `internal/service/` test files — `workflow_test.go`, `project_test.go`, `tag_test.go`, `relation_test.go`, `task_test.go`, `task_claim_test.go`
- `internal/tui/` — `commands_test.go`
- `internal/mcp/` — `handlers_test.go`
- `cmd/tusk/main.go`

---

### Task 1: Move sqlite package

Moving sqlite first because service and filter tests depend on it.

**Files:**
- Move: `internal/sqlite/` → `sqlite/` (15 files: `annotation.go`, `annotation_count_test.go`, `annotation_test.go`, `player.go`, `player_test.go`, `relation.go`, `relation_count_test.go`, `relation_test.go`, `store.go`, `store_test.go`, `tag.go`, `tag_test.go`, `task.go`, `task_test.go`, `tx_test.go`)
- Modify: all `.go` files across the repo that import `github.com/germanamz/tusk/internal/sqlite`

- [ ] **Step 1: Move the directory**

```bash
git mv internal/sqlite sqlite
```

- [ ] **Step 2: Rewrite all import paths**

```bash
find . -name '*.go' -exec sed -i '' 's|github.com/germanamz/tusk/internal/sqlite|github.com/germanamz/tusk/sqlite|g' {} +
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

Expected: all tests pass. This includes `filter/integration_test.go` and `filter/resolve_test.go` which previously imported `internal/sqlite` — they now import `sqlite` (the new location).

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
refactor: move sqlite package to top-level

Move internal/sqlite → sqlite to make the SQLite storage backend
importable by external Go programs. Update all import paths.

Part of v0.8 Package Restructure — Phase 3.
EOF
)"
```

---

### Task 2: Move inmem package

**Files:**
- Move: `internal/inmem/` → `inmem/` (4 files: `project.go`, `project_test.go`, `workflow.go`, `workflow_test.go`)
- Modify: all `.go` files across the repo that import `github.com/germanamz/tusk/internal/inmem`

- [ ] **Step 1: Move the directory**

```bash
git mv internal/inmem inmem
```

- [ ] **Step 2: Rewrite all import paths**

```bash
find . -name '*.go' -exec sed -i '' 's|github.com/germanamz/tusk/internal/inmem|github.com/germanamz/tusk/inmem|g' {} +
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
refactor: move inmem package to top-level

Move internal/inmem → inmem to make in-memory repository
implementations importable by external Go programs. Update all
import paths.

Part of v0.8 Package Restructure — Phase 3.
EOF
)"
```

---

### Task 3: Move service package

**Files:**
- Move: `internal/service/` → `service/` (15 files: `player.go`, `player_test.go`, `project.go`, `project_test.go`, `relation.go`, `relation_test.go`, `tag.go`, `tag_test.go`, `task.go`, `task_claim_test.go`, `task_test.go`, `urgency.go`, `urgency_test.go`, `workflow.go`, `workflow_test.go`)
- Modify: all `.go` files across the repo that import `github.com/germanamz/tusk/internal/service`

- [ ] **Step 1: Move the directory**

```bash
git mv internal/service service
```

- [ ] **Step 2: Rewrite all import paths**

```bash
find . -name '*.go' -exec sed -i '' 's|github.com/germanamz/tusk/internal/service|github.com/germanamz/tusk/service|g' {} +
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
refactor: move service package to top-level

Move internal/service → service to make business logic importable
by external Go programs. Update all import paths.

Part of v0.8 Package Restructure — Phase 3.
EOF
)"
```

---

### Task 4: Verify internal/ contains only tui and mcp

This is the acceptance gate for the entire Package Restructure initiative.

**Files:**
- None modified — verification only

- [ ] **Step 1: List internal/ contents**

```bash
ls internal/
```

Expected output:
```
mcp  tui
```

Only `tui` and `mcp` remain — these are CLI/server wiring and intentionally stay internal.

- [ ] **Step 2: Grep for any stale internal references to moved packages**

```bash
grep -rn 'github.com/germanamz/tusk/internal/' --include='*.go' . | grep -v '/internal/tui/' | grep -v '/internal/mcp/'
```

Expected: zero matches. The only remaining `internal/` imports should be `internal/tui` (from `cmd/tusk/main.go`) and `internal/mcp` (from `internal/tui/app.go`).

- [ ] **Step 3: Verify all 7 moved packages resolve**

```bash
go list \
  github.com/germanamz/tusk/domain \
  github.com/germanamz/tusk/config \
  github.com/germanamz/tusk/repository \
  github.com/germanamz/tusk/filter \
  github.com/germanamz/tusk/service \
  github.com/germanamz/tusk/sqlite \
  github.com/germanamz/tusk/inmem
```

Expected: all 7 packages listed.

- [ ] **Step 4: Run full test suite with race detector**

```bash
go test -race ./...
```

Expected: all tests pass with zero race conditions.

- [ ] **Step 5: Build binary and smoke test**

```bash
make build && bin/tusk version
```

Expected: version output prints successfully.

- [ ] **Step 6: Run e2e tests**

```bash
make test-e2e
```

Expected: all e2e tests pass. E2E tests shell out to the built binary and don't import internal packages, but this confirms the binary is fully functional end-to-end.

---

## User-Visible Behavior Preserved

- All CLI commands work identically (`tusk add`, `tusk list`, `tusk info`, `tusk start`, `tusk done`, `tusk delete`, `tusk modify`, `tusk annotate`, `tusk link`, `tusk unlink`, `tusk tree`, `tusk next`, `tusk pop`, `tusk available`, `tusk claim`, `tusk release`, `tusk player register`, `tusk project list`, `tusk workflow list`, `tusk workflow info`, `tusk tag list/create/delete/rename/modify`)
- MCP server starts and responds to all tool calls identically
- All filter syntax works identically
- SQLite database operations (WAL mode, migrations, optimistic locking) work identically
- Configuration loading from `~/.config/tusk/config.toml` works identically
- `make build`, `make test`, `make test-race`, `make test-e2e`, `make lint` all pass

## Changes Introduced

**Moved directories:**
- `internal/service/` → `service/` (15 files)
- `internal/sqlite/` → `sqlite/` (15 files)
- `internal/inmem/` → `inmem/` (4 files)

**Modified files (import path rewrites only):**
- `cmd/tusk/main.go` — `service`, `sqlite`, `inmem` imports updated
- `internal/tui/*.go` — `service`, `sqlite`, `inmem` imports updated
- `internal/mcp/*.go` — `service`, `sqlite`, `inmem` imports updated
- `filter/integration_test.go` — `sqlite` import updated (this file was already at top-level from Phase 2)
- `filter/resolve_test.go` — `sqlite` import updated (same)

**Final state of `internal/`:**
```
internal/
├── mcp/    (8 files — MCP server, stays internal)
└── tui/    (17 files — CLI rendering, stays internal)
```

**New dependencies:** None

**Schema migrations:** None

**Bridge code:** None

**Environment variables:** None
