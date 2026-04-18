# Phase 4 — TaskService Lifecycle Actions: Start and Pop

**Design reference:** `docs/superpowers/specs/2026-04-18-event-log-design.md`.
**Milestone:** v0.13 — Initiative: Event Log.

## Prerequisites

Phases 1, 2, and 3 must be complete.

## Inherits from phases 1–3

- Events framework + retention (phase 1) and shared wiring (phase 2) are in place.
- After phase 3, `TaskService.Create`, `Update`, `Claim`, `Release`, `Complete`, and `Delete` all emit their action-specific events atomically with their repo writes via `bundle.WriteTx.WithTx`.
- Auto-complete and auto-revert cascades (`checkAutoComplete`, `checkAutoRevert`) now take a `WriteTx` parameter and emit `status_changed` events with `source="auto_complete"` or `"auto_revert"` when cascading parents transition.
- `TaskService.applyValidatedUpdate(ctx, tx WriteTx, task, oldStatus)` exists and is the shared helper that writes a prepared task + runs cascades without emitting primary-task events.
- **Known residual state:** `TaskService.Start` and `TaskService.Pop` still call `s.Update` internally. Because `s.Update` now emits `status_changed(source="user")` and (when fields differ) `task_modified`, `Start` currently produces a `status_changed` event and `Pop` produces `status_changed` + possibly `task_modified` from the Claim inside it plus another `status_changed` from the Start inside it. These events are over-emissions, not bugs — this phase replaces them with single-event emissions.

## Goal

Rewrite `TaskService.Start` and `TaskService.Pop` so each emits exactly one action-specific event per call:

- `Start` emits exactly one `task_started` event (with `auto_claimed: bool` payload field), regardless of whether the task was unclaimed and the call auto-claimed it, or already claimed by the calling player.
- `Pop` emits exactly one `task_popped` event (with `claimed_by` + `prev_status` payload fields), never the combination `task_claimed` + `task_started` + overlapping `status_changed`s that the naive decomposition would produce.

Both must continue to run auto-complete/auto-revert cascades correctly (e.g., starting a child from `pending` to `active` should not cascade anything, but those cascade checks still belong inside `applyValidatedUpdate`). Both must still live behind optimistic locking.

This phase also adds a transactional-invariant test covering every emitting path: if `tx.Events().Record` returns an error, the entire task mutation rolls back.

## Tasks

### 4.1 — Rewrite `TaskService.Start`

Current state: `Start` (service/task.go:559–620) resolves the workflow's start status, optionally validates the player and assembles a `domain.TaskUpdate` with `Status` and possibly `ClaimedBy`/`ClaimedAt`, then calls `s.Update(ctx, upd)` — which after phase 3 emits `status_changed(source="user")` and `task_modified` (because `claimed_by` changed).

Rewrite so `Start` performs its own validated mutation inline and emits one `task_started` event:

```go
func (s *TaskService) Start(ctx context.Context, shortID string, version int, playerID string) (*domain.Task, error) {
    bundle, task, err := s.bundleForShortID(ctx, shortID)
    if err != nil { return nil, err }
    if task.Version != version { return nil, domain.ErrConflict }

    project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
    if err != nil { return nil, fmt.Errorf("loading project %v: %w", task.ProjectID, err) }
    wfName, err := s.workflowName(ctx, project)
    if err != nil { return nil, err }
    startStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleStart)
    if err != nil { return nil, fmt.Errorf("resolving start status: %w", err) }

    allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, task.Status, startStatus)
    if err != nil { return nil, fmt.Errorf("checking transition: %w", err) }
    if !allowed {
        return nil, fmt.Errorf("transition %q → %q not allowed: %w", task.Status, startStatus, domain.ErrInvalidTransition)
    }

    var players repository.PlayerRepository
    autoClaimed := false

    if playerID != "" {
        players, err = s.playerRepo(ctx)
        if err != nil { return nil, err }
        if players != nil {
            if _, err := players.GetByID(ctx, playerID); err != nil {
                return nil, fmt.Errorf("player %q: %w", playerID, err)
            }
            if task.ClaimedBy != nil && *task.ClaimedBy != playerID {
                return nil, domain.ErrTaskClaimed
            }
            if task.ClaimedBy == nil {
                now := time.Now().UTC().Truncate(time.Millisecond)
                claimedBy := playerID
                task.ClaimedBy = &claimedBy
                task.ClaimedAt = &now
                autoClaimed = true
            }
        }
    }

    oldStatus := task.Status
    task.Status = startStatus
    task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

    actor := ActorFromContext(ctx)
    var result *domain.Task
    err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
        updated, err := s.applyValidatedUpdate(ctx, tx, task, oldStatus)
        if err != nil { return err }
        result = updated
        return tx.Events().Record(ctx, domain.NewTaskStartedEvent(updated, oldStatus, autoClaimed, actor))
    })
    if err != nil { return nil, err }

    if players != nil && playerID != "" {
        players.UpdateLastSeen(ctx, playerID) //nolint:errcheck
    }
    return result, nil
}
```

