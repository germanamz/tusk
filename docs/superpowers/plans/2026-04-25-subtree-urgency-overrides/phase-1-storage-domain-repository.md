# Phase 1 — Storage, Domain Types, and Repository

**Spec:** `docs/superpowers/specs/2026-04-25-subtree-urgency-overrides-design.md`
**Phase size:** 5 tasks

## Prerequisites

Base codebase as of commit `144643f`. No prior phases required; this is the first phase.

## Goal

Land the database column, domain types, validator, and repository surface that later phases will build on. After this phase the `tasks.urgency_overrides` column exists, round-trips cleanly, and the recursive-CTE ancestor lookup is callable — but no service or UI behavior changes for users.

## Tasks

### Task 1.1 — Migration 012: add `urgency_overrides` column

1. Create `migrations/012_task_urgency_overrides.up.sql`:
   ```sql
   ALTER TABLE tasks ADD COLUMN urgency_overrides TEXT;
   ```
   No index, no default. Nullable.
2. Create `migrations/012_task_urgency_overrides.down.sql`:
   ```sql
   ALTER TABLE tasks DROP COLUMN urgency_overrides;
   ```
3. Confirm the migration is picked up by the embedded-migrations loader in `migrations/migrations.go`. Follow the exact pattern used by `011_task_order.up.sql` / `.down.sql` — that migration was added most recently and is the right reference.
4. Verify `make test ./sqlite/...` still passes; migrations run cleanly from a fresh database, and the existing migration harness in `sqlite/store_test.go` exercises the up path.

### Task 1.2 — Domain types

1. In `domain/task.go`, add to the `Task` struct after the existing `UDA map[string]any` field:
   ```go
   UrgencyOverrides *UrgencyOverrides
   ```
   Reuses the existing `domain.UrgencyOverrides` struct already defined in `domain/project_settings.go`. Do not move or rename that struct — keep the existing location; the import is already in scope (same package).
2. Create `domain/urgency_overrides_patch.go`:
   ```go
   package domain

   // UrgencyOverridesPatch describes an RFC 7396-style merge patch applied to
   // a task's urgency_overrides column. ClearAll runs first, then Clear keys,
   // then Set keys. See the spec's §1 for ordering rules.
   type UrgencyOverridesPatch struct {
       Set      map[string]float64 // key → new value
       Clear    map[string]bool    // key → true means delete
       ClearAll bool               // drop every key
   }
   ```
   Key strings use the snake_case form (`priority_weight`, `due_weight`, …) — same keyspace the project-level CLI parser already maps into via `urgencyCLIToConfigKey` (see `internal/tui/project_parse.go:45`).
3. Create `domain/urgency_overrides_validator.go`:
   ```go
   package domain

   import "fmt"

   // ValidUrgencyWeightKeys lists the 10 keys accepted in any urgency-overrides
   // input. Exported so CLI/MCP error messages can render the same set.
   var ValidUrgencyWeightKeys = []string{
       "priority_weight", "due_weight", "age_weight", "active_weight",
       "blocking_weight", "blocked_weight", "tags_weight", "project_weight",
       "annotations_weight", "waiting_weight",
   }

   // ValidateUrgencyOverridesPatch accepts only the 10 known weight keys and
   // values that are JSON numbers or explicit nil. Typo-friendly error names
   // the offending key and lists valid keys.
   func ValidateUrgencyOverridesPatch(raw map[string]any) error
   ```
   Implementation:
   - For each key in `raw`: if key not in `ValidUrgencyWeightKeys`, return `fmt.Errorf("unknown urgency weight %q; valid keys: %s", key, strings.Join(ValidUrgencyWeightKeys, ", "))`.
   - For each value: accept `nil` (will map to a Clear later), `float64`, `float32`, `int`, `int64` (coerce numeric types — the MCP JSON decoder may deliver any of these). Reject all other types with `fmt.Errorf("urgency weight %q must be a number or null, got %T", key, value)`.
4. Reuse the existing `domain.UrgencyOverrides` field names (`PriorityWeight`, `DueWeight`, …). The snake_case keys in `ValidUrgencyWeightKeys` match the existing JSON tags on that struct (see `domain/project_settings.go:19-30`), so downstream code can round-trip either form via a small key ↔ field lookup (see Task 3.2 in Phase 3 for the consumer).

### Task 1.3 — Repository interface for ancestor lookup

1. In `repository/task.go` (or wherever `TaskRepository` is defined — match the existing `TaskRepository` interface file), add:
   ```go
   // AncestorOverride is one node in a task ancestor walk. Overrides is nil
   // when the node has no urgency_overrides JSON set.
   type AncestorOverride struct {
       TaskID    uuid.UUID
       ParentID  *uuid.UUID
       ProjectID uuid.UUID
       Overrides *domain.UrgencyOverrides
   }

   // GetAncestorOverrides returns every input task plus every ancestor
   // reachable via parent_id, one row per visited node. Root nodes have a
   // nil ParentID. Nodes without overrides have a nil Overrides pointer.
   // Implementations must be safe to call with a zero-length input
   // (returns an empty slice, no error).
   GetAncestorOverrides(ctx context.Context, taskIDs []uuid.UUID) ([]AncestorOverride, error)
   ```
2. If the repository package defines mock / fake implementations for testing (check `sqlite/sqlitetest/` and any other test helpers), add a stub implementation to each that returns `nil, nil` or the simplest correct behavior. Keep it compile-safe.

