# Phase 1 — Foundation: migration, domain field, SQLite persistence

Initiative: v0.13 Sibling Ordering
Design spec: `docs/superpowers/specs/2026-04-23-sibling-ordering-design.md`

## Prerequisites

None beyond the base codebase (current `main`).

## Goal

Add the `order` column to `tasks`, backfill every existing row with a deterministic dense integer sequence per sibling group, extend the domain types and SQLite repository so `Task.Order` round-trips cleanly, update hierarchical read paths to sort by `order`, and land the repo-level helpers (`NextOrder`, `FirstOrder`, `NeighborOrders`) that the service layer will call in Phase 2.

**User-visible behavior after this phase is unchanged.** `tusk task tree` and `tusk task list parent=…` produce the same output they produced before the phase — every existing task now carries a backfilled `order`, and the new sort clauses (`"order" ASC NULLS LAST, created_at ASC, id ASC`) are equivalent to the prior `created_at ASC` order because the backfill assigned values strictly in `created_at` sequence. No new CLI flag, no new service method, no new MCP surface.

## Tasks

### Task 1.1 — Migration 011_task_order

Create two files.

`migrations/011_task_order.up.sql`:

```sql
ALTER TABLE tasks ADD COLUMN "order" REAL;

WITH numbered AS (
    SELECT
        id,
        CAST(ROW_NUMBER() OVER (
            PARTITION BY parent_id
            ORDER BY created_at ASC, id ASC
        ) AS REAL) AS seq
    FROM tasks
)
UPDATE tasks
SET "order" = (SELECT seq FROM numbered WHERE numbered.id = tasks.id);

CREATE INDEX idx_tasks_parent_order ON tasks(parent_id, "order");
```

`migrations/011_task_order.down.sql`:

```sql
DROP INDEX IF EXISTS idx_tasks_parent_order;
ALTER TABLE tasks DROP COLUMN "order";
```

The embedded migrations are loaded by `migrations/migrations.go`; the loader picks up every `NNN_*.up.sql` / `NNN_*.down.sql` pair automatically. No Go changes in that file.

Acceptance:
- `go test ./sqlite/...` passes.
- On a fresh DB, `PRAGMA table_info(tasks)` shows a new `order REAL` column.
- On a DB seeded with three tasks created in a known `created_at` order under the same parent, after running `up` the three rows have `order = 1.0, 2.0, 3.0` respectively.
- After running `down`, the column and the index are gone and `PRAGMA table_info(tasks)` matches the pre-migration state.

### Task 1.2 — Domain type extensions and sentinel errors

Edit `domain/task.go`:

- Add `Order *float64` to the `Task` struct. Place it after `Priority` (group it with the other numeric/optional fields). Use JSON tag `order` — **do not** apply `omitempty` to it; a null `order` is meaningful on export and must round-trip as JSON `null`.
- Add `Order **float64` to the `TaskUpdate` struct. Place it after `Priority`. Follow the exact same `**T` pattern already used for `Description`, `Level`, `DueAt`, `ParentID`, etc.

Edit `domain/errors.go`:

- Add two new sentinel errors:

  ```go
  // ErrCyclicParent indicates a task move would create a parent cycle.
  ErrCyclicParent = errors.New("task move would create a parent cycle")

  // ErrOrderGapExhausted indicates no float64 midpoint remains between neighbors.
  // The wrapper message produced by the service layer appends the sibling group's
  // parent short ID so the `tusk task move --resequence <parent>` command is
  // copy-pasteable.
  ErrOrderGapExhausted = errors.New("no float64 midpoint remains between neighbors")
  ```

  Keep them alongside the existing `ErrNotFound`, `ErrConflict`, `ErrCyclicBlock`, `ErrInvalidTransition`, `ErrDuplicateRelation` block. No helper functions — the service layer will wrap with `fmt.Errorf("%w: parent %s", ErrOrderGapExhausted, parentShortID)` when emitting.

No other struct changes. Do **not** touch `snapshotTask` / `diffTaskFields` in `service/task.go` — event emission for `order` writes through `task_modified` lands in Phase 2.

> **Scope note.** After this phase, `TaskUpdate.Order` exists at the type level but `TaskService.Update` will not yet apply it: the service's field-diff helper is deliberately left untouched. A Go-library caller that constructs a `TaskUpdate{Order: …}` during Phase 1 will observe their field being silently ignored (no error, no write, no event). This is acceptable because no caller in-tree populates `TaskUpdate.Order` until Phase 3 wires the inline parser. Phase 2 Task 2.4 adds the `Order` branch to `diffTaskFields` and completes the wiring.

