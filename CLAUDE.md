# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Tusk is a concurrent-safe task management CLI written in Go with MCP server support. Single binary, SQLite persistence, hierarchical tasks, typed relations, pluggable storage backends.

## Commands

```bash
make build          # Compile to bin/tusk
make test           # Unit + e2e tests
make test-race      # Tests with race detector
make test-e2e       # E2e tests only
make vet            # go vet
make lint           # golangci-lint run ./...

# Single unit test
go test -v ./service -run TestTaskCreate

# Single e2e scenario
go test -v ./tests/e2e -run TestErrorHandling
```

## Architecture

Layered design — dependencies flow downward only:

```
Interface Layer (CLI via Cobra, MCP server)
    ↓
Service Layer (business logic, validation)
    ↓
Repository Layer (Go interfaces only)
    ↓
Storage Implementations (SQLite with WAL mode)
```

**Key packages:**
- `cmd/tusk/` — entry point, DI wiring, flag/env parsing
- `domain/` — core types and sentinel errors, no dependencies
- `service/` — business logic (TaskService, ProjectService, WorkflowService, RelationService, TagService, UrgencyEngine)
- `repository/` — interface definitions only
- `sqlite/` — SQLite implementations of repository interfaces
- `filter/` — 3-stage filter parser: Lexer → Parser → Resolver
- `config/` — Viper-based config loading
- `inmem/` — in-memory repository implementations (project, workflow)
- `internal/mcp/` — MCP server (stdio + SSE transports)
- `internal/tui/` — CLI commands + output rendering (text and JSON)
- `migrations/` — embedded SQL migration files
- `tests/e2e/` — black-box CLI tests

## Key Patterns

**Optimistic locking:** Every mutable entity has a `version` field. Updates use `WHERE id = ? AND version = ?`; if `rows_affected == 0`, return `domain.ErrConflict`.

**Double-pointer updates:** `TaskUpdate` uses `**string`/`**uuid.UUID` for nullable fields — `nil` = don't change, `*nil` = set NULL, `*"value"` = set value.

**Error handling:** Sentinel errors in `domain/errors.go` (`ErrNotFound`, `ErrConflict`, `ErrCyclicBlock`, `ErrInvalidTransition`, `ErrDuplicateRelation`). Always check with `errors.Is()`.

**UUID + 8-char short ID:** Tasks get both — UUID for internal use, short ID for CLI display. Short ID collisions are handled.

**Soft delete:** Tasks transition to `deleted` status via workflow, not removed from DB.

**Filter syntax:** TaskWarrior-inspired — `status=pending,active`, `priority=2..4`, `due=today`, `+tag`, `-tag`, `parent=<short_id>`, `tree=<short_id>`. Uses `=` as the field separator (`:` is reserved for value modifiers like ordered sequences).

## E2E Test Harness

Tests in `tests/e2e/` use a custom harness. Each scenario runs 4 times (2 DB config modes × 2 output formats). Steps can reference prior results with `$0.short_id`.

```go
scenarios := []Scenario{
    {
        Name: "create_and_info",
        Steps: []Step{
            {Args: []string{"add", "My task"}},
            {Args: []string{"info", "$0.short_id"}},
        },
    },
}
```

## Database

Default path: `~/.local/share/tusk/tusk.db`. Override: `--db` flag > `TUSK_DB` env var > default.

SQLite pragmas: WAL mode, busy_timeout=5000, foreign_keys=ON.

Default project `_default` (UUID all zeros) with workflow: pending ↔ active, active → completed, {pending,active} → deleted, completed → pending.

## Commits

Conventional commits with scope: `test(e2e):`, `fix:`, `docs:`, `feat:`.

## References

- `PRODUCT.md` — product-level description of implemented features
- `docs/v0.1-status.md` — v0.1 implementation recap
- `config/default.toml` — embedded default configuration (source of truth for all defaults)