Notes:

- The no-player branch of the old Start stayed inside `s.Update`; this rewrite unifies both branches through `applyValidatedUpdate`. Player validation still happens before the tx opens.
- `autoClaimed` is `true` only when the caller provided a non-empty `playerID`, the task was unclaimed, and we filled in `ClaimedBy` + `ClaimedAt`. When the task was already claimed by the same player (idempotent re-start), `autoClaimed` is `false`.
- `Start` does not emit `task_claimed` even though it updates claim fields. The `task_started` event is the sole event emitted for this action. Its `auto_claimed` flag is the complete record.

### 4.2 — Rewrite `TaskService.Pop`

Current state: `Pop` (service/task.go:830–871) iterates `s.Available` results and for each candidate calls `s.Claim` followed by `s.Start`, releasing on Start failure. After phase 3, each `s.Claim` and `s.Start` emits its own events, producing a noisy `task_claimed + task_started` (or worse, with inner status_changed) pair per pop attempt.

Rewrite `Pop` to perform the claim+start atomically inside a single `WriteTx` and emit exactly one `task_popped` event per successful claim. The available-tasks query still runs outside the tx (it is a read). The tx opens only once a candidate is selected.

```go
func (s *TaskService) Pop(ctx context.Context, playerID string, filter domain.FilterExpr) (*domain.Task, error) {
    available, err := s.Available(ctx, filter)
    if err != nil { return nil, err }
    if len(available) == 0 { return nil, domain.ErrNoAvailableTasks }

    players, err := s.playerRepo(ctx)
    if err != nil { return nil, err }
    if players == nil { return nil, fmt.Errorf("player support not configured") }
    if _, err := players.GetByID(ctx, playerID); err != nil {
        return nil, fmt.Errorf("player %q: %w", playerID, err)
    }

    actor := ActorFromContext(ctx)
    for _, candidate := range available {
        bundle, task, err := s.bundleForShortID(ctx, candidate.ShortID)
        if err != nil {
            if errors.Is(err, domain.ErrNotFound) { continue }
            return nil, err
        }

        project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
        if err != nil { return nil, fmt.Errorf("loading project: %w", err) }
        wfName, err := s.workflowName(ctx, project)
        if err != nil { return nil, err }
        startStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleStart)
        if err != nil { return nil, fmt.Errorf("resolving start status: %w", err) }

        allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, task.Status, startStatus)
        if err != nil { return nil, fmt.Errorf("checking transition: %w", err) }
        if !allowed { continue }

        if task.ClaimedBy != nil {
            // Lost the race with another claimer; try the next candidate.
            continue
        }

        now := time.Now().UTC().Truncate(time.Millisecond)
        claimedBy := playerID
        task.ClaimedBy = &claimedBy
        task.ClaimedAt = &now
        oldStatus := task.Status
        task.Status = startStatus
        task.ModifiedAt = now

        var result *domain.Task
        err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
            updated, err := s.applyValidatedUpdate(ctx, tx, task, oldStatus)
            if err != nil { return err }
            result = updated
            return tx.Events().Record(ctx, domain.NewTaskPoppedEvent(updated, playerID, oldStatus, actor))
        })
        if err != nil {
            if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrTaskClaimed) {
                continue
            }
            return nil, err
        }

        players.UpdateLastSeen(ctx, playerID) //nolint:errcheck
        return result, nil
    }

    return nil, domain.ErrNoAvailableTasks
}
```

Notes:

- `Pop` emits exactly one `task_popped` event on success and no events at all on failure. No `task_claimed`, `task_started`, or `status_changed` events are emitted by this path.
- The `ErrConflict` / `ErrTaskClaimed` recovery mirrors the original loop's behavior of trying the next candidate when a race is lost.
- If every candidate is unavailable by the time `Pop` finishes iterating, return `domain.ErrNoAvailableTasks` to match the original contract.

### 4.3 — Update existing `Start` and `Pop` tests

The current `service/task_test.go` has tests for both methods (search for `TestTaskService_Start`, `TestTaskService_Pop`, `TestTaskService_StartAutoClaim`, and any race-condition tests). Verify each:

- Uses `newSeededBundle` (so `bundle.WriteTx` is populated) rather than constructing a `RepoBundle` literal.
- Still passes after the rewrites. In particular, assertions on returned task state (status transitioned to start status, claim fields populated) remain correct.

Add new cases:

