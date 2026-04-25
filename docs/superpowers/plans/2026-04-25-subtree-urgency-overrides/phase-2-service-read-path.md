# Phase 2 — Service-Layer Read Path (Resolution Chain)

**Spec:** `docs/superpowers/specs/2026-04-25-subtree-urgency-overrides-design.md`
**Phase size:** 5 tasks

## Prerequisites

Phase 1 must be merged. This phase assumes:
- `tasks.urgency_overrides` column exists and round-trips through the SQLite layer.
- `domain.Task.UrgencyOverrides *UrgencyOverrides` is populated on reads, persisted on writes.
- `repository.TaskRepository.GetAncestorOverrides(ctx, taskIDs)` returns ancestor walks for any input set, with `AncestorOverride` rows (`TaskID`, `ParentID`, `ProjectID`, `Overrides`).
- `domain.UrgencyOverridesPatch` and `domain.ValidateUrgencyOverridesPatch` exist in `domain/`.

## Inherits From

No service or UI consumer calls the Phase 1 additions yet — the machinery is plumbed but unused. This phase wires ancestor-aware resolution into `service/urgency.go` and `service/task.go::listInBundle`, and exposes a single-task helper for future rendering.

## Goal

After this phase, anything that lists or fetches tasks resolves the override chain transparently. No caller has yet *set* an override (that ships in Phase 3), so user-visible behavior is byte-identical to pre-Phase-2 in practice — the new code paths are dormant for databases where every `urgency_overrides` is NULL.

## Tasks

### Task 2.1 — Extend ScoringContext and UrgencyEngine.weightsFor

1. In `service/urgency.go`, add to `ScoringContext` (around line 29):
   ```go
   EffectiveWeights map[uuid.UUID]*UrgencyWeights
   ```
   Document inline: "per-task fully-resolved weights (project + ancestor + self). Populated only for tasks whose chain contributes at least one non-default value; callers still fall through to `ProjectWeights` / `defaults` when a task ID is absent."
2. Change the signature of `UrgencyEngine.weightsFor` (currently at line 151):
   ```go
   func (e *UrgencyEngine) weightsFor(task *domain.Task, ctx ScoringContext) UrgencyWeights
   ```
   (Previously took `projectID uuid.UUID`.) New resolution order inside the function:
   - If `ctx.EffectiveWeights != nil` and `w, ok := ctx.EffectiveWeights[task.ID]; ok`, return `*w`.
   - Else if `ctx.ProjectWeights != nil` and `pw, ok := ctx.ProjectWeights[task.ProjectID]; ok`, return `*pw`.
   - Else return `e.defaults` under `e.mu.RLock()` (preserve the existing locking pattern).
3. Update the single call site in `UrgencyEngine.Score` (line ~81) to pass `task` instead of `task.ProjectID`.
4. `UrgencyEngine.ScoreAndSort` is unchanged at the call-site level; `Score` now gets the right weights per task automatically.

### Task 2.2 — buildEffectiveWeights helper

1. In `service/task.go`, add a new method sibling to `buildProjectWeights` (line ~387). Signature:
   ```go
   func (s *TaskService) buildEffectiveWeights(
       ctx context.Context,
       bundle *RepoBundle,
       tasks []*domain.Task,
       projectWeights map[uuid.UUID]*UrgencyWeights,
   ) (map[uuid.UUID]*UrgencyWeights, error)
   ```
