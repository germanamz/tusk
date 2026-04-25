# Phase 3 — Service-Layer Write Path (TaskUpdate Plumbing)

**Spec:** `docs/superpowers/specs/2026-04-25-subtree-urgency-overrides-design.md`
**Phase size:** 5 tasks

## Prerequisites

Phases 1 and 2 must be merged. Implementer can rely on:
- `tasks.urgency_overrides` column round-trip via the SQLite layer.
- `domain.Task.UrgencyOverrides`, `domain.UrgencyOverridesPatch`, `domain.ValidateUrgencyOverridesPatch`, `domain.ValidUrgencyWeightKeys` in `domain/`.
- `service.TaskService.ResolveEffectiveWeights(ctx, taskID) (UrgencyWeights, bool, error)` for inherited-value lookups.
- `service.buildEffectiveWeights(ctx, bundle, tasks, projectWeights)` for batch resolution.

## Inherits From

After Phase 2, the service resolves ancestor + self overrides on every list path, but no caller can yet mutate the column — it persists as NULL on every task. This phase enables programmatic writes.

## Goal

Make `TaskService.Update` accept and apply urgency-overrides mutations: full-replace (`UrgencyOverrides`), RFC 7396 merge patch (`UrgencyMergePatch`), and arithmetic deltas (`UrgencyDelta`). After this phase, programmatic clients can set, clear, and tune overrides on any task. CLI and MCP do not expose the surface yet — that is Phases 4 and 5.

## Tasks

### Task 3.1 — Extend domain.TaskUpdate

1. In `domain/task.go`, add three fields to the `TaskUpdate` struct (place after the existing nullable-pointer fields `ClaimedBy`, `ClaimedAt`):
   ```go
   // UrgencyOverrides replaces the full urgency_overrides JSON column.
   // Ptr-to-ptr semantics match other nullable fields:
   //   nil        → don't touch
   //   *nil       → clear all (column becomes NULL)
   //   *value     → full replace with the given pointer target
   // Mutually exclusive with UrgencyMergePatch and UrgencyDelta.
   UrgencyOverrides **UrgencyOverrides

   // UrgencyMergePatch applies an RFC 7396-style per-key patch after any
   // ClearAll. nil = don't touch.
   UrgencyMergePatch *UrgencyOverridesPatch

   // UrgencyDelta applies per-key arithmetic deltas after the merge patch.
   // Each key → signed delta float. When self has a value, the delta is added
   // to it; otherwise the delta is added to the resolved-inherited value at
   // the self position in the chain.
   UrgencyDelta map[string]float64
   ```
2. Zero values of each field are no-ops — this keeps existing callers of `service.TaskService.Update` binary-compatible.

### Task 3.2 — Promote `urgencyOverrideFieldPtr` to domain

1. Move the unexported helper `urgencyOverrideFieldPtr` (currently in `internal/tui/project_parse.go:68`) to `domain/urgency_overrides_helpers.go` as an exported function:
   ```go
   package domain

   // UrgencyOverrideFieldPtr returns a pointer to the **float64 field on
   // UrgencyOverrides matching the given snake_case key. Returns nil for
   // unknown keys. Both the CLI parser and the service write path use this
   // to translate key strings into concrete field pointers.
   func UrgencyOverrideFieldPtr(o *UrgencyOverrides, key string) **float64
   ```
   Keep the switch body identical (10 cases matching `domain.ValidUrgencyWeightKeys`).
2. Update the call site in `internal/tui/project_parse.go:148` to call `domain.UrgencyOverrideFieldPtr` and delete the old unexported copy.
3. No behavior change to existing project-modify tests; they should pass unchanged.

### Task 3.3 — TaskService.Update: merge patch and full-replace application

1. In `service/task.go::Update` (line ~433), inside the existing transaction body and after the block that applies `upd.UDA` but before the final persistence step (`bundle.Tasks.Update(ctx, task)`), add the urgency application logic.
2. **Mutual-exclusion guard:**
   ```go
   if upd.UrgencyOverrides != nil && (upd.UrgencyMergePatch != nil || len(upd.UrgencyDelta) > 0) {
       return nil, fmt.Errorf("urgency_overrides full-replace is mutually exclusive with merge patch and delta; got both in one update")
   }
   ```
   Return before any mutation.
3. **Apply full replace** (if `upd.UrgencyOverrides != nil`):
   ```go
   task.UrgencyOverrides = *upd.UrgencyOverrides // may be nil to clear
   ```
   Skip the merge-patch and delta steps below (return out of this block).
