# Design Spec — Sibling Ordering (v0.13)

Status: proposed
Date: 2026-04-23
Initiative: ROADMAP.md § v0.13 — Sibling Ordering
Scope: introduce a per-task fractional `order` field plus a `tusk task move` command so hierarchical views have a stable document-position sort that is independent of urgency.

## 1. Goal

Give the roadmap self-host and every other tree-shaped project a meaningful way to say "this goes before that" without coupling position to urgency or shoehorning it into a UDA. Flat views (`list`, `next`, `available`, `pop`) continue to sort by urgency; hierarchical views (`tree`, `list parent=…`, `list tree=…`) switch to ordering by `order` with `created_at ASC` as the tiebreak.

Requirements from the ROADMAP initiative:

- `order` is a nullable `DOUBLE` column on `tasks`; new tasks default to `max(sibling.order) + 1.0` (or `1.0` if first).
- `tusk task create` and `tusk task modify` accept an inline `order=<float>`.
- Tree-shaped views sort by `order`; flat views keep sorting by urgency.
- `--sort=order|urgency|created|priority|due` is an override on list/tree.
- `tusk task move` supports `--before`, `--after`, `--first`, `--last`, atomically re-parents when the target has a different parent, and exposes `--resequence <parent>` as an operator tool.
- An MCP tool `tusk_task_move` mirrors the CLI.
- `order` round-trips through JSON and Markdown export/import.

## 2. Non-Goals (Deferred)

- Automatic resequence on gap underflow. Move returns `ErrOrderGapExhausted`; the user runs `--resequence` manually.
- Arithmetic delta modifiers on `order` (`+order=`, `-order=`). Absolute writes only.
- Bulk move (move multiple IDs in one call). One task per invocation.
- Moving a task relative to a non-sibling (`--before <target>` where the target has a different parent implicitly re-parents; no "insert under X at position N" shorthand beyond `--first --parent X` / `--last --parent X`).
- Pre-v0.13-migration tasks keeping sparse order values. The migration backfills dense integers.

## 3. Data Model

### 3.1 Task

`domain.Task` gains:

```go
Order *float64 // nullable; NULL sorts last in hierarchical views
```

`domain.TaskUpdate` gains:

```go
Order **float64 // nil = no change, *nil = clear to NULL, *&v = set to v
```

JSON / text / CSV field name is `"order"`. JSON serialization does **not** apply `omitempty` to `order` because `null` is meaningful (explicitly cleared) and distinct from "inherit the default".

### 3.2 Errors

```go
var (
    ErrCyclicParent        = errors.New("task move would create a parent cycle")
    ErrOrderGapExhausted   = errors.New("no float64 midpoint remains between neighbors; run `tusk task move --resequence <parent>`")
)
```

Both are sentinels — callers match with `errors.Is`. `ErrOrderGapExhausted` wraps a formatted message that names the parent short ID so the resequence command can be copy-pasted.

### 3.3 Database

Migration `011_task_order`:

```sql
-- up
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

```sql
-- down
DROP INDEX IF EXISTS idx_tasks_parent_order;
ALTER TABLE tasks DROP COLUMN "order";
```

The `modernc.org/sqlite` driver pinned in `go.mod` ships a SQLite build that supports both `ROW_NUMBER()` window functions and `ALTER TABLE DROP COLUMN`. No build-tag or driver-version changes required.

Post-migration invariant: every row has a non-NULL `order`. NULL reappears only when a caller explicitly clears via `order=` on modify; sort clauses treat NULL as "sort last" so this edge behaves deterministically.

The literal column name `"order"` is always quoted in SQL to avoid colliding with the reserved word. The `taskColumns` string constant in `sqlite/task.go` carries the quoted form.

### 3.4 Indexes

The new composite index `idx_tasks_parent_order` supports:

- Hierarchical `GetChildren(parentID)` and `GetDescendants(rootID)` queries that end with `ORDER BY "order", created_at`.
- Fast `SELECT MAX("order") FROM tasks WHERE parent_id = ?` for the Create default and `--last` placement.
- Fast `SELECT MIN("order") FROM tasks WHERE parent_id = ?` for `--first`.

No other schema changes.

## 4. Service Layer

### 4.1 Create default

`TaskService.Create` computes `Order` inside the existing create transaction when the caller leaves it nil:

```go
if task.Order == nil {
    next, err := repo.NextOrder(ctx, task.ParentID)
    if err != nil {
        return err
    }
    task.Order = &next
}
```

`TaskRepository.NextOrder(ctx, parentID *uuid.UUID) (float64, error)` returns `max(sibling.order) + 1.0`, or `1.0` when the group is empty. `parentID == nil` scopes to root-level siblings. The query runs inside the caller's transaction.

Caller-supplied `Order` values pass through verbatim — no midpoint math on Create. If a caller creates two tasks with the same order, tiebreak falls to `created_at ASC`.

### 4.2 Absolute modify

`TaskService.Update` with `TaskUpdate.Order` set writes the literal value (or clears to NULL on `**nil`). No sibling-group lookup, no midpoint. This path is the "I know the float I want" escape hatch; normal users drive position through `tusk task move`.

An absolute write emits `task_modified` with `Changes["order"] = { old, new }` — same shape as every other field edit.

### 4.3 Move

Signature:

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
    TargetID *uuid.UUID   // required for Before/After; ignored by First/Last
    ParentID **uuid.UUID  // First/Last only; nil = keep current parent,
                          //                  *nil = move to root,
                          //                  *&id = move under id
    ActorID  *string
}

func (s *TaskService) Move(ctx context.Context, req MoveRequest) (*domain.Task, error)
```

