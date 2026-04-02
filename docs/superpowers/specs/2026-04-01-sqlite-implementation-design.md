# SQLite Implementation with Migrations — Design Spec

**Date:** 2026-04-01
**Scope:** `internal/sqlite/` package — concrete SQLite implementations of all repository interfaces, plus migration runner.

---

## Overview

Implement the SQLite storage layer for Tusk. This fills the `internal/sqlite/` stub files with working code that satisfies the six repository interfaces defined in `internal/repository/`. Includes an embedded migration runner that applies `migrations/*.up.sql` files at startup.

---

## Store (`store.go`)

The `Store` is pure infrastructure — no repository methods.

### Responsibilities

- Open SQLite connection with pragmas: WAL mode, `busy_timeout=5000`, `foreign_keys=ON`
- Embed `migrations/` directory via `go:embed`
- Apply migrations on startup using a `schema_migrations` table
- Expose `DB() *sql.DB` for repo structs
- `Close()` to shut down the connection

### Constructor

`New(dbPath string) (*Store, error)` — opens the database, sets pragmas, runs migrations, returns a ready-to-use Store.

### Migration Runner

- Creates `schema_migrations` table if it doesn't exist (columns: `version INT PRIMARY KEY`, `applied_at TEXT`)
- Reads embedded `.up.sql` files, sorts by version number prefix
- Skips already-applied versions
- Applies new migrations inside a transaction, records each version in `schema_migrations`
- Pragmas are set **before** migrations (outside any transaction, since `PRAGMA journal_mode` cannot run inside a transaction)

---

## Repo Structs

Six separate structs, one per file, each holding a `*sql.DB` reference:

| Struct | File | Implements | Notes |
|---|---|---|---|
| `TaskRepo` | `task.go` | `repository.TaskRepository` | Filters, hierarchy, optimistic locking |
| `RelationRepo` | `relation.go` | `repository.RelationRepository` | Directional queries (blocking/blocked_by) |
| `ProjectRepo` | `project.go` | `repository.ProjectRepository` | Straightforward CRUD |
| `TagRepo` | `tag.go` | `repository.TagRepository` | Join table ops (AssignToTask, RemoveFromTask, GetTaskTags) |
| `WorkflowRepo` | `workflow.go` | `repository.WorkflowRepository` | JSON statuses column marshal/unmarshal |
| `AnnotationRepo` | `annotation.go` | `repository.AnnotationRepository` | Simple CRUD, immutable after create |

Each constructed via `NewXxxRepo(db *sql.DB) *XxxRepo`.

**New files needed:** `workflow.go` and `annotation.go` (not yet in `internal/sqlite/`).

---

## Key Implementation Patterns

### Optimistic Locking (Task Update/Delete)

```sql
UPDATE tasks SET ..., version = version + 1, modified_at = ?
WHERE id = ? AND version = ?
```

If `rows_affected == 0`, return `domain.ErrConflict`. Same pattern for `Delete`.

### TaskFilter → Dynamic WHERE Clauses

`List` builds a query dynamically. Each non-nil `TaskFilter` field appends a `WHERE` condition and a parameter. Uses positional `?` placeholders. Empty filter returns all tasks.

### GetDescendants — Recursive CTE

```sql
WITH RECURSIVE descendants AS (
    SELECT * FROM tasks WHERE parent_id = ?
    UNION ALL
    SELECT t.* FROM tasks t JOIN descendants d ON t.parent_id = d.id
)
SELECT * FROM descendants
```

Single query, single round-trip. SQLite has solid recursive CTE support.

### JSON Columns

- `Workflow.Statuses` (`[]string`) — `json.Marshal` on write, `json.Unmarshal` on read
- `Task.UDA` (`map[string]any`) — same pattern. Stored as `'{}'` when empty (matches migration default)

### Nullable Fields Mapping

- `*uuid.UUID` fields (`ParentID`, `ProjectID`) → `sql.NullString` (UUIDs stored as TEXT)
- `*time.Time` fields (`DueAt`, `WaitUntil`) → `sql.NullString` (ISO 8601 strings)
- `*string` fields (`RecurrenceRule`, `Tag.Color`) → `sql.NullString`

### Row Scanning Helpers

Private `scanTask` helper in `task.go` scans a `*sql.Row` or `*sql.Rows` into a `*domain.Task`, handling all nullable conversions in one place. Similar helpers for other types where useful.

---

## Testing Strategy

### Test Database

Each test function creates an in-memory SQLite database (`:memory:`) via `store.New(":memory:")`. Migrations run automatically, giving a fresh schema per test. Fast, no cleanup needed.

### Test Files

One test file per repo: `task_test.go`, `relation_test.go`, `project_test.go`, `tag_test.go`, `workflow_test.go`, `annotation_test.go`, plus `store_test.go` for migration/connection tests.

### What Gets Tested

- **`store_test.go`** — DB opens, pragmas are set (WAL, foreign_keys), migrations applied, `schema_migrations` table populated correctly
- **Each repo test** — happy path CRUD, error cases (`ErrNotFound`, `ErrConflict`, `ErrDuplicateRelation`), filter combinations for `TaskRepo.List`, recursive `GetDescendants` with 3+ levels, nullable field round-trips

### Interface Satisfaction

Each test file includes `var _ repository.XxxRepository = (*XxxRepo)(nil)` compile-time checks.

### No Mocks

These are integration tests hitting real SQLite. That's the purpose of this layer.

---

## Files Changed/Created

| File | Action |
|---|---|
| `internal/sqlite/store.go` | Rewrite from stub |
| `internal/sqlite/task.go` | Rewrite from stub |
| `internal/sqlite/relation.go` | Rewrite from stub |
| `internal/sqlite/project.go` | Rewrite from stub |
| `internal/sqlite/tag.go` | Rewrite from stub |
| `internal/sqlite/workflow.go` | Create new |
| `internal/sqlite/annotation.go` | Create new |
| `internal/sqlite/store_test.go` | Create new |
| `internal/sqlite/task_test.go` | Create new |
| `internal/sqlite/relation_test.go` | Create new |
| `internal/sqlite/project_test.go` | Create new |
| `internal/sqlite/tag_test.go` | Create new |
| `internal/sqlite/workflow_test.go` | Create new |
| `internal/sqlite/annotation_test.go` | Create new |

---

## Decisions

| Decision | Chosen | Rationale |
|---|---|---|
| Migration strategy | Embedded SQL + programmatic runner | Fits single-binary philosophy, reuses existing `.sql` files |
| Package structure | Store + separate repo structs | Matches file layout, keeps each file focused on one interface |
| Recursive queries | Recursive CTE | Single round-trip, SQLite supports it well, practical depth ~4 levels |
