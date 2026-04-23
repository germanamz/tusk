# Phase 2 — Service & Events: Move, Resequence, create default, task_moved event

Initiative: v0.13 Sibling Ordering
Design spec: `docs/superpowers/specs/2026-04-23-sibling-ordering-design.md`

## Prerequisites

Phase 1 (`phase-1-foundation.md`) must be merged first.

## Inherits From

At the start of this phase you can rely on everything Phase 1 introduced:

- `domain.Task.Order *float64` and `domain.TaskUpdate.Order **float64` exist and round-trip through the SQLite repo.
- `domain.ErrCyclicParent` and `domain.ErrOrderGapExhausted` are defined in `domain/errors.go`.
- Migration `011_task_order` has added the `"order"` column, backfilled every existing row with dense per-parent integers, and created the `idx_tasks_parent_order` index.
- `TaskRepository` exposes `NextOrder(ctx, parentID) (float64, error)`, `FirstOrder(ctx, parentID) (float64, error)`, and `NeighborOrders(ctx, parentID, pivot) (prev, next *float64, err error)` — all three safe to call inside an open transaction.
- `GetChildren` and `GetDescendants` already sort by `"order" ASC NULLS LAST, created_at ASC, id ASC`.
- No service method yet consumes any of the above — Phase 1 was persistence-only.

No CLI or MCP surface exists for ordering yet; that ships in Phase 3.

## Goal

Make the service layer the single owner of sibling-ordering logic: new tasks auto-receive a sensible `order` default on create, `TaskService.Move` and `TaskService.Resequence` exist and carry their own transactional guarantees, and every structural move is recorded in the event log as a dedicated `EventTaskMoved`. Absolute `order=` writes via `TaskUpdate.Order` continue to flow through the existing `task_modified` event so the diff path has no new branches.

**User-visible behavior after this phase.** The CLI surface is unchanged — there is no `tusk task move` command yet (Phase 3) and no inline `order=` parser (Phase 3). Go-library consumers gain three new service methods (`TaskService.Move`, `TaskService.Resequence`, plus the auto-defaulted `Create`). From a shell user's perspective, the only observable change is that `tusk task create` now persists an `order` value (max-of-siblings + 1) automatically instead of leaving it NULL, so newly-created tasks land at the end of their sibling group in `tree` views — this is exactly the behavior the feature description promises and the one Phase 1's preserved-behavior section documented as pending.

## Tasks

### Task 2.1 — Event type, payload, and midpoint helper

Edit `domain/event_task.go`:

- Add the new event type constant alongside the existing ones:

  ```go
  EventTaskMoved EventType = "task_moved"
  ```
- Add the payload type and its `EventKind` method next to `TaskCreatedPayload` / `TaskModifiedPayload` / etc.:

  ```go
  type TaskMovedPayload struct {
      Kind        EventType  `json:"kind"`
      OldParentID *uuid.UUID `json:"old_parent_id,omitempty"`
      NewParentID *uuid.UUID `json:"new_parent_id,omitempty"`
      OldOrder    *float64   `json:"old_order,omitempty"`
      NewOrder    *float64   `json:"new_order,omitempty"`
  }

  func (TaskMovedPayload) EventKind() EventType { return EventTaskMoved }
  ```

  The payload uses pointers so the JSON round-trip distinguishes "no value" from "zero". `Kind` is redundant with `EventKind()` but every other task payload carries it — stay consistent.

Create `service/task_move_math.go` (new file) holding a small, pure helper:

```go
package service

import (
    "math"

    "github.com/germanamz/tusk/domain"
)

// computeMidpoint returns the arithmetic mean of low and high. It returns
// domain.ErrOrderGapExhausted when the mean is indistinguishable from either
// endpoint under float64 comparison — that is, when the gap has collapsed
// below the representation's resolution at that magnitude.
func computeMidpoint(low, high float64) (float64, error) {
    if !(low < high) {
        return 0, domain.ErrOrderGapExhausted
    }
    mid := low + (high-low)/2
    if mid == low || mid == high {
        return 0, domain.ErrOrderGapExhausted
    }
    if math.IsNaN(mid) || math.IsInf(mid, 0) {
        return 0, domain.ErrOrderGapExhausted
    }
    return mid, nil
}
```

