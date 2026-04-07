# Task Queue — Phase 2: Service Layer & CLI Commands

**Prerequisites:** Phase 1 must be completed.

---

## Inherits From

Phase 1 introduced:
- `domain.ErrNoAvailableTasks` sentinel error in `internal/domain/errors.go`
- `RelationRepository.CountBlockedByIncompleteTasks` method (interface + SQLite implementation)
- Updated test stub in `internal/repository/repository_test.go`

The implementer can rely on `CountBlockedByIncompleteTasks` being available on `s.relationRepo` inside `TaskService`.

---

## Goal

Add `TaskService.Available` and `TaskService.Pop` methods, plus `tusk available` and `tusk pop` CLI commands. After this phase, users can list available tasks and pop the next task from the queue via CLI. MCP tools come in Phase 3.

---

## Tasks

### Task 1: Implement `TaskService.Available`

**File:** `internal/service/task.go`

Add method after `Release` (around line 590):

```go
func (s *TaskService) Available(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error)
```

Implementation:

1. Define a local pointer helper at the top of the method or as a package-level unexported function (no `boolPtr` exists in the `service` package):
   ```go
   func boolPtr(b bool) *bool { return &b }
   ```
2. Build the base filter as an `AndFilter` containing:
   - An `OrFilter` with two `TermFilter`s: `{Statuses: ["pending"]}` and `{Statuses: ["active"]}`
   - A `TermFilter` with `Unclaimed: boolPtr(true)`
3. If `filter` is non-nil, wrap it into the `AndFilter` as an additional child
4. Call the existing `s.List(ctx, combinedFilter)` — this handles repo query, urgency scoring, and sorting
5. Post-filter the results to remove non-actionable tasks:
   a. Collect all task IDs from the result
   b. Call `s.relationRepo.CountBlockedByIncompleteTasks(ctx, taskIDs)` — remove tasks with count > 0
   c. Remove waiting tasks: skip any task where `task.WaitUntil != nil && task.WaitUntil.After(time.Now())` (same logic as `TaskService.Next` at line 235)
6. Return the remaining list (urgency order is preserved since we only remove elements)

**Important:** The waiting-task filter matches the existing `Next` method behavior. An available task with `wait_until` in the future is not actionable.

### Task 2: Implement `TaskService.Pop`

**File:** `internal/service/task.go`

Add method after `Available`:

```go
func (s *TaskService) Pop(ctx context.Context, playerID string, filter domain.FilterExpr) (*domain.Task, error)
```

Implementation:

1. Call `s.Available(ctx, filter)` to get the urgency-ranked list
2. If list is empty, return `nil, domain.ErrNoAvailableTasks`
3. Iterate from top (this is a retry loop for race conditions — it claims exactly one task):
   a. Call `s.Claim(ctx, task.ShortID, playerID, task.Version)`
   b. If error is `domain.ErrTaskClaimed` or `domain.ErrConflict` — continue to next task
   c. On successful claim, call `s.Start(ctx, task.ShortID, task.Version+1, playerID)`
   d. If Start returns `domain.ErrConflict` — release the claim (`s.Release(ctx, task.ShortID, playerID, task.Version+1)`) and continue to next task. If release fails, log but don't propagate (best-effort cleanup).
   e. Return the started task (the return value from `Start`)
4. If all tasks exhausted, return `nil, domain.ErrNoAvailableTasks`

### Task 3: Add `tusk available` CLI command

**File:** `internal/tui/commands.go`

Add the command to the slice returned by `buildTaskCmds()` (around line 46). Place it after the `release` command (line 116).

Command definition:
```go
{
    Use:   "available [filters...]",
    Short: "List unclaimed, actionable, unblocked tasks",
    RunE:  a.runAvailable,
}
```

Implement `runAvailable` method on `App`. Follow the pattern of `runList` (which starts around line 237):

1. If `a.playerID` is empty, return an error: `"--player flag is required for available"` (same pattern as `runClaim`)
2. Auto-register the player using the same helper used by `runClaim` (check how `runClaim` calls `ensurePlayer` or similar)
3. Parse filter args using `filter.ParseExpr(strings.Join(args, " "))` — same as `runList`
4. Resolve filter expression via `a.resolver.ResolveExpr(ctx, expr)` — same as `runList`
5. Call `a.taskSvc.Available(ctx, resolvedFilter)`
6. Batch-load tags for all tasks — same as `runList`
7. Render using the same table renderer as `runList` (check how `runList` calls the render functions)

### Task 4: Add `tusk pop` CLI command

**File:** `internal/tui/commands.go`

Add the command to the slice returned by `buildTaskCmds()`, after `available`.

Command definition:
```go
{
    Use:   "pop [filters...]",
    Short: "Claim and start the highest-urgency available task",
    RunE:  a.runPop,
}
```

Implement `runPop` method on `App`:

1. If `a.playerID` is empty, return an error: `"--player flag is required for pop"`
2. Auto-register the player (same as `runAvailable`)
3. Parse filter args — same as `runAvailable`
4. Resolve filter expression — same as `runAvailable`
5. Call `a.taskSvc.Pop(ctx, a.playerID, resolvedFilter)`
6. If `domain.ErrNoAvailableTasks`, print "No available tasks" and return exit code 1 (check how other commands handle domain errors via `formatError`)
7. On success, load tags for the single task and render using the same detail renderer as `runInfo`

### Task 5: Add `ErrNoAvailableTasks` to `formatError`

**File:** `internal/tui/commands.go`

In the `formatError` function (line 120), add a case:

```go
case errors.Is(err, domain.ErrNoAvailableTasks):
    return "No available tasks"
```

---

## Verification

After this phase:
- `go build ./...` compiles cleanly
- `make test` passes
- Manual verification: `tusk add "test task" && tusk available --player p1` shows the task
- Manual verification: `tusk pop --player p1` claims+starts the task
- Manual verification: `tusk pop --player p1` again returns "No available tasks"
- Existing CLI commands behave identically

---

## User-Visible Behaviors Preserved

All existing CLI commands (`list`, `info`, `claim`, `release`, `start`, `done`, `delete`, etc.) continue to work identically. The only additions are the new `available` and `pop` subcommands.

---

## Changes Introduced

| Type | Detail |
|------|--------|
| New service methods | `TaskService.Available` and `TaskService.Pop` in `internal/service/task.go` |
| New CLI commands | `tusk available` and `tusk pop` in `internal/tui/commands.go` |
| Modified function | `formatError` in `internal/tui/commands.go` — added `ErrNoAvailableTasks` case |
| No bridge code | All additions are final implementations |