2. Implementation (inline comments in the doc are not required; follow the structure below):
   - **Short-circuit fast path.** Iterate `tasks`: if every task has `t.UrgencyOverrides == nil` AND `t.ParentID == nil`, return `nil, nil`. No CTE round-trip needed — we know there's nothing to inherit or apply.
   - Otherwise collect `taskIDs := make([]uuid.UUID, len(tasks))`. Call `bundle.Tasks.GetAncestorOverrides(ctx, taskIDs)`. On error wrap with `fmt.Errorf("loading ancestor overrides: %w", err)`.
   - Build three lookup maps from the result rows:
     - `parentByID map[uuid.UUID]*uuid.UUID` — visited node ID → parent ID.
     - `overridesByID map[uuid.UUID]*domain.UrgencyOverrides` — visited node ID → overrides (nil if none).
     - `projectByID map[uuid.UUID]uuid.UUID` — visited node ID → project ID.
   - Pre-allocate result `out := make(map[uuid.UUID]*UrgencyWeights, len(tasks))`.
   - For each task `t` in `tasks`:
     1. Walk `t.ID` up via `parentByID` collecting a `[]uuid.UUID` chain. Order matters: we need root → self. Append IDs starting at `t.ID`, then `*parentByID[t.ID]`, etc., until the current ID has `parentByID[id] == nil`. Then reverse to root → self.
     2. Seed `merged`: start with `projectWeights[t.ProjectID]` if present (deref the pointer), else acquire `e.defaults` via `s.engine.defaults` under `s.engine.mu.RLock()`. To avoid accessing an unexported field, expose a helper `UrgencyEngine.Defaults() UrgencyWeights` (RLock-guarded) in `service/urgency.go`.
     3. `contributed := false`. For each ancestor ID in chain order, if `overridesByID[id] != nil`, merge: `merged = MergeWeights(merged, overridesByID[id])`; set `contributed = true`.
     4. If `contributed`, store a copy: `out[t.ID] = &merged`. Otherwise leave unset (the weightsFor fallback chain handles it).
   - Return `out, nil`.
3. Add the helper `UrgencyEngine.Defaults()` mentioned above:
   ```go
   func (e *UrgencyEngine) Defaults() UrgencyWeights {
       e.mu.RLock()
       defer e.mu.RUnlock()
       return e.defaults
   }
   ```

### Task 2.3 — Wire into listInBundle

1. In `service/task.go::listInBundle` (line ~280), after the existing `projectWeights := s.buildProjectWeights(...)` call (it builds `sctx.ProjectWeights`) and before the `ScoringContext` literal is constructed, add:
   ```go
   projectWeights := s.buildProjectWeights(ctx, tasks)
   effective, err := s.buildEffectiveWeights(ctx, bundle, tasks, projectWeights)
   if err != nil {
       return nil, err
   }
   ```
2. Update the `ScoringContext` literal to include `EffectiveWeights: effective`.
3. After `s.engine.ScoreAndSort(tasks, sctx)`, add a small loop that stamps the resolved weights onto each task's transient field — this lets downstream renderers read the data without re-resolving:
   ```go
   // Phase 4 consumers (rendering) read t.EffectiveWeights directly.
   // The field is nil for tasks whose chain matches defaults.
   for _, t := range tasks {
       if w, ok := effective[t.ID]; ok {
           t.EffectiveWeights = w
       }
   }
   ```
   However, `Task.EffectiveWeights` does NOT exist yet — it is introduced in Phase 4. **Defer this stamping loop to Phase 4.** Phase 2 stops at populating `ScoringContext.EffectiveWeights`. The scoring math reads it via `weightsFor`; no rendering consumer exists yet.

### Task 2.4 — ResolveEffectiveWeights single-task helper

1. Add a public method on `TaskService`:
   ```go
   // ResolveEffectiveWeights returns the fully-resolved urgency weights for a
   // single task, walking the project + ancestor + self override chain. The
   // second return is true when any node in the chain contributed a non-default
   // value (drives Phase 4's render-or-omit decision for effective_urgency_weights).
   func (s *TaskService) ResolveEffectiveWeights(ctx context.Context, taskID uuid.UUID) (UrgencyWeights, bool, error)
   ```
2. Implementation:
   - Resolve the bundle: `bundle, _, err := s.bundleForID(ctx, taskID)`. Wrap errors with `fmt.Errorf("resolving bundle: %w", err)`.
   - Load the task via `bundle.Tasks.Get(ctx, taskID)`. Propagate `domain.ErrNotFound` unchanged.
   - Build `projectWeights := s.buildProjectWeights(ctx, []*domain.Task{task})`.
   - Call `effective, err := s.buildEffectiveWeights(ctx, bundle, []*domain.Task{task}, projectWeights)`.
   - If `w, ok := effective[task.ID]; ok`, return `*w, true, nil`.
   - Otherwise build the fallback: `if pw, ok := projectWeights[task.ProjectID]; ok { return *pw, false, nil }` else `return s.engine.Defaults(), false, nil`.
