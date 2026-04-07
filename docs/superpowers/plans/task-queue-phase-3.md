# Task Queue — Phase 3: MCP Tools & E2E Tests

**Prerequisites:** Phase 2 must be completed.

---

## Inherits From

Phase 1 introduced:
- `domain.ErrNoAvailableTasks` sentinel error
- `RelationRepository.CountBlockedByIncompleteTasks` method

Phase 2 introduced:
- `TaskService.Available(ctx, filter)` — returns urgency-sorted unclaimed, actionable, unblocked tasks
- `TaskService.Pop(ctx, playerID, filter)` — claims+starts the top available task (optimistic retry)
- `tusk available [filters...] --player <id>` CLI command
- `tusk pop [filters...] --player <id>` CLI command
- `formatError` handles `ErrNoAvailableTasks`

The implementer can rely on `Available` and `Pop` being fully functional service methods, and the CLI commands working.

---

## Goal

Add MCP tools for available and pop, plus comprehensive E2E tests for both CLI and MCP surfaces. After this phase, the Task Queue initiative is complete.

---

## Tasks

### Task 1: Add `tusk_task_available` MCP tool

**File:** `internal/mcp/server.go` (registration) and `internal/mcp/tools.go` (handler)

**Registration** in `server.go` — add after the `tusk_task_next` registration (around line 453). Follow the `addTool` pattern:

```go
s.addTool("task",
    mcp.NewTool("tusk_task_available",
        mcp.WithDescription("List unclaimed, actionable, unblocked tasks sorted by urgency"),
        mcp.WithString("player_id",
            mcp.Required(),
            mcp.Description("Player ID — auto-registers as agent on first use"),
        ),
        mcp.WithString("filter",
            mcp.Description("Boolean filter expression (e.g. 'project:backend AND +api')"),
        ),
    ),
    s.handleTaskAvailable,
)
```

Also add `"tusk_task_available": true` to the known tools map in `server.go` (around line 107, where `tusk_task_claim` and `tusk_task_release` are listed).

**Handler** in `tools.go` — add `handleTaskAvailable` method on `Server`. Follow the pattern of `handleTaskList` (line 333):

1. Extract `player_id` (required string)
2. Auto-register player as `"agent"` type — same pattern as `handleTaskClaim` (line 948)
3. Extract optional `filter` string
4. If filter provided, parse with `filter.ParseExpr` and resolve with `filter.NewResolver` — same as `handleTaskList`
5. Call `s.taskSvc.Available(ctx, resolvedFilter)`
6. Batch-load tags for all returned tasks — same as `handleTaskList`
7. Return `toolResultJSON` with array of `toTaskResponse` objects — same shape as `handleTaskList`

### Task 2: Add `tusk_task_pop` MCP tool

**File:** `internal/mcp/server.go` (registration) and `internal/mcp/tools.go` (handler)

**Registration** in `server.go` — add after `tusk_task_available`:

```go
s.addTool("task",
    mcp.NewTool("tusk_task_pop",
        mcp.WithDescription("Claim and start the highest-urgency available task for the given player"),
        mcp.WithString("player_id",
            mcp.Required(),
            mcp.Description("Player ID — auto-registers as agent on first use"),
        ),
        mcp.WithString("filter",
            mcp.Description("Optional boolean filter to narrow candidates (e.g. 'project:backend')"),
        ),
    ),
    s.handleTaskPop,
)
```

Also add `"tusk_task_pop": true` to the known tools map.

**Handler** in `tools.go` — add `handleTaskPop` method on `Server`. Follow the pattern of `handleTaskClaim`:

1. Extract `player_id` (required string)
2. Auto-register player as `"agent"` type
3. Extract optional `filter` string, parse and resolve if provided
4. Call `s.taskSvc.Pop(ctx, playerID, resolvedFilter)`
5. If `domain.ErrNoAvailableTasks` — return `mcp.NewToolResultText("No available tasks matching the given filters")`
6. On success, load tags for the single task and return `toolResultJSON(toTaskResponse(task, tags))`