Create `service/task_move_math_test.go` covering:
- `(1, 2)` → `1.5`, no error.
- `(0, 1)` → `0.5`, no error.
- Reversed pair `(2, 1)` → `ErrOrderGapExhausted`.
- Equal pair `(1, 1)` → `ErrOrderGapExhausted`.
- Endpoint-equality case: craft `low = 1.0`, `high = math.Nextafter(1.0, 2.0)` — midpoint equals one endpoint → `ErrOrderGapExhausted`.

Acceptance: `go test ./domain/... ./service/... -run "EventTask|Midpoint"` passes. `go build ./...` passes.

### Task 2.2 — `TaskService.Move`

Add a new method to `TaskService` in `service/task.go` (or a dedicated `service/task_move.go` if the existing file is already large — follow whatever split the codebase uses for `task_claim.go` / `task_routing.go`). Exact signature:

```go
type MovePosition int

const (
    MovePositionBefore MovePosition = iota + 1
    MovePositionAfter
    MovePositionFirst
    MovePositionLast
)

type MoveRequest struct {
    TaskID   uuid.UUID
    Version  int
    Position MovePosition
    TargetID *uuid.UUID
    ParentID **uuid.UUID // nil = keep current; *nil = root; *&id = specific parent
    ActorID  *string
}

func (s *TaskService) Move(ctx context.Context, req MoveRequest) (*domain.Task, error)
```

Implementation contract:

1. Open a transaction using the existing `tx.Service` helper (`service/tx.go`) so every read and write in the method shares snapshot isolation.
2. Load the subject task by `TaskID`. On miss, return `domain.ErrNotFound`.
3. Validate `req`:
   - `Before` / `After` require non-nil `TargetID`; `First` / `Last` reject a non-nil `TargetID`.
   - `ParentID` is only honored for `First` / `Last`; on `Before` / `After` ignore it (do not error — caller-convenience).
   - Invalid `Position` value → `fmt.Errorf("invalid move position: %d", req.Position)`.
4. Determine the **effective new parent** (`*uuid.UUID`, nil = root):
   - `Before` / `After`: load the target, new parent = `target.ParentID`. On target miss, `domain.ErrNotFound`.
   - `First` / `Last`:
     - `req.ParentID == nil` → new parent = `subject.ParentID`.
     - `req.ParentID != nil && *req.ParentID == nil` → new parent = nil (root).
     - otherwise → new parent = `*req.ParentID`; validate the parent exists via `taskRepo.GetByID`. On miss, return `domain.ErrNotFound`.
5. **Cycle guard**: if the new parent is not nil, ensure it is not the subject itself and not a descendant of the subject. Reuse `taskRepo.GetDescendants(subject.ID)` and build a `map[uuid.UUID]struct{}` for O(1) lookup. On cycle, return `domain.ErrCyclicParent`.
6. Compute the new `order` value:
   - `Before`:
     - Let `pivot = *target.Order` (fall back to `1.0` if `target.Order == nil`; a NULL-order target predates backfill — rare but possible if somebody ran `order=` clear).
     - `prev, _, err := taskRepo.NeighborOrders(ctx, target.ParentID, pivot)`; if `err`, return.
     - If `prev == nil` → `newOrder = pivot - 1.0`.
     - Else → `newOrder, err = computeMidpoint(*prev, pivot)`; if `err`, wrap with `fmt.Errorf("%w: parent %s", err, formatParentShortID(target.ParentID))` and return.
   - `After`:
     - Symmetric with `next`.
   - `First`:
     - `newOrder, err := taskRepo.FirstOrder(ctx, newParentID)`; if `err`, return.
   - `Last`:
     - `newOrder, err := taskRepo.NextOrder(ctx, newParentID)`; if `err`, return.