3. Export the helper on the existing `Client` root-package type so external library consumers can call it (match how `Tasks` is exposed in `client.go`). If `Client` is plumbed through a `TaskService` field, nothing new is needed — the method is automatically reachable.

### Task 2.5 — Service tests

1. In `service/urgency_test.go`, add `TestMergeWeightsChained`:
   - Build a 5-layer chain of overrides: defaults, then project (overrides `PriorityWeight`), then ancestor-root (overrides `DueWeight`), then ancestor-mid (overrides `DueWeight` to a different value and adds `AgeWeight`), then self (overrides `BlockingWeight`).
   - Fold them with `MergeWeights` in order: defaults → +project → +root → +mid → +self.
   - Assert: `PriorityWeight` comes from project; `DueWeight` comes from ancestor-mid (overrode root); `AgeWeight` comes from ancestor-mid; `BlockingWeight` comes from self; every other key comes from defaults.
2. Create `service/task_urgency_overrides_test.go`. Tests use the existing service test harness pattern (see `service/task_test.go` for in-memory bundle setup).
3. Add the following tests in that file:
   - `TestBuildEffectiveWeightsNoOverrides` — list of three tasks, none has overrides, none has a parent. Call `buildEffectiveWeights`; assert result is nil (fast-path, no CTE call).
   - `TestBuildEffectiveWeightsSelfOnly` — task with `UrgencyOverrides.BlockingWeight = 20`, no parent, no project override. Assert result `[task.ID].BlockingWeight == 20`.
   - `TestBuildEffectiveWeightsAncestorOnly` — root has `BlockingWeight = 10`; child has nil overrides. Assert `result[child.ID].BlockingWeight == 10` and `result[root.ID].BlockingWeight == 10`.
   - `TestBuildEffectiveWeightsCloserAncestorWins` — root sets `BlockingWeight = 10`; mid sets `BlockingWeight = 20`; leaf is nil. Assert `result[leaf.ID].BlockingWeight == 20`.
   - `TestBuildEffectiveWeightsPerKeyMerge` — root sets `BlockingWeight = 10`; mid sets `DueWeight = 5`; leaf is nil. Assert `result[leaf.ID].BlockingWeight == 10 && result[leaf.ID].DueWeight == 5`.
   - `TestBuildEffectiveWeightsProjectPlusSelf` — project sets `BlockingWeight = 10`; task self sets `DueWeight = 5`. Assert resolved weights have both.
   - `TestResolveEffectiveWeightsSingleTask_HasOverrides` — single-task helper on a task with ancestor overrides returns `(weights, true, nil)`. Numeric result matches `buildEffectiveWeights` on the same task.
   - `TestResolveEffectiveWeightsSingleTask_NoOverrides` — single-task helper on a task whose chain has no overrides returns `(project-or-defaults, false, nil)`.

## User-visible behaviors that must still work after this phase

- `tusk task list`, `tusk task tree`, `tusk task get`, `tusk task next`, `tusk task pop`, `tusk task available` produce numerically identical urgency scores and byte-identical output for any task whose entire ancestor chain (plus self) has no `urgency_overrides` set — the common case for any existing database upgraded to this phase.
- `make test-race` passes — `EffectiveWeights` is built inside `listInBundle` per call and never shared across goroutines.
- Project-level urgency overrides (the existing feature) continue to resolve exactly as before for tasks with no ancestor chain overrides. When a task has neither ancestor nor self overrides, `weightsFor` falls through to `ProjectWeights` unchanged.

## Bridge code

None. The new resolution path is dormant until Phase 3 enables write surface that can populate `urgency_overrides`.

## Changes Introduced

- **New files:**
  - `service/task_urgency_overrides_test.go`
- **Modified files:**
  - `service/urgency.go` — `ScoringContext.EffectiveWeights`, `weightsFor` signature change, new `Defaults()` helper.
  - `service/urgency_test.go` — `TestMergeWeightsChained`.
  - `service/task.go` — `buildEffectiveWeights` method, `ResolveEffectiveWeights` public method, wiring in `listInBundle`.
- **Modified interfaces:** `UrgencyEngine.weightsFor` is unexported, so the signature change is internal. `UrgencyEngine.Defaults()` and `TaskService.ResolveEffectiveWeights` are new exported methods.
- **No new schema migrations, environment variables, or dependencies.**