4. **Apply merge patch** (if `upd.UrgencyMergePatch != nil`):
   1. `patch := upd.UrgencyMergePatch`
   2. If `patch.ClearAll`, set `task.UrgencyOverrides = nil`.
   3. If `task.UrgencyOverrides == nil` and (`len(patch.Clear) > 0` or `len(patch.Set) > 0`), allocate a fresh `task.UrgencyOverrides = &domain.UrgencyOverrides{}`.
   4. For each key in `patch.Clear`:
      - `fp := domain.UrgencyOverrideFieldPtr(task.UrgencyOverrides, key)`. If nil, return `fmt.Errorf("urgency_overrides patch: unknown key %q", key)`.
      - `*fp = nil`.
   5. For each key, value in `patch.Set`:
      - `fp := domain.UrgencyOverrideFieldPtr(task.UrgencyOverrides, key)`. If nil, return the same error as above.
      - `v := value; *fp = &v`.
   6. After applying, call a local helper `normalizeUrgencyOverrides(task)`: if every `*float64` field on `task.UrgencyOverrides` is nil, set `task.UrgencyOverrides = nil` so the column persists as NULL. Define this helper in `service/task.go` (unexported, near other small helpers).

### Task 3.4 — TaskService.Update: delta application

After the merge-patch step (still inside the same tx), if `len(upd.UrgencyDelta) > 0`:

1. **Validate keys up front.** For each key in `upd.UrgencyDelta`: if `domain.UrgencyOverrideFieldPtr(&domain.UrgencyOverrides{}, key) == nil`, return `fmt.Errorf("urgency_overrides delta: unknown key %q", key)`.

2. **Add a bundle-scoped resolver helper** to `service/task.go`:
   ```go
   // resolveEffectiveWeightsFromTask resolves the chain using the provided
   // in-memory task for self state, so callers that have just mutated
   // task.UrgencyOverrides get the consistent answer. Ancestors are read
   // fresh from the repo via GetAncestorOverrides.
   //
   // Unlike TaskService.ResolveEffectiveWeights, this does not re-read
   // the task from the DB — callers inside a write transaction need the
   // post-patch in-memory state as the self contribution.
   func (s *TaskService) resolveEffectiveWeightsFromTask(
       ctx context.Context, bundle *RepoBundle, task *domain.Task,
   ) (UrgencyWeights, error)
   ```
   Implementation: reuse the merge logic from `buildEffectiveWeights` on a one-task input, using the passed-in `task` (including its live in-memory `UrgencyOverrides`) as the self node. Do not call `bundle.Tasks.Get(task.ID)` — the caller owns the authoritative self state.

3. **Add a weight-by-key accessor** to `service/urgency.go`:
   ```go
   // WeightByKey returns the named weight's resolved value using the snake_case
   // keys listed in domain.ValidUrgencyWeightKeys. Returns (0, false) for
   // unknown keys.
   func WeightByKey(w UrgencyWeights, key string) (float64, bool)
   ```
   10-case switch over the same keyspace (`priority_weight → w.Priority`, `due_weight → w.Due`, …).

4. **Compute one baseline resolution for all deltas.** Call `resolveEffectiveWeightsFromTask(ctx, bundle, task)` once. The returned weights already reflect the post-merge-patch self state, so for any key:
   - If self has a value → baseline.get(key) returns the self value (self wins in `MergeWeights`).
   - If self has no value → baseline.get(key) returns the inherited value (defaults → project → ancestors).

   Either way, `baseline.get(key)` is the correct base for the delta. This collapses the two cases ("delta against self" vs "delta against inherited") into one uniform rule: `new_value = baseline[key] + delta[key]`. Because `upd.UrgencyDelta` is a `map[string]float64`, no key appears twice, so delta-ordering only affects which keys are touched, not the arithmetic — but apply in sorted order anyway for reproducibility:

   ```go
   baseline, err := s.resolveEffectiveWeightsFromTask(ctx, bundle, task)
   if err != nil {
       return nil, fmt.Errorf("resolving baseline for urgency delta: %w", err)
   }

   keys := make([]string, 0, len(upd.UrgencyDelta))
   for k := range upd.UrgencyDelta {
       keys = append(keys, k)
   }
   sort.Strings(keys)

   if task.UrgencyOverrides == nil {
       task.UrgencyOverrides = &domain.UrgencyOverrides{}
   }
   for _, key := range keys {
       base, ok := WeightByKey(baseline, key)
       if !ok {
           return nil, fmt.Errorf("urgency_overrides delta: unknown key %q", key)
       }
       newValue := base + upd.UrgencyDelta[key]
       fp := domain.UrgencyOverrideFieldPtr(task.UrgencyOverrides, key)
       v := newValue
       *fp = &v
   }
   ```