7. Optimistic update. Execute `UPDATE tasks SET parent_id = ?, "order" = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?`. Use the existing `sqlite/task.go` update helpers if they already accept a targeted column set; otherwise add a narrow `TaskRepo.UpdateOrderAndParent(ctx, id, parentID, order, fromVersion, updatedAt) (newVersion int, err error)` helper that runs this single statement. Map `rows_affected == 0` to `domain.ErrConflict`.
8. Emit the event. Build a `TaskMovedPayload` capturing `OldParentID = subject.ParentID`, `OldOrder = subject.Order`, `NewParentID = effectiveNewParent`, `NewOrder = &newOrder`. Hand it to the existing event publisher used by `TaskService.Update` (the one that writes into the `events` table inside the same transaction). Confirm the publisher honors the actor ID threading (`req.ActorID`) — every other `task_*` event already does.
9. Commit. Re-read the task (fresh `version`, `parent_id`, `order`) and return it.

`formatParentShortID(id *uuid.UUID) string` is a local helper: returns `"root"` if nil, otherwise the 8-char short ID derived from the UUID (reuse whatever helper the CLI uses for short ID derivation — search `grep 'ShortID(' domain/` and `grep 'shortID(' sqlite/`).

Create `service/task_move_test.go` covering:
- Before same-parent, midpoint placement; event emitted with correct `OldOrder`, `NewOrder`, unchanged parents.
- After same-parent, midpoint placement.
- Before cross-parent (target belongs to a different parent) — subject is re-parented; event records old and new parent.
- First with `ParentID == nil` (keep current) on a subject that has two siblings; new order is `min - 1.0`.
- First with `*ParentID == nil` (move to root) from a nested position.
- Last with explicit parent — new order is `max + 1.0`.
- Cycle rejection: attempt to move parent under its own child; assert `ErrCyclicParent`, no DB mutation.
- Underflow: fixture two siblings at `1.0` and `math.Nextafter(1.0, 2.0)`, attempt `Before` on the second; assert error wraps `ErrOrderGapExhausted` and the error message contains the parent's short ID.
- Version mismatch: call `Move` with a stale version; assert `ErrConflict`.
- ErrNotFound when subject or target does not exist.

Acceptance: `go test ./service/... -run Move` passes.

### Task 2.3 — `TaskService.Resequence`

Add to the same file (or a peer `service/task_resequence.go`):

```go
// Resequence rewrites every sibling under parentID to dense integer orders
// (1.0, 2.0, 3.0, ...), preserving their current (order ASC NULLS LAST,
// created_at ASC) relative positions. parentID == nil scopes to root.
// Returns the count of rows whose order actually changed.
func (s *TaskService) Resequence(ctx context.Context, parentID *uuid.UUID, actorID *string) (rewritten int, err error)
```

Contract:

1. Open a tx.
2. Load the sibling group with `taskRepo.GetChildren(parentID)` — Phase 1 already sorts it correctly.
3. For each task in order, compute the new value `seq = 1.0, 2.0, 3.0, ...`. If `task.Order != nil && *task.Order == seq`, skip. Otherwise:
   - Update the row's `order` via `TaskRepo.UpdateOrderAndParent` (or equivalent) keeping `parent_id` unchanged, bumping `version`.
   - Emit a `task_modified` event with `Changes["order"] = { old: *task.Order, new: seq }`. Reuse the existing diff-event emission helper — do **not** emit `task_moved`; `Resequence` never changes a parent.
   - Increment `rewritten`.
4. Commit, return `rewritten`.

Edge cases:
- Empty group → return `0, nil` with no tx writes.
- Already-sequential group (`[1.0, 2.0, 3.0]`) → return `0, nil` with no tx writes (every element is skipped).
- A task with `order == nil` → always counts as a change (`seq` ≠ nil).

Tests in `service/task_resequence_test.go`:
- Empty group no-op.
- `[3, 1, 2]` → rewritten to `[1 (was 3), 2 (was 1), 3 (was 2)]` relative to the `(order, created_at)` sort, with three `task_modified` events.
- `[1, 2, 3]` → zero rewrites, zero events.
- `[nil, 2, 3]` (one NULL) → the NULL task sorts last, becomes `3.0`; the other two shift to `1.0, 2.0`; three events.
- ActorID threads into the events.

Acceptance: `go test ./service/... -run Resequence` passes.

### Task 2.4 — Create default for `Order`

Edit `TaskService.Create` in `service/task.go`. Inside the existing create transaction, immediately before the repo write (and after any taxonomy / parent validation), add:

