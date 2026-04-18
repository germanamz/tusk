# Phase 5 — RelationService Emission

**Design reference:** `docs/superpowers/specs/2026-04-18-event-log-design.md`.
**Milestone:** v0.13 — Initiative: Event Log.

## Prerequisites

Phases 1 and 2 must be complete. This phase does **not** depend on phases 3 or 4 — it can run in parallel with them if a separate implementer is available.

## Inherits from phases 1–2

- Events framework and retention in place (phase 1).
- `RepoBundle.WriteTx` is populated in production code paths and in test bundles built via `bundleFromStore` (phase 2).
- `domain.NewRelationAddedEvent` and `domain.NewRelationRemovedEvent` constructors exist (phase 1, `domain/event_relation.go`).
- `RelationService.Add` currently uses `sourceBundle.Store.WithRelationTx` for `"blocks"` relations (for cycle detection) and a non-tx `sourceBundle.Relations.Create` for other relation types. `RelationService.Remove` is non-transactional.

## Goal

Refactor `RelationService.Add` and `RelationService.Remove` so that every successful mutation writes its repo row and its event inside the same `WriteTx`. Preserve cycle detection behavior for `"blocks"` relations. After this phase, the Event Log initiative's shipping surface is complete: every mutation across `TaskService` (phases 3–4) and `RelationService` emits an action-specific event atomically.

## Tasks

### 5.1 — Refactor `RelationService.Add`

Current `service/relation.go:52–97` has two branches: `"blocks"` uses `WithRelationTx` with cycle detection; other relation types bypass the tx entirely.

