# Task Queue Design Spec

**Initiative:** Task Queue (v0.7)
**Date:** 2026-04-07

---

## Overview

Atomic pop operation and available-task listing for efficient agent orchestration. Builds on player claiming (v0.7) and urgency scoring (v0.6).

Two stories:
1. **Available tasks** — `tusk available` CLI + `tusk_task_available` MCP tool
2. **`tusk pop`** — atomically find highest-urgency available task, claim it, start it, return it

---

## Key Design Decisions

### "Not blocked" semantics

A task is "blocked" only if it has incomplete blockers — tasks with `status NOT IN ('completed', 'deleted')` on the source side of a `blocks` relation. A completed blocker no longer prevents the target from being actionable.

**Implementation:** New repository method `CountBlockedByIncompleteTasks` that JOINs `relations` with `tasks` on `source_id`, filtering to incomplete blockers. Same signature as `CountBlockedByTasks`.

### Atomicity of `pop`

Optimistic retry loop — consistent with the project's concurrency model:
1. Call `Available` to get the urgency-ranked list
2. Attempt `Claim` + `Start` on the top task
3. If `ErrTaskClaimed` or `ErrConflict`, try the next task
4. Return the single successfully claimed+started task
5. If list exhausted, return `ErrNoAvailableTasks`

No new transactional repo methods needed. Urgency scoring stays in Go.

### Actionable status scope

`available` and `pop` filter to `status:pending OR status:active` — same as `tusk list` defaults. Active-but-unclaimed tasks are legitimate candidates (e.g., a player released a task mid-work).

### Pop claims + starts

`pop` both claims and transitions to `active` in one operation. Players who want finer control use `claim` and `start` separately.

---

## Repository Layer

### New method on `RelationRepository`

```go
CountBlockedByIncompleteTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
```

**SQLite implementation:** Same structure as `countRelationsByTasks` but JOINs `relations.source_id` with `tasks.id` and adds `WHERE tasks.status NOT IN ('completed', 'deleted')`. Returns count of incomplete blockers per task.

No other repository changes needed.

---

## Domain Layer

### New sentinel error

```go
var ErrNoAvailableTasks = errors.New("no available tasks")
```

Added to `internal/domain/errors.go`.

---

## Service Layer

### `TaskService.Available(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error)`

1. Build base filter: `(status:pending OR status:active) AND unclaimed:true`
2. If caller provides additional filters, AND them onto the base
3. Call existing `List` logic (repo query + urgency scoring + sorting)
4. Post-filter: call `CountBlockedByIncompleteTasks` on the result set, remove tasks with count > 0
5. Return the filtered, urgency-sorted list

Player auto-registration happens at the CLI/MCP layer before calling this method.

### `TaskService.Pop(ctx context.Context, playerID string, filter domain.FilterExpr) (*domain.Task, error)`

1. Call `Available(ctx, filter)` to get the ranked list
2. If list is empty, return `ErrNoAvailableTasks`
3. Iterate from top (retry loop for race conditions only — claims exactly one task):
   a. Attempt `Claim(ctx, task.ShortID, playerID, task.Version)`
   b. If `ErrTaskClaimed` or `ErrConflict` — skip to next task
   c. On successful claim, call `Start(ctx, task.ShortID, task.Version+1, playerID)`
   d. If Start fails with `ErrConflict` — release the claim and skip to next task
   e. Return the started task
4. If all tasks exhausted by contention, return `ErrNoAvailableTasks`

---

## CLI Layer

### `tusk available [filters...] --player <id>`

- Requires `--player` flag (auto-registers player)
- Accepts optional filters (same syntax as `tusk list`)
- Calls `TaskService.Available(ctx, filter)`
- Renders using the same table renderer as `tusk list`
- Supports `--json` output format

### `tusk pop [filters...] --player <id>`

- Requires `--player` flag (auto-registers player)
- Accepts optional filters to narrow the queue
- Calls `TaskService.Pop(ctx, playerID, filter)`
- Renders the single task using the same detail format as `tusk info`
- `ErrNoAvailableTasks` → "No available tasks" message, exit code 1
- Supports `--json` output format

---

## MCP Layer

### `tusk_task_available`

- **Parameters:** `player_id` (required string), `filter` (optional string), plus individual filter fields matching `tusk_task_list`
- Auto-registers player as `"agent"` type
- Calls `TaskService.Available(ctx, filter)`
- Returns array of `taskResponse` objects (same shape as `tusk_task_list`)
- Tool group: `"task"`

### `tusk_task_pop`

- **Parameters:** `player_id` (required string), `filter` (optional string)
- Auto-registers player as `"agent"` type
- Calls `TaskService.Pop(ctx, playerID, filter)`
- Returns single `taskResponse` with tags
- `ErrNoAvailableTasks` → text result: "No available tasks matching the given filters"
- Tool group: `"task"`

---

## E2E Tests

### CLI scenarios

- Available: create tasks with mixed statuses/claims/blocks, verify correct filtering
- Available: complete a blocker, verify blocked task becomes available
- Available: filter narrowing with project/tag filters
- Pop: verify highest-urgency task is returned, claimed, and started
- Pop: sequential pops by same player get different tasks
- Pop: empty queue returns error
- Pop: filters narrow the pop candidates

### MCP scenarios

- Mirror CLI scenarios using MCP tool calls

---

## Files Changed

| Layer | File(s) | Change |
|-------|---------|--------|
| Domain | `internal/domain/errors.go` | Add `ErrNoAvailableTasks` |
| Repository interface | `internal/repository/relation.go` | Add `CountBlockedByIncompleteTasks` |
| SQLite | `internal/sqlite/relation.go` | Implement `CountBlockedByIncompleteTasks` |
| Service | `internal/service/task.go` | Add `Available` and `Pop` methods |
| CLI | `internal/tui/commands.go` | Add `available` and `pop` subcommands |
| MCP | `internal/mcp/tools.go` | Add handlers + registration |
| E2E | `tests/e2e/` | CLI and MCP scenarios |
| Stubs | `internal/repository/repository_test.go` | Add stub for new interface method |

No migrations, no config changes, no new dependencies.