```go
if task.Order == nil {
    next, err := s.repos.Tasks.NextOrder(ctx, task.ParentID)
    if err != nil {
        return err
    }
    task.Order = &next
}
```

Place this block after every validator has run but before `s.repos.Tasks.Create(ctx, task)`. The auto-assigned value is written through the normal insert path, so the resulting `task_created` event already captures it (the event payload snapshots the full task).

Do **not** change `TaskService.Update` — the existing field-diff path handles `TaskUpdate.Order` as a plain field now that the sqlite repo round-trips it. Confirm by adding a test in `service/task_test.go` (or the closest existing update test file):

```go
// test outline
t.Run("update order absolute", func(t *testing.T) {
    // create task, default order = 1.0
    // call Update with TaskUpdate{Order: doublePtr(ptrFloat(5.5))}
    // assert fetched task has Order == 5.5, version bumped, exactly one task_modified event with Changes["order"] = {old: 1.0, new: 5.5}
})

t.Run("update order clear", func(t *testing.T) {
    // create task, default order = 1.0
    // call Update with TaskUpdate{Order: doublePtr(nil)} (**nil)
    // assert fetched task has Order == nil, one task_modified event with Changes["order"] = {old: 1.0, new: nil}
})
```

Note: the `**float64` "`doublePtr(nil)` means clear" convention mirrors how `Description`, `Level`, etc. behave — if the current diff helper in `service/task.go` does not yet treat `**T` of `*nil` as "clear", extend the helper to handle the `Order` field explicitly. Search for `diffTaskFields` in `service/task.go` and add the `Order` branch; the pattern for `Level` (added in the Task Level Taxonomy initiative) is the template to copy.

Tests in `service/task_test.go`:
- `TestTaskService_Create_AssignsOrder_Default_EmptyGroup`: root-level create with no order, assert `Order == 1.0`.
- `TestTaskService_Create_AssignsOrder_Default_NonEmpty`: after creating two children under one parent with default order, create a third; assert its order is `3.0`.
- `TestTaskService_Create_RespectsCallerOrder`: call Create with `Order = ptrFloat(2.5)` on an empty group; assert persisted order is `2.5` (no defaulting).
- `TestTaskService_Update_Order_Absolute` and `_Clear` as sketched above.

Acceptance: `go test ./service/...` passes. `go test ./... -race` passes.

## Preserved User-Visible Behavior

All behavior guaranteed at the end of Phase 1 continues to hold. In addition:

- `tusk task create` (with or without `--output json`) produces a task whose new `order` field is non-null and strictly greater than any existing sibling — previously it was NULL after Phase 1 merged.
- `tusk task tree` places newly-created tasks at the end of their sibling group deterministically.
- `tusk task get <id>` exposes `order` in JSON output with the new default value. Text renderings are unchanged (the text renderer does not currently surface `order`; the Phase 3 CLI work will add a dedicated line to `tusk task get` output).
- `tusk task list` / `next` / `available` / `pop` remain urgency-sorted.
- No new CLI command, no new MCP tool, no new filter field; invoking `tusk task move …` still exits with the usual "unknown command" error.

## Changes Introduced

**New files:**
- `service/task_move_math.go`
- `service/task_move_math_test.go`
- `service/task_move.go` (if the move method is split out; optional — may live in `service/task.go`)
- `service/task_move_test.go`
- `service/task_resequence.go` (optional — may live in `service/task.go`)
- `service/task_resequence_test.go`

**Modified files:**
- `domain/event_task.go` — `EventTaskMoved`, `TaskMovedPayload`.
- `service/task.go` — `Create` auto-assigns `Order`, `diffTaskFields` handles `Order`.
- `service/task_test.go` — new create/update tests for `Order`.
- `sqlite/task.go` — possibly a new narrow `UpdateOrderAndParent` helper (only if the existing generic update doesn't already take an explicit column set).
- `repository/task.go` — matching interface signature if the helper was added.

**Bridge code:** none.

**Dependencies added:** none.

**Behavioral changes:** `TaskService.Create` now writes a non-null `order` on every task. This is a forward-only change — there is no flag to disable it. The post-Phase-1 preserved-behavior note that called out NULL-orders-on-new-tasks as acceptable becomes moot here.