Acceptance:
- `go build ./...` passes.
- `go test ./domain/... -run Task` passes without needing new assertions (existing tests don't reference `Order`).

### Task 1.3 — SQLite scan, write, and sort clauses

Edit `sqlite/task.go`:

- Extend the `taskColumns` constant to include `"order"` (quoted, because `order` is a SQL reserved word). Place it immediately after `priority` so the column layout matches the in-memory struct grouping.
- Update `scanTask` (and any helper that unpacks a row into `*domain.Task`) to decode a nullable `REAL` into `*float64`. Use `sql.NullFloat64` as the intermediate and promote to `*float64` on `Valid`. Keep the existing error-wrapping style.
- Update `TaskRepo.Create`'s `INSERT` to pass `nullableFloat(task.Order)`. Add a `nullableFloat` helper alongside the existing `nullableUUID` / `nullableString` helpers in the repo package; on nil it writes `sql.NullFloat64{}`, otherwise `sql.NullFloat64{Float64: *v, Valid: true}`.
- Update `TaskRepo.Update`'s `UPDATE` statement and argument list to include `"order" = ?`. The order of `SET` clauses must match the order of arguments — add `"order" = ?` immediately after `priority = ?` to keep the layout parallel to the struct and `taskColumns` constant.
- Update `TaskRepo.GetChildren`:

  ```go
  fmt.Sprintf(`SELECT %s FROM tasks WHERE parent_id = ? ORDER BY "order" ASC NULLS LAST, created_at ASC, id ASC`, taskColumns)
  ```
- Update `TaskRepo.GetDescendants`: append the same `ORDER BY` clause on the outer `SELECT` of the recursive CTE. The CTE body itself does not need to sort — only the outermost projection does.
- Do **not** change `TaskRepo.List` — filter-driven listings return the service's native order; sorting lives in the renderer / urgency engine.

Add three new methods on `TaskRepo`:

```go
// NextOrder returns max("order") + 1.0 for siblings under parentID. parentID == nil
// scopes to root-level siblings. Returns 1.0 when the group is empty.
func (r *TaskRepo) NextOrder(ctx context.Context, parentID *uuid.UUID) (float64, error)

// FirstOrder returns min("order") - 1.0 for siblings under parentID. parentID == nil
// scopes to root-level siblings. Returns 1.0 when the group is empty.
func (r *TaskRepo) FirstOrder(ctx context.Context, parentID *uuid.UUID) (float64, error)

// NeighborOrders returns the nearest ordered neighbors of pivot within the sibling
// group under parentID. prev is the largest order < pivot (nil if none); next is
// the smallest order > pivot (nil if none). parentID == nil scopes to root.
func (r *TaskRepo) NeighborOrders(ctx context.Context, parentID *uuid.UUID, pivot float64) (prev, next *float64, err error)
```

Implementation notes:
- All three queries must quote `"order"` in the SQL.
- `NextOrder` / `FirstOrder` use `SELECT MAX("order") FROM tasks WHERE parent_id IS ? OR parent_id = ?` — write a small local helper or use `buildParentPredicate(parentID)` if that already exists; otherwise use two separate query branches (one for `parent_id IS NULL`, one for `parent_id = ?`) to keep the `=` path index-friendly.
- `NeighborOrders` runs a single `SELECT` returning two aggregates, e.g.:

  ```sql
  SELECT
      (SELECT "order" FROM tasks WHERE parent_id IS ? AND "order" < ? ORDER BY "order" DESC LIMIT 1),
      (SELECT "order" FROM tasks WHERE parent_id IS ? AND "order" > ? ORDER BY "order" ASC LIMIT 1)
  ```

  Swap the `IS ?` for `= ?` in the non-root branch. Scan both results as `sql.NullFloat64` and promote to `*float64`.

Add the methods to `repository/task.go` (or wherever the `TaskRepository` interface lives) so the interface surface matches the SQLite implementation.

Also register `"order"` in any repo-side filter column map used by `buildFilterExpr` so future filter-grammar work in Phase 3 has a direct column target. **Do not** wire it into the filter parser in this phase — that belongs in Phase 3's task-field work — but reserving the repo-side mapping here keeps the Phase 3 diff small. If the existing code does not have a lookup table to extend, skip this sub-step and leave the repo-side addition for Phase 3.

Acceptance:
- `go test ./sqlite/...` passes; a new round-trip test (Task 1.4) confirms `Order` survives create → fetch → update → fetch cycles.
- Creating three children under one parent via the existing `TaskRepo.Create` path, in any order of `Order` values, and then calling `TaskRepo.GetChildren(parent)` returns them sorted by `(order ASC NULLS LAST, created_at ASC, id ASC)`.
- `NextOrder`, `FirstOrder`, `NeighborOrders` return the expected values against table-test fixtures.

### Task 1.4 — SQLite migration backfill and round-trip tests

Create `sqlite/task_order_test.go` (new file) covering:

- `TestMigration_011_TaskOrder_Backfill`: boot a repo on migration `010`, seed five tasks (mix of root-level and nested, different `created_at` values, duplicate `created_at` across rows to exercise the `id ASC` tiebreak), run migration `011`, assert each row's `order` matches the expected `1.0, 2.0, ...` sequence per sibling group.
- `TestMigration_011_TaskOrder_Down`: run `up` then `down`; assert `PRAGMA table_info(tasks)` no longer shows `order` and the index is gone.
- `TestTaskRepo_OrderRoundTrip`: create a task with `Order = ptrFloat(3.5)`, fetch, assert `Order == 3.5`; update with `Order **float64 = **&(1.25)`, fetch, assert `Order == 1.25`; update with `Order = **nil` (clear), fetch, assert `Order == nil`.
- `TestTaskRepo_NextOrder`: empty parent returns `1.0`; parent with `[1, 2, 3]` returns `4.0`; parent with `[2.5]` returns `3.5`; `parentID == nil` scopes to root.
- `TestTaskRepo_FirstOrder`: empty parent returns `1.0`; parent with `[1, 2, 3]` returns `0.0`; `parentID == nil` scopes to root.
- `TestTaskRepo_NeighborOrders`: fixture `[1, 2, 3]`, pivot `2.5` returns `(2, 3)`; pivot `0.5` returns `(nil, 1)`; pivot `5` returns `(3, nil)`; pivot `2` returns `(1, 3)` (strict `<` / `>`).
- `TestTaskRepo_GetChildren_SortOrder`: seed four children with `order = [3, 1, nil, 2]`, assert `GetChildren` returns them as `[1, 2, 3, nil]`.

Test helpers: reuse `sqlite/sqlitetest` for DB boot; add a small `ptrFloat(v float64) *float64` helper in the test file.

Acceptance: `go test ./sqlite/... -run TaskOrder` passes.

## Preserved User-Visible Behavior

After this phase, every behavior below must still work exactly as it did on `main`:

- `tusk task create "…" priority=N` produces a task with `priority=N` and `order = <deterministic>` (the service-level auto-assign lands in Phase 2; in the meantime the new task is written with `order = NULL`, which sorts last — acceptable because the only paths that read `GetChildren` / `GetDescendants` in text form are the `tree` and `list parent=…` renderers, and a single new task with a NULL `order` at the end of its sibling group is observably identical to its pre-phase position because the post-migration group is already dense-ordered by `created_at`).
- `tusk task tree` renders the same tree shape as before, in the same order, because the backfill preserved `created_at ASC`.
- `tusk task list parent=<id>` returns the same rows in the same order.
- `tusk task get <id>` renders an unchanged task — the new `Order` field is exposed in the JSON output but JSON consumers tolerate new fields (the existing E2E suite asserts on specific fields, not strict schema).
- All existing migration up/down cycles still succeed.

## Changes Introduced

**New files:**
- `migrations/011_task_order.up.sql`
- `migrations/011_task_order.down.sql`
- `sqlite/task_order_test.go`

**Modified files:**
- `domain/task.go` — `Task.Order *float64`, `TaskUpdate.Order **float64`.
- `domain/errors.go` — `ErrCyclicParent`, `ErrOrderGapExhausted` sentinels.
- `sqlite/task.go` — `taskColumns`, `scanTask`, `Create`, `Update`, `GetChildren`, `GetDescendants`, new `NextOrder` / `FirstOrder` / `NeighborOrders` methods, new `nullableFloat` helper.
- `repository/task.go` (interface file — name may differ; locate via `grep 'interface.*TaskRepository'`) — add `NextOrder`, `FirstOrder`, `NeighborOrders` method signatures.

**New DB schema:**
- Column `tasks."order" REAL NULL`.
- Index `idx_tasks_parent_order` on `(parent_id, "order")`.

**Migrations:**
- `011_task_order` (up/down pair).

**Bridge code:** none.

**Dependencies added:** none.