### Task 1.4 — SQLite implementation

1. In `sqlite/task.go`:
   - Extend the `taskColumns` constant (around line 18) to include `urgency_overrides` in the exact position that matches the struct-field order — place after `uda` to match the domain struct's new field position. Ensure every SELECT / INSERT / UPDATE statement that references `taskColumns` continues to compile after the list change.
   - Extend the `scanTask` function (around line 540 — see the existing scanner that handles `Order *float64` via `sql.NullFloat64`, line 546) to scan `urgency_overrides` via `sql.NullString`. On `Valid == true`, `json.Unmarshal` the string into a fresh `*domain.UrgencyOverrides` and assign to `t.UrgencyOverrides`. On invalid, leave nil. On JSON decode error, return a wrapped error: `fmt.Errorf("scanning task %s: decoding urgency_overrides: %w", id, err)`.
   - Extend the INSERT helper (around line 38 — the slice passed to `ExecContext` when creating a task) and the UPDATE helper (around line 80) to round-trip `urgency_overrides`:
     - Marshal via a small helper `nullableURIOverrides(task.UrgencyOverrides) any`: returns `nil` when pointer is nil, otherwise `json.Marshal` and return the string. Name to match existing conventions (`nullableFloat`, `nullableUUID`).
2. Implement `TaskRepo.GetAncestorOverrides`. Build the SQL with a recursive CTE:
   ```sql
   WITH RECURSIVE ancestors(id, parent_id, project_id, urgency_overrides) AS (
       SELECT id, parent_id, project_id, urgency_overrides
       FROM tasks
       WHERE id IN (%s)
       UNION
       SELECT t.id, t.parent_id, t.project_id, t.urgency_overrides
       FROM tasks t
       INNER JOIN ancestors a ON t.id = a.parent_id
   )
   SELECT id, parent_id, project_id, urgency_overrides FROM ancestors;
   ```
   Where `%s` is an inlined `?,?,...` placeholder list (safe — task IDs are UUIDs bound as parameters). The recursion terminates at any node with `parent_id IS NULL`. Return an empty slice (not nil, not error) when `len(taskIDs) == 0`. Decode the JSON column into `*domain.UrgencyOverrides` per row, nil if the column is NULL.

### Task 1.5 — Repository tests

1. In `sqlite/task_test.go`, add `TestTaskUrgencyOverridesRoundTrip`:
   - Use the existing `sqlitetest` harness to get a fresh DB.
   - Create a task, call `Update` with `UrgencyOverrides = &domain.UrgencyOverrides{BlockingWeight: ptrFloat(20.0), DueWeight: ptrFloat(3.5)}`, read it back via `Get`, and assert `reflect.DeepEqual` on the read struct's `UrgencyOverrides` pointer target.
   - Create another task, set `UrgencyOverrides = nil`, assert the column is NULL after round-trip (read back as `nil`, not an empty struct).
2. Add `TestGetAncestorOverrides`:
   - Fixture: build a 4-deep parent chain — `root → A → B → C` — plus an unrelated sibling task under `root`.
   - Set `urgency_overrides` on `root` (e.g. `BlockingWeight = 10`) and on `B` (e.g. `DueWeight = 5`). Leave `A` and `C` with nil overrides.
   - Call `GetAncestorOverrides(ctx, []uuid.UUID{C.ID})`. Assert the returned set is exactly `{C, B, A, root}` (order-insensitive; index returned rows by `TaskID` and compare map keys). Sibling task must NOT appear. Verify `root.Overrides != nil`, `B.Overrides != nil`, `A.Overrides == nil`, `C.Overrides == nil`.
   - Call `GetAncestorOverrides(ctx, []uuid.UUID{C.ID, sibling.ID})`. Assert the union of both walks (`C, B, A, root, sibling`), no duplicates on `root`.
   - Call `GetAncestorOverrides(ctx, []uuid.UUID{})`. Assert an empty slice, no error.

## User-visible behaviors that must still work after this phase

- All existing CRUD, list, tree, get, modify, move, link, and claim operations on tasks continue to behave identically. The new column is invisible to every caller.
- `tusk task get --output json` output is byte-identical to pre-Phase-1 (no new fields added to `taskJSON` in this phase — that belongs to Phase 4).
- Existing migrations 001–011 still apply cleanly to a fresh database.
- `make test` and `make test-race` pass.

## Bridge code

None.

## Changes Introduced

- **New files:**
  - `migrations/012_task_urgency_overrides.up.sql`
  - `migrations/012_task_urgency_overrides.down.sql`
  - `domain/urgency_overrides_patch.go`
  - `domain/urgency_overrides_validator.go`
- **Modified files:**
  - `domain/task.go` — `Task.UrgencyOverrides *UrgencyOverrides` field.
  - `repository/task.go` (or equivalent interface file) — `AncestorOverride` struct, `GetAncestorOverrides` method on the interface.
  - `sqlite/task.go` — column round-trip in `taskColumns`, `scanTask`, and INSERT/UPDATE helpers; `GetAncestorOverrides` implementation.
  - `sqlite/task_test.go` — `TestTaskUrgencyOverridesRoundTrip`, `TestGetAncestorOverrides`.
  - Any mock/fake `TaskRepository` implementation in the test harness — stub the new method.
- **Modified interfaces:** `TaskRepository` gains `GetAncestorOverrides`.
- **Schema migrations:** 012 (up + down).
- **No environment variables, no new dependencies.**