### Task 3: Add CLI E2E tests for `available` and `pop`

**File:** `tests/e2e/task_queue_test.go` (new file — keeps task queue scenarios separate from player registration/claiming tests in `player_test.go`)

Use the existing E2E harness pattern. Each scenario runs across DB config modes x output formats automatically.

**Scenarios to implement:**

1. **Available — basic filtering:**
   - Add 3 tasks with different priorities
   - Claim one task with player p1
   - Run `available --player p2`
   - Assert: only the 2 unclaimed tasks appear

2. **Available — blocked task excluded:**
   - Add task A and task B
   - Link A blocks B
   - Run `available --player p1`
   - Assert: B does not appear (blocked by incomplete A)
   - Done A (complete it)
   - Run `available --player p1` again
   - Assert: B now appears (blocker is complete)

3. **Available — with filter narrowing:**
   - Add tasks in different projects
   - Run `available project:backend --player p1`
   - Assert: only backend project tasks appear

4. **Pop — claims and starts highest-urgency task:**
   - Add 2 tasks: one priority:3, one priority:1
   - Run `pop --player p1`
   - Assert: returned task is the priority:3 one
   - Assert: task status is `active`
   - Assert: task claimed_by is `p1`

5. **Pop — sequential pops get different tasks:**
   - Add 2 tasks
   - Run `pop --player p1` — gets first task
   - Run `pop --player p1` — gets second task
   - Run `pop --player p1` — returns "No available tasks"

6. **Pop — with filters:**
   - Add tasks in two projects
   - Run `pop project:backend --player p1`
   - Assert: returned task is from backend project

### Task 4: Add MCP E2E tests for `available` and `pop`

**File:** `tests/e2e/mcp_task_queue_test.go` (new file — keeps MCP task queue scenarios separate from player MCP tests in `mcp_player_test.go`)

Follow the MCP E2E test pattern used in `mcp_player_test.go`. MCP tests call tools via the MCP harness and verify JSON responses.

**Scenarios to implement:**

1. **tusk_task_available — returns unclaimed unblocked tasks:**
   - Create tasks via MCP
   - Claim one via `tusk_task_claim`
   - Call `tusk_task_available` with `player_id`
   - Assert: claimed task not in results

2. **tusk_task_available — blocked task excluded:**
   - Create 2 tasks, add blocks relation
   - Call `tusk_task_available`
   - Assert: blocked task not in results

3. **tusk_task_pop — claims and starts top task:**
   - Create tasks with different priorities via MCP
   - Call `tusk_task_pop` with `player_id`
   - Assert: response contains highest-priority task
   - Assert: status is `active`, claimed_by matches player_id

4. **tusk_task_pop — empty queue:**
   - Call `tusk_task_pop` with no tasks available
   - Assert: text response "No available tasks matching the given filters"

---

## Verification

After this phase:
- `go build ./...` compiles cleanly
- `make test` passes (all existing + new E2E tests)
- `make test-e2e` passes
- MCP tools visible in tool listing (under "task" group)
- Full round-trip: create tasks via MCP → pop via MCP → verify claimed+started

---

## User-Visible Behaviors Preserved

All existing CLI commands and MCP tools continue to work identically. The only additions are:
- `tusk_task_available` MCP tool
- `tusk_task_pop` MCP tool
- E2E test coverage for the complete Task Queue feature

---

## Changes Introduced

| Type | Detail |
|------|--------|
| New MCP tool | `tusk_task_available` — handler in `internal/mcp/tools.go`, registration in `internal/mcp/server.go` |
| New MCP tool | `tusk_task_pop` — handler in `internal/mcp/tools.go`, registration in `internal/mcp/server.go` |
| Modified known tools map | `internal/mcp/server.go` — added entries for both new tools |
| New E2E tests | CLI scenarios in `tests/e2e/task_queue_test.go` |
| New E2E tests | MCP scenarios in `tests/e2e/mcp_task_queue_test.go` |
| No bridge code | All additions are final implementations. No bridge code from prior phases to remove. |