Rewrite so both branches share a single `WriteTx` path. Cycle detection stays inside the tx (it must — detecting cycles without seeing other writers' committed state would be wrong). Emission happens inside the same fn.

```go
func (s *RelationService) Add(ctx context.Context, sourceShortID, targetShortID, relType string) (*domain.Relation, error) {
    if !validRelationTypes[relType] {
        return nil, fmt.Errorf("invalid relation type %q: must be one of blocks, relates_to, duplicates", relType)
    }

    sourceBundle, source, err := s.findTask(ctx, sourceShortID)
    if err != nil {
        if errors.Is(err, domain.ErrNotFound) { return nil, domain.ErrSourceNotFound }
        return nil, fmt.Errorf("resolving source task: %w", err)
    }
    _, target, err := s.findTask(ctx, targetShortID)
    if err != nil {
        if errors.Is(err, domain.ErrNotFound) { return nil, domain.ErrTargetNotFound }
        return nil, fmt.Errorf("resolving target task: %w", err)
    }

    rel := &domain.Relation{
        ID:           uuid.New(),
        SourceID:     source.ID,
        TargetID:     target.ID,
        RelationType: relType,
        CreatedAt:    time.Now().UTC().Truncate(time.Millisecond),
    }

    actor := ActorFromContext(ctx)
    err = sourceBundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
        if relType == "blocks" {
            if err := s.checkCycle(ctx, tx.Relations(), source.ID, target.ID); err != nil {
                return err
            }
        }
        if err := tx.Relations().Create(ctx, rel); err != nil {
            return err
        }
        return tx.Events().Record(ctx, domain.NewRelationAddedEvent(rel, source.ShortID, target.ShortID, actor))
    })
    if err != nil { return nil, err }
    return rel, nil
}
```

Notes:

- `s.checkCycle` already accepts `repository.RelationRepository`, so feeding it `tx.Relations()` works without signature changes.
- Cycle rejection (`domain.ErrCyclicBlock`) rolls back the transaction automatically — no partial write, no event recorded. Matches the current behavior.
- The event is emitted with the short IDs because consumers read events with the human-facing identifier; storing UUIDs alone would require a lookup to be useful.

### 5.2 — Refactor `RelationService.Remove`

Current `service/relation.go:100–118` is a single `sourceBundle.Relations.DeleteByFields` call. Rewrite so deletion and emission happen inside a `WriteTx`:

```go
func (s *RelationService) Remove(ctx context.Context, sourceShortID, targetShortID, relType string) error {
    sourceBundle, source, err := s.findTask(ctx, sourceShortID)
    if err != nil {
        if errors.Is(err, domain.ErrNotFound) { return domain.ErrSourceNotFound }
        return fmt.Errorf("resolving source task: %w", err)
    }
    _, target, err := s.findTask(ctx, targetShortID)
    if err != nil {
        if errors.Is(err, domain.ErrNotFound) { return domain.ErrTargetNotFound }
        return fmt.Errorf("resolving target task: %w", err)
    }

    if !validRelationTypes[relType] {
        return fmt.Errorf("invalid relation type %q: must be one of blocks, relates_to, duplicates", relType)
    }

    actor := ActorFromContext(ctx)
    return sourceBundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
        // Look up the relation before deletion so the event can carry its ID.
        rel, err := tx.Relations().GetByFields(ctx, source.ID, target.ID, relType)
        if err != nil { return err }
        if err := tx.Relations().DeleteByFields(ctx, source.ID, target.ID, relType); err != nil {
            return err
        }
        return tx.Events().Record(ctx, domain.NewRelationRemovedEvent(rel, source.ShortID, target.ShortID, actor))
    })
}
```

If `RelationRepository` does not currently expose `GetByFields(ctx, sourceID, targetID, relType)`, add it as part of this task. The SQLite implementation reads the existing row (the same key `DeleteByFields` uses) and returns `domain.ErrNotFound` when absent.

Rationale for the extra read: event payloads carry the relation's UUID (`entity_id`) so consumers can correlate added/removed pairs. A delete-without-read can't populate it.

### 5.3 — Tests

Extend `service/relation_test.go`:

1. **`Add` with `relates_to` emits `relation_added`** with correct payload (`source_short_id`, `target_short_id`, `relation_kind`) and `entity_id == rel.ID`. Actor from `WithActor(ctx, "german")` flows through.
2. **`Add` with `blocks` emits `relation_added`** when no cycle is present.
3. **`Add` with `blocks` rejecting a cycle emits no event.** Set up a cycle (A blocks B already; try A blocks B → target forms cycle with B blocks A), assert `ErrCyclicBlock` returned and the event table contains no `relation_added` row for the attempt.
4. **`Remove` emits `relation_removed`** with matching payload.
5. **`Remove` of a non-existent relation returns `ErrNotFound` and emits no event.**
6. **Actor propagation.** Verify `PlayerID` is nil when no actor is in context and equals the actor when set.

Use `newTestBundle` / `newSeededBundle` (phase 2 wires `WriteTx`).

### 5.4 — Rollback regression check for relations

Extend `service/task_tx_invariant_test.go` (created in phase 4) with two additional cases: `RelationService.Add` and `RelationService.Remove`. Inject the failing `WriteTxProvider` from phase 4 and assert:

- `Add`: when `tx.Events().Record` fails, no relation row exists afterward. Assert via `bundle.Relations.GetByFields` returning `ErrNotFound`.
- `Remove`: when `tx.Events().Record` fails, the relation row still exists afterward.

If this file is entirely owned by phase 4's implementer agent, this task extends it; if phase 4 hasn't landed yet (parallel execution), create a sibling file `service/relation_tx_invariant_test.go` using the same `failingProvider` type and copy the ~30 lines of setup. Deduplication is a post-merge concern, not a phase-boundary concern.

## User-visible behavior preserved

- `tusk task link <source> blocks <target>` creates the relation if and only if it wouldn't form a cycle — identical behavior to before phase 5.
- `tusk task unlink <source> <kind> <target>` removes the relation and returns the same errors as before.
- MCP tools backed by these methods behave identically.
- Performance: each `Add`/`Remove` now opens one transaction (up from zero in the `relates_to` case, one in the `blocks` case). This is a cost of correctness and is negligible in SQLite WAL mode.

## Changes introduced

- **No new files** (unless phase 4 hasn't landed; then `service/relation_tx_invariant_test.go`).
- **Modified files:**
  - `service/relation.go` — full rewrites of `Add` and `Remove`.
  - `service/relation_test.go` — new cases per 5.3.
  - `service/task_tx_invariant_test.go` — add `Add` and `Remove` cases per 5.4.
  - `repository/relation.go` — add `GetByFields` method signature if missing.
  - `sqlite/relation.go` — implement `GetByFields` if missing (likely already exists as a helper; verify first).
- **Modified interfaces / signatures:** potentially adds `RelationRepository.GetByFields`. Additive; no existing caller breaks.
- **No schema or config changes.**
- **No new dependencies.**
- **No bridge code.** Phase completes the Event Log initiative's shipping surface.

## Acceptance criteria

- `make build`, `make vet`, `make lint` all pass.
- Full test suite (`make test`) passes, including the new relation event cases.
- Manual check with a dev build:
  - `tusk task link A blocks B` → events table shows one `relation_added` with `entity_kind='relation'`.
  - `tusk task unlink A blocks B` → shows one `relation_removed`.
  - Attempt to create a cycle → error surfaces to CLI; no `relation_added` row written.
- After all phases land, the milestone-level verification passes:
  - Create a task, modify it, start it, complete it, link it to another, unlink. Expect exactly: `task_created`, `task_modified`, `task_started`, `task_completed`, `relation_added`, `relation_removed` — six rows, in that order.
  - Run the same sequence 15_000 times; assert event count settles at `max_events = 10000` (with `prune_slack=1000`, peaks briefly up to 11_000 then prunes).