Behavior, in order:

1. Load the subject task (by ID). Return `ErrNotFound` on miss.
2. Determine the **effective new parent**:
   - `Before` / `After`: load the target task; new parent = `target.ParentID`.
   - `First` / `Last`: use `req.ParentID` per the tristate above; `nil` keeps the subject's current parent.
3. Cycle guard: if the new parent equals the subject's ID, or the new parent is in `GetDescendants(subject.ID)`, return `ErrCyclicParent`.
4. Compute the new `order` value:
   - `Before`: load the target's predecessor (largest `order < target.order` within the same parent). If no predecessor exists, `new = target.order - 1.0`. Otherwise `new = (predecessor.order + target.order) / 2`.
   - `After`: mirror, using the successor. `new = target.order + 1.0` if no successor, else midpoint.
   - `First`: `new = min(sibling.order) - 1.0`, or `1.0` if the group is empty.
   - `Last`: `new = max(sibling.order) + 1.0`, or `1.0` if empty.
5. Underflow guard: if `Before`/`After` produced a midpoint that equals either neighbor under `float64` comparison, return `ErrOrderGapExhausted` naming the parent short ID.
6. Optimistic update: single `UPDATE tasks SET parent_id = ?, "order" = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?`. If `rows_affected == 0`, return `ErrConflict`.
7. Emit `EventTaskMoved` with the before/after snapshot (both parent and order fields included even when one didn't change — the payload is a move record, not a diff).
8. Return the fresh task.

All reads and the write happen inside a single SQLite transaction opened via the existing `tx.Service` helper. The service does not call `TaskService.Update`; `Move` owns its own repo writes so the event shape stays dedicated.

### 4.4 Resequence

```go
func (s *TaskService) Resequence(ctx context.Context, parentID *uuid.UUID, actorID *string) error
```

Inside one transaction:

1. Load the sibling group ordered by `("order" ASC NULLS LAST, created_at ASC, id ASC)`.
2. Rewrite each row to `1.0, 2.0, 3.0, ...`; bump every row's `version`.
3. Emit one `task_modified` event per row whose order actually changed (`Changes["order"] = { old, new }`). Parent unchanged, so no `task_moved` event.

Idempotent: running `Resequence` twice on a group already in `1.0, 2.0, 3.0, ...` is a no-op and emits zero events. Callers that want a dry run use a separate future flag; not in scope for v0.13.

### 4.5 Guard helpers

Both `Move` and the `Update` path for `Order` share a small internal helper — `computeMidpoint(low, high float64) (float64, error)` — that returns `ErrOrderGapExhausted` when the midpoint is indistinguishable from either endpoint. Keeping it in one place means the underflow contract has one definition.

## 5. Event Log

New event:

```go
const EventTaskMoved EventType = "task_moved"

type TaskMovedPayload struct {
    Kind        EventType `json:"kind"`
    OldParentID *uuid.UUID `json:"old_parent_id,omitempty"`
    NewParentID *uuid.UUID `json:"new_parent_id,omitempty"`
    OldOrder    *float64   `json:"old_order,omitempty"`
    NewOrder    *float64   `json:"new_order,omitempty"`
}

func (TaskMovedPayload) EventKind() EventType { return EventTaskMoved }
```

Emitted only by `TaskService.Move`. Absolute `order=<float>` writes via `task modify`, and `Resequence`, continue to flow through `task_modified`. Undo (v0.16) plugs into the move event by swapping old/new pairs — no diff-merge logic required.

Bounded-retention pruning handles move events the same way it handles every other event type.

## 6. CLI

### 6.1 Inline `order=` on create / modify

Pure inline — no new cobra flags. Handled by the existing inline parser:

```
tusk task create "Ship onboarding" project=backend order=2.5
tusk task modify a3f8b2c1 order=4.0        # absolute write
tusk task modify a3f8b2c1 order=           # clear to NULL (sinks to end)
```

The lexer already tokenizes `order=<value>`. A new entry in the task-field registry maps `order` to `TaskUpdate.Order`. `uda.order` stays a separate, free-form UDA — no collision.

### 6.2 `tusk task move`

```
tusk task move <id> --before <target>
tusk task move <id> --after  <target>
tusk task move <id> --first [--parent <id>|--parent root]
tusk task move <id> --last  [--parent <id>|--parent root]
tusk task move --resequence <parent-id>
```

Cobra layout:

- `tusk task move` takes one positional (`<id>`), except `--resequence` which takes its positional (`<parent-id>`) instead.
- Flag mutual exclusion (enforced in `PreRunE`):
  - Exactly one of `--before / --after / --first / --last / --resequence`.
  - `--parent` requires `--first` or `--last`.
  - `--resequence` accepts no other flags.
- `--parent root` is a sentinel string that resolves to "no parent"; any other value is resolved as a short ID or UUID.
- `--version <int>` optional flag for callers that want to hand-gate optimistic locking; default reads the task once and uses its current version.
- `--output json` renders the moved task in the standard task-JSON shape. Text output shows a one-line confirmation plus the new parent / order values.

Error surface: `ErrCyclicParent`, `ErrOrderGapExhausted`, `ErrNotFound`, `ErrConflict` map to exit code 1 with the standard error prefix used elsewhere in tusk (`errors.go`).

### 6.3 `--sort` override on list / tree

New flag on `tusk task list` and `tusk task tree`:

```
--sort=order|urgency|created|priority|due
```

- Defaults are unchanged: `tree` → `order`, `list` (and `next` / `available` / `pop`) → `urgency`.
- The flag is renderer-scoped: the service returns filter matches in their native order, and the rendering path applies the chosen sort. Urgency sort continues to run through `UrgencyEngine.ScoreAndSort`; other sorts use `sort.SliceStable` on the appropriate field pair (`(order, created_at)`, `(created_at)`, `(priority, urgency)`, `(due_at, urgency)`).
- Tree rendering relies on the SQL `ORDER BY` clause (see §7) for the default `order` sort so no client-side re-sort is needed on the happy path; `--sort=<other>` triggers the in-process stable sort before the tree is built.

## 7. Repository & Sort Clauses

- `TaskRepository.GetChildren(parentID)` gains `ORDER BY "order" ASC NULLS LAST, created_at ASC, id ASC`.
- `TaskRepository.GetDescendants(rootID)` gains the same clause on the outer `SELECT` of the recursive CTE so descendants stream in tree-friendly order.
- `TaskRepository.List(filter)` keeps its existing behavior — filter results come back unsorted so the service layer (or renderer) decides.
- New helpers:
  - `NextOrder(ctx, parentID *uuid.UUID) (float64, error)` — `max + 1.0` (see §4.1).
  - `FirstOrder(ctx, parentID *uuid.UUID) (float64, error)` — `min - 1.0`, or `1.0` on empty.
  - `NeighborOrders(ctx, parentID *uuid.UUID, pivot float64) (prev, next *float64, err error)` — single query returning the closest `order < pivot` and `order > pivot` within the same parent group. Used by `Move` for `Before` / `After` midpoint computation.

All helpers run inside the caller's transaction so Move's snapshot stays consistent.

## 8. Filter Grammar

Add `order` to the task-field registry in `filter/`:

- `order=2.5` — exact match.
- `order=2..5` — range match; endpoints inclusive, floats allowed.
- `order=` (empty) — matches tasks with NULL order.

This is a pure registry entry; the lexer already understands `key=value`, ranges, and empty values. Fits the broader "inline syntax works in create, modify, and filter" principle — no reason to withhold it.

No boolean-operator changes. No `+order` / `-order` modifier semantics (arithmetic deltas deferred per §2).

## 9. MCP

### 9.1 Tools

- `tusk_task_move`:
  - Input:
    - `task_id` (string, required) — short ID or UUID.
    - `position` (string, required) — `"before" | "after" | "first" | "last"`.
    - `target_id` (string, required when `position` is `before` or `after`; rejected otherwise).
    - `parent_id` (string or null, allowed only when `position` is `first` or `last`):
      - Key absent → keep the subject's current parent.
      - `null` → move to root.
      - `"<id>"` → move under that parent.
    - `version` (integer, required) — optimistic locking.
    - `player_id` (string, optional).
  - Output: the moved task in the standard task-response shape (including fresh `version`).
- `tusk_task_resequence`:
  - Input:
    - `parent_id` (string or null, required) — short ID / UUID, or `null` for root.
    - `player_id` (string, optional).
  - Output: `{ "rewritten": <count> }`.

Both tools validate input shape at the MCP layer (e.g., `position=before` requires `target_id`, `parent_id` rejected) and delegate to the service's `MoveRequest` / `Resequence`.

### 9.2 Blocked fields

The v0.12 blocked-fields mechanism keys off the tool name + field path. `order` and `parent_id` on `tusk_task_modify` are the knobs consumers hide when they want agents to create and comment but not reorder. `tusk_task_move` is blockable as a whole tool via the existing `visibility.tools` config. No new config shape.

### 9.3 Existing create / modify tools

`tusk_task_create` and `tusk_task_modify` auto-inherit `order` from the standard field registry used by their JSON schemas. Input type: optional number, nullable on modify.

## 10. Export / Import

### 10.1 JSON (bidirectional)

- Export: `order` emitted as `"order": <number|null>`. Preserves clears.
- Import: `order` read verbatim when present; if the key is absent, the importer applies the same `NextOrder` default as `TaskService.Create` so the loaded task slots into place among the existing siblings.
- Round-trip: exporting an empty workspace, re-importing, re-exporting yields byte-identical JSON. The E2E suite's existing round-trip harness is the authoritative check.

### 10.2 Markdown (bidirectional)

- Export: walks each sibling group in `order` sequence and emits bullets in that order. The markdown format does not carry the raw float — document position is the carrier. Comment header records the exporter version and export timestamp (unchanged).
- Import: assigns `order = 1.0, 2.0, 3.0, ...` to bullets in document order within each parent. A hand-written plan authored with no awareness of `order` still produces a clean, predictable tree.

### 10.3 CSV (export only)

- Add an `order` column after `priority` in the header and row tuples. Values are raw floats; NULL becomes an empty cell. No import path — CSV remains export-only per ROADMAP.

## 11. Testing

### 11.1 Unit

- `domain/taxonomy_test.go`-style table tests for the midpoint helper (`computeMidpoint`): verifies underflow detection, exact mid for `(1, 2)` = `1.5`, endpoint-equality rejection.
- `service/task_move_test.go` covers every `MoveRequest` shape: before/after same-parent, before/after cross-parent, first/last with and without `--parent`, cycle rejection, underflow rejection, version conflict, `ErrNotFound` on unknown target, `ErrNotFound` on unknown subject.
- `service/task_resequence_test.go` covers: empty group no-op, already-sequential no-op, scattered floats rewritten to integers, NULL entries placed last and rewritten, event emission count matches rewritten rows.
- `sqlite/task_order_test.go` covers the migration backfill under realistic fixtures (root-only tasks, deep hierarchies, tasks with duplicate `created_at`).

### 11.2 E2E

`tests/e2e/` scenario `sibling_ordering` runs the standard 4-way matrix (2 DB modes × 2 output formats):

- Create three children under a parent, assert `tree` returns them in create order (`1.0, 2.0, 3.0`).
- `move child-2 --before child-1`, assert new `tree` order is `child-2, child-1, child-3` in both text and JSON.
- `move child-3 --first`, assert it's now the first child.
- Cross-parent: `move child-2 --after root-other`, assert re-parenting applied.
- `order=` clear on a fourth child sinks it to the end.
- `--resequence` on a group with a manually-written `1.5` value rewrites to `1.0, 2.0, 3.0, 4.0`.
- Cycle rejection: attempt `move parent --under child`, assert `ErrCyclicParent` exit.
- Underflow rejection: hand-craft two tasks at `1.0` and `1.0 + 2^-52`, attempt `move X --before Y`, assert `ErrOrderGapExhausted` with the parent ID in the message.

Export/import round-trip (JSON and Markdown) adds one scenario each, piggybacking on the existing Data Portability harness.

## 12. Phasing

Four phases, each shippable on its own. Phase `n+1` depends on phase `n`.

1. **Foundation** — migration 011 (column + backfill + index), `Task.Order` / `TaskUpdate.Order`, sentinel errors, sqlite scan/write, sort-clause updates on `GetChildren` / `GetDescendants`, `NextOrder` / `FirstOrder` / `NeighborOrders` repo helpers. Tests: sqlite migration, sqlite read/write round-trip.
2. **Service & CLI core** — `TaskService.Move`, `TaskService.Resequence`, `computeMidpoint` helper, inline `order=` in the task-field registry (flows through Create and Update automatically), `tusk task move` cobra command, `--sort` flag on list/tree. Tests: service unit tests, CLI snapshot tests.
3. **MCP + events + filter** — `EventTaskMoved` type and payload, emission from `Move`, `tusk_task_move` and `tusk_task_resequence` tools, filter-grammar `order=` entry. Tests: event emission, MCP tool round-trip.
4. **Export/import & E2E** — JSON export/import wiring, Markdown export/import wiring, CSV `order` column, full E2E scenario, ROADMAP ticks, and the v0.13-status doc landing at milestone completion only.

Bridge code between phases is minimal: phase 1 leaves `task.Order` observable but unmoved; phase 2 lights up move; phase 3 adds MCP + event plumbing on top of existing service; phase 4 is pure additive.