5. Normalize via the helper from Task 3.3 (no-op if all keys remain set, but keeps the NULL-vs-empty-object invariant consistent).

### Task 3.5 — Service tests

1. Extend `service/task_urgency_overrides_test.go` (created in Phase 2) with:
   - `TestUpdateUrgencyOverridesFullReplace` — pass `UrgencyOverrides = ptrPtr(&UrgencyOverrides{BlockingWeight: ptrFloat(20)})`; verify persisted state; pass `UrgencyOverrides = ptrPtr[*UrgencyOverrides](nil)` (outer non-nil, inner nil); verify column becomes NULL.
   - `TestUpdateUrgencyOverridesMergePatchSet` — task starts with nil overrides; pass `UrgencyMergePatch = &UrgencyOverridesPatch{Set: {"blocking_weight": 20, "due_weight": 5}}`; verify exactly those two keys are stored.
   - `TestUpdateUrgencyOverridesMergePatchClear` — task starts with three keys; pass `Clear: {"due_weight": true}`; verify only `due_weight` removed.
   - `TestUpdateUrgencyOverridesMergePatchClearAll` — task starts with three keys; pass `ClearAll: true`; verify column NULL.
   - `TestUpdateUrgencyOverridesPatchCombined` — pass `ClearAll: true, Clear: {"priority_weight": true}, Set: {"priority_weight": 5, "due_weight": 3}`; verify result has only `priority_weight = 5` and `due_weight = 3` (ClearAll runs first, then Clear drops priority_weight, then Set puts priority_weight back at 5).
   - `TestUpdateUrgencyDeltaInheritsResolvedValue` — project has `BlockingWeight = 10` override; task has no self override; pass `UrgencyDelta = {"blocking_weight": 5}`; verify persisted self `blocking_weight = 15`.
   - `TestUpdateUrgencyDeltaAdditiveOnExistingValue` — task has self `BlockingWeight = 20`; pass delta `{"blocking_weight": 5}`; verify persisted self `blocking_weight = 25`.
   - `TestUpdateUrgencyDeltaAfterPatch` — task has nil overrides; pass patch `Set: {"blocking_weight": 10}` + delta `{"blocking_weight": 3}`; verify final `blocking_weight = 13` (patch applies first: 10, then delta: 10+3).
   - `TestUpdateUrgencyConflictingFields` — pass `UrgencyOverrides` AND `UrgencyMergePatch`; assert error mentions "mutually exclusive"; assert task row unchanged (no version bump, JSON unchanged).
   - `TestUpdateUrgencyUnknownKey` — pass patch `Set: {"bogus": 1}`; assert error mentions `bogus` by name; assert no persisted change.
   - `TestUpdateUrgencyNormalizesEmpty` — task with `{priority_weight: 5}`; pass patch `Clear: {"priority_weight": true}`; verify the column becomes NULL after normalization, not an empty `{}`.
2. Each test uses the standard service-test fixture pattern (see `service/task_test.go` setup). Use the existing `ptrFloat`, `ptrString`, etc. helpers if they exist; otherwise define small local helpers at the top of `task_urgency_overrides_test.go`.

## User-visible behaviors that must still work after this phase

- All existing `TaskService.Update` callers (tests, MCP handlers, CLI runModify) that do not populate the new fields produce byte-identical persisted state and return values.
- `tusk task modify` from the CLI continues to behave identically — Phase 4 introduces the parser that populates the new fields. Until then, the CLI cannot reach this code path.
- Optimistic locking on `Update` still works — passing a stale `Version` returns `domain.ErrConflict` before any urgency application logic runs.
- All existing E2E tests pass without modification.

## Bridge code

None.

## Changes Introduced

- **New files:**
  - `domain/urgency_overrides_helpers.go` — exported `UrgencyOverrideFieldPtr`.
- **Modified files:**
  - `domain/task.go` — three new fields on `TaskUpdate`.
  - `internal/tui/project_parse.go` — deletes local `urgencyOverrideFieldPtr`, calls the domain export.
  - `service/task.go` — mutual-exclusion guard, full-replace, merge-patch, and delta branches in `Update`; `normalizeUrgencyOverrides` helper; `resolveEffectiveWeightsFromTask` helper.
  - `service/urgency.go` — `WeightByKey(w UrgencyWeights, key string) (float64, bool)` helper.
  - `service/task_urgency_overrides_test.go` — new tests.
- **Modified interfaces:** `domain.TaskUpdate` gains three fields (additive; zero values are no-ops). The promoted `domain.UrgencyOverrideFieldPtr` is new exported surface.
- **No new schema migrations, environment variables, or dependencies.**
