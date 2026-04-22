# Phase 1 — Foundation: migration, domain types, SQLite persistence

Initiative: v0.13 Task Level Taxonomy
Design spec: `docs/superpowers/specs/2026-04-22-task-level-taxonomy-design.md`

## Prerequisites

None. Baseline codebase (commit `73c4ad1` or later on `main`).

## Goal

Introduce the storage column, the new domain types (`Taxonomy`, `TaxonomyValidator`, `TaxonomyError`), the `Level` field on `Task` / `TaskUpdate`, and the `Taxonomy` field on `ProjectSettings`. Fully persist `level` through the SQLite repo so that a caller who sets `task.Level` can round-trip it. No service-level validation yet; no CLI / MCP / filter surface yet.

User-visible behavior after this phase: unchanged. The new column exists and the new types are importable, but nothing in the product sets `task.Level` or applies the taxonomy validator. Existing task reads/writes continue to work exactly as before.

## Tasks

### Task 1.1 — Migration 010_task_level

Create two files:

- `migrations/010_task_level.up.sql`:
  ```sql
  ALTER TABLE tasks ADD COLUMN level TEXT;
  CREATE INDEX idx_tasks_level ON tasks(level);
  ```
- `migrations/010_task_level.down.sql`:
  ```sql
  DROP INDEX IF EXISTS idx_tasks_level;
  ALTER TABLE tasks DROP COLUMN level;
  ```

Embedded migrations are loaded via `migrations/migrations.go` — confirm the new files are picked up (the loader reads every `*.up.sql` / `*.down.sql` in the package directory).

Acceptance: `go test ./sqlite/...` passes; fresh DB shows `level` column on `tasks` with `PRAGMA table_info(tasks)`.

### Task 1.2 — Domain type extensions

Edit `domain/task.go`:

- Add `Level *string` to `Task` struct, placed after `Description` to keep related text fields grouped.
- Add `Level **string` to `TaskUpdate` struct, placed after `Description`. Follow the same `**T` pattern used for `ParentID`, `DueAt`, etc.

No other struct changes. Do not touch `snapshotTask` or `diffTaskFields` in `service/task.go` — that lands in Phase 3 when the event wiring activates.

Acceptance: `go build ./...` passes.

### Task 1.3 — Domain `Taxonomy` type

Create `domain/taxonomy.go`:

```go
package domain

import (
    "fmt"
    "regexp"
)

// Taxonomy is an ordered list of rank groups. Index 0 is the top rank and
// the only rank whose members may appear as root tasks. Each inner slice is
// a peer set of level names sharing that rank.
type Taxonomy [][]string

// IsEmpty reports whether the taxonomy has no ranks (levels disabled).
func (t Taxonomy) IsEmpty() bool

// Contains reports whether level appears anywhere in the taxonomy.
func (t Taxonomy) Contains(level string) bool

// RankOf returns the rank index for level and true, or 0/false when level
// is not declared.
func (t Taxonomy) RankOf(level string) (int, bool)

// IsTopRank reports whether level sits at rank 0.
func (t Taxonomy) IsTopRank(level string) bool

// Clone returns a deep copy so callers can safely mutate the result.
func (t Taxonomy) Clone() Taxonomy

// Validate rejects malformed taxonomies:
//   - zero ranks
//   - an empty peer group
//   - a level name that doesn't match [a-zA-Z_][a-zA-Z0-9_-]*
//   - a duplicate level name anywhere in the taxonomy
func (t Taxonomy) Validate() error
```

Reuse `udaKeyPattern` from `domain/task.go` for the name regex — copy its `regexp.MustCompile` literal into this file under a new unexported `levelNamePattern` var to avoid cross-file tight coupling.

Create `domain/taxonomy_test.go` covering:

- `Validate` accepts a well-formed taxonomy.
- Each reject branch returns a non-nil error referencing the offending input.
- `RankOf`, `Contains`, `IsTopRank`, `IsEmpty`, `Clone` behave as documented.

Acceptance: `go test ./domain/... -run Taxonomy` passes.

### Task 1.4 — Validator and typed error

Create `domain/errors.go` additions (append to existing file, do not replace):

```go
var ErrTaxonomyViolation = errors.New("task violates project taxonomy")

// TaxonomyError describes how a task violates its project's taxonomy.
// Wraps ErrTaxonomyViolation so errors.Is works.
type TaxonomyError struct {
    Level       string   // level the task was assigned ("" when missing)
    ParentLevel string   // parent's level ("" when no parent or parent has no level)
    Reason      string   // "missing" | "unknown_level" | "root_requires_top_rank" | "parent_rank_not_lower"
    Taxonomy    Taxonomy // taxonomy that produced the violation; for rendering
}

func (e *TaxonomyError) Error() string
func (e *TaxonomyError) Unwrap() error { return ErrTaxonomyViolation }
```