1. **Start (explicit player, unclaimed) emits exactly one `task_started` with `auto_claimed=true`.**
2. **Start (explicit player, already claimed by same player) emits `task_started` with `auto_claimed=false`.**
3. **Start (no player) emits `task_started` with `auto_claimed=false`** and `PlayerID == ActorFromContext(ctx)` (or nil if none).
4. **Start never emits `status_changed` or `task_claimed` events.** Assert those types are absent from the event list after the call.
5. **Pop emits exactly one `task_popped`.** Assert payload fields `claimed_by` and `prev_status`. Assert no `task_claimed`, `task_started`, `status_changed`, or `task_modified` events were emitted.
6. **Pop with no available tasks emits zero events and returns `ErrNoAvailableTasks`.**
7. **Pop race-condition regression.** Create two tasks; concurrently call `Pop` with different player IDs (use a goroutine pair with `sync.WaitGroup`); assert exactly two `task_popped` events and both players ended up with a distinct task. This exercises the `ErrConflict`/`ErrTaskClaimed` recovery path.

### 4.4 — Transactional invariant test

Create a new test file `service/task_tx_invariant_test.go` that exercises every emitting mutation path and asserts the rollback guarantee. For each method under test, inject a `WriteTxProvider` whose `tx.Events().Record` returns a sentinel error:

```go
// Fake WriteTx whose Events().Record always errors. Tasks/Relations go to
// an in-memory repo wired from a real sqlite.Tx so the tx is real — we
// want the actual rollback behavior, not a mock.
```

Implementation approach:

1. Define `type failingEvents struct { inner repository.EventRepository }` whose `Record` returns `errors.New("inject: event record failed")`. Its `List`, `Count`, `PruneToSize` delegate to `inner`.
2. Define `type failingWriteTx struct { inner service.WriteTx; events repository.EventRepository }` that overrides `Events()` to return the failing wrapper.
3. Define `type failingProvider struct { real service.WriteTxProvider }` whose `WithTx` calls `real.WithTx`, wrapping the inner `WriteTx` in `failingWriteTx` before passing to `fn`.
4. In each test case: set `bundle.WriteTx = &failingProvider{real: bundle.WriteTx}`, perform the mutation, assert it returned the injected error, and assert the underlying task row has its pre-call state (version, status, claim fields — whichever should not have changed).

Cases (one `t.Run` each): `Create`, `Update` (non-status change), `Update` (status change), `Claim`, `Release`, `Complete`, `Delete`, `Start`, `Pop`. Every case must demonstrate that the task row is unchanged after the failure — the assertion proves the entire mutation was rolled back with the event insert.

This test file is the authoritative guarantee that the event log is never missing a row for a mutation that appears to have happened.

## User-visible behavior preserved

- `tusk task start <id>` behaves identically: transitions status to the workflow's start status, auto-claims if `--player` is set and the task was unclaimed, rejects if claimed by another player, version-checks. Return payload unchanged.
- `tusk task pop --player <id>` behaves identically: returns the highest-urgency available task after claiming + starting it atomically; surfaces the same error when nothing is available; tolerates races against other concurrent pops.
- Auto-complete and auto-revert cascades continue to fire when Start or Pop cross trigger statuses (rare in kanban but possible in custom workflows).
- Optimistic-locking semantics unchanged.

## Changes introduced

- **New files:** `service/task_tx_invariant_test.go` (rollback guarantee coverage).
- **Modified files:**
  - `service/task.go` — full rewrite of `Start` and `Pop` bodies.
  - `service/task_test.go` — updated and extended test cases per 4.3.
- **Modified interfaces / signatures:** none. Public signatures of `Start` and `Pop` unchanged.
- **No schema or config changes.**
- **No new dependencies.**
- **No bridge code.** Phase leaves the TaskService fully compliant with the action-intent event model.

## Acceptance criteria

- `make build`, `make vet`, `make lint` all pass.
- Full test suite (`make test`) passes, including all new cases.
- `grep -n "s.Update\|s.Claim\|s.Start" service/task.go` shows `Start` and `Pop` no longer calling `s.Update`, `s.Claim`, or `s.Start`. The only remaining internal uses of these helpers are non-event-emitting call sites that didn't exist before (there should be none).
- Manual check with a dev build:
  - `tusk task start <id>` on an unclaimed task with `--player german` → event table shows one `task_started` row with payload `auto_claimed=true`, `player_id='german'`.
  - `tusk task pop --player german` → event table shows one `task_popped` row with payload `claimed_by='german'`.
  - No `task_claimed`, `task_modified`, or `status_changed` rows accompany these actions (in a kanban workflow with no cascades configured).
- The transactional-invariant test (4.4) is green and covers all nine emitting methods.