Create `domain/taxonomy_validator.go`:

```go
package domain

// ValidationContext carries everything TaxonomyValidator needs without
// requiring repository access.
type ValidationContext struct {
    Taxonomy    Taxonomy
    ParentLevel *string // nil when the task has no parent; "" when parent has no level
}

type TaxonomyValidator struct{}

// Check returns nil when task satisfies vc.Taxonomy, otherwise a *TaxonomyError
// wrapping ErrTaxonomyViolation. Empty taxonomies accept any task state.
func (TaxonomyValidator) Check(vc ValidationContext, task *Task) error
```

Implement the five rules exactly as spec § 5 describes. No repository access. No side effects.

Create `domain/taxonomy_validator_test.go` covering each of the four `Reason` values, the empty-taxonomy short-circuit, the root-with-top-rank allow case, and the parent-rank-strict-less allow case.

Acceptance: `go test ./domain/... -run Validator` passes; `errors.Is(err, ErrTaxonomyViolation)` is true for every rejection.

### Task 1.5 — ProjectSettings.Taxonomy tristate

Edit `domain/project_settings.go`:

- Add `Taxonomy *Taxonomy \`json:"taxonomy,omitempty"\`` to `ProjectSettings`.

Create `domain/project_settings_test.go` additions (or a new `taxonomy_tristate_test.go` if the existing file is tight) covering JSON round-trip of each tristate:

| Go value                  | JSON                                               |
| ------------------------- | -------------------------------------------------- |
| `Taxonomy == nil`         | key absent                                         |
| `&domain.Taxonomy{}`      | `"taxonomy": []`                                   |
| `&populated`              | `"taxonomy": [["milestone"], ...]`                 |

Acceptance: test passes, tristate survives marshal+unmarshal.

### Task 1.6 — SQLite task persistence for `level`

Edit `sqlite/task.go`:

- Add `level` to the `taskColumns` constant.
- `TaskRepo.Create` — bind `nullableString(task.Level)` in the extended VALUES list.
- `TaskRepo.Update` — add `level = ?` to the SET list, bind `nullableString(task.Level)`.
- `TaskRepo.scanOne` and any list-scan helper — scan `level` into a `sql.NullString`, assign to `task.Level` when valid.

Do not touch the `List` query predicate builder — that lands in Phase 2 when `TaskFilter.Levels` is introduced.

Extend `sqlite/task_test.go`:

- A test that `Create` persists `task.Level = "story"` and `GetByID` round-trips it.
- A test that `Update` can switch `level` from `"story"` to `"task"`, and from set to NULL.
- A test that `scanOne` yields `Level == nil` for a NULL column.

Acceptance: `go test ./sqlite/... -run Level` passes; `go test ./sqlite/...` is green overall.

## Changes Introduced

**New files:**
- `migrations/010_task_level.up.sql`, `migrations/010_task_level.down.sql`
- `domain/taxonomy.go`, `domain/taxonomy_test.go`
- `domain/taxonomy_validator.go`, `domain/taxonomy_validator_test.go`

**Modified files:**
- `domain/task.go` — `Task.Level *string`, `TaskUpdate.Level **string`
- `domain/errors.go` — `ErrTaxonomyViolation`, `TaxonomyError` type
- `domain/project_settings.go` — `ProjectSettings.Taxonomy *Taxonomy`
- `sqlite/task.go` — column list and CRUD bindings for `level`
- `sqlite/task_test.go` — level persistence round-trip tests
- `domain/project_settings_test.go` (or new `*_test.go` for tristate JSON)

**Schema migration:** `010_task_level` adds the `level TEXT` column and index on `tasks`.

**New environment variables / dependencies:** none.

**Bridge code:** none — every new type is usable on its own; the SQLite layer is fully wired for reads/writes.

## Behavioral Acceptance

After this phase the following must still work unchanged:

- Every CLI command that existed before (`tusk task list`, `tusk task create`, etc.).
- Every MCP tool (no signatures change).
- E2E suite passes.
- `go build ./...`, `make test`, `make vet`, `make lint` all pass.
- Fresh `tusk init` + create task works end-to-end; task round-trips through SQLite with `level` scanning as NULL.

Additionally:

- Setting `task.Level = ptr("story")` in code (via a direct repo call in tests) persists and reads back through `TaskRepo`.
- `domain.Taxonomy{{"milestone"}, {"story"}}.RankOf("story")` returns `(1, true)`.
- `domain.TaxonomyValidator{}.Check(...)` returns `*TaxonomyError` wrapping `ErrTaxonomyViolation` for every spec § 5 rule.
