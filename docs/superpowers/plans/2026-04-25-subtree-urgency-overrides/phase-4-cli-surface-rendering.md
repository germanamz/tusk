# Phase 4 — CLI Surface and Rendering

**Spec:** `docs/superpowers/specs/2026-04-25-subtree-urgency-overrides-design.md`
**Phase size:** 6 tasks

## Prerequisites

Phases 1, 2, 3 must be merged. Implementer can rely on full programmatic read + write of urgency overrides through `TaskService`.

## Inherits From

After Phase 3:
- `TaskService.Update` accepts `UrgencyOverrides` (full replace), `UrgencyMergePatch`, and `UrgencyDelta`, applying them in order `ClearAll → Clear → Set → Delta` inside a single transaction, with mutual-exclusion between `UrgencyOverrides` and the patch/delta fields.
- `TaskService.ResolveEffectiveWeights(ctx, taskID) (UrgencyWeights, bool, error)` returns resolved weights and a chain-has-overrides bool.
- `domain.UrgencyOverrideFieldPtr`, `domain.ValidUrgencyWeightKeys`, `domain.ValidateUrgencyOverridesPatch` all live in `domain/`.
- No CLI or MCP surface exposes the new machinery; `urgency_overrides` is always NULL for any task that wasn't mutated through direct service calls (i.e., effectively always, in normal usage).

## Goal

Wire the CLI: parse the inline urgency syntax on `tusk task modify`, render `urgency_overrides` and `effective_urgency_weights` in `task get` (text + JSON), and surface the same JSON fields on every list/tree response. After this phase, CLI users can set, clear, delta-tune, and inspect overrides end-to-end.

## Tasks

### Task 4.1 — Shared urgency parser helper

1. Create `internal/tui/urgency_parse.go`:
   ```go
   package tui

   import (
       "fmt"
       "strconv"

       "github.com/germanamz/tusk/syntax"
   )

   // UrgencyParseResult is the structured output of parseUrgencyFields.
   type UrgencyParseResult struct {
       ClearAll bool
       Clear    map[string]bool
       Set      map[string]float64
       Delta    map[string]float64
   }

   // Empty returns true when no urgency-related field was consumed.
   func (r UrgencyParseResult) Empty() bool {
       return !r.ClearAll && len(r.Clear) == 0 && len(r.Set) == 0 && len(r.Delta) == 0
   }

   // urgencyFieldInput is the minimal shape parseUrgencyFields needs.
   // Both filter.FieldFilter and syntax.FieldFilter fit.
   type urgencyFieldInput struct {
       Key      string
       Value    string
       Modifier byte // 0, '+', or '-'
   }

   // parseUrgencyFields consumes urgency-flavored fields from an iterator and
   // returns the structured result plus a list of indices that were NOT
   // consumed (so callers can continue processing non-urgency fields).
   func parseUrgencyFields(fields []urgencyFieldInput) (UrgencyParseResult, []int, error)
   ```
2. Implementation details:
   - Walk `fields`. For each field, decide if it is urgency-related:
     - Key `"urgency.clear"` → special control. Modifier must be 0 (reject `+urgency.clear` and `-urgency.clear`). Value must be `"true"` or `"false"`. `"true"` → `result.ClearAll = true`. `"false"` → no-op (but consume the field). Otherwise error `urgency.clear expects true or false, got %q`.
     - Key starts with `"urgency."` AND calls `urgencyCLIToConfigKey(key)` (keep in `internal/tui/project_parse.go:45` — do NOT move it, just import it) successfully → this is a weight field. Handle:
       - Modifier `0`: value `""` → `result.Clear[cfgKey] = true`. Value non-empty → parse float via `strconv.ParseFloat`; on success `result.Set[cfgKey] = v`; on error wrap with `fmt.Errorf("field %q: invalid float %q: %w", key, value, err)`.
       - Modifier `'+'`: parse float; `result.Delta[cfgKey] = +v`.
       - Modifier `'-'`: parse float; `result.Delta[cfgKey] = -v`.
     - Otherwise: not urgency-related; append the field's original index to the "not consumed" slice.
   - Return `(result, notConsumed, nil)`.

### Task 4.2 — Refactor parseProjectModify to use the shared helper

1. In `internal/tui/project_parse.go::parseProjectModify` (line 158):
   - After `fs, parseErrs := syntax.ParseFields(input)` (line 192) and before the main field-iteration loop (line 197), convert `fs.Fields` to `[]urgencyFieldInput` and call `parseUrgencyFields`.
   - Map `result.Set` to `mut.UrgencySet` and `result.Delta` to `mut.UrgencyDelta` (existing fields on `projectModifyFields`).
   - Reject `result.ClearAll` or `len(result.Clear) > 0` with:
     ```go
     return projectModifyFields{}, fmt.Errorf("urgency.clear=true and urgency.<weight>= (empty-value clear) are not supported on project modify; use tusk task modify for task-level overrides")
     ```
   - Skip the consumed fields when continuing the main loop — iterate only the "not consumed" indices from `parseUrgencyFields`.
2. Remove the inline urgency-field handling block that previously lived in the main loop (project_parse.go:197-220 covered the `+`/`-` modifier branch; lines 274-284 covered the bare `key=value` default branch for urgency). The shared helper replaces both.
3. Run `make test ./internal/tui/...`; existing project-modify tests must pass unchanged.

### Task 4.3 — Wire urgency into `tusk task modify`

1. In `internal/tui/commands.go::runModify` (line 744), after the existing `filter.Parse(input)` call (line 749) builds `fs`, convert `fs.Fields` to `[]urgencyFieldInput` and call `parseUrgencyFields`. Keep track of consumed indices.
2. Construct the domain update:
   ```go
   if result.ClearAll || len(result.Clear) > 0 || len(result.Set) > 0 {
       upd.UrgencyMergePatch = &domain.UrgencyOverridesPatch{
           ClearAll: result.ClearAll,
           Clear:    result.Clear,
           Set:      result.Set,
       }
   }
   if len(result.Delta) > 0 {
       upd.UrgencyDelta = result.Delta
   }
   ```
3. When iterating `fs.Fields` later in `runModify` to handle non-urgency keys, skip any field consumed by `parseUrgencyFields`. The simplest approach: build a `set[int]` of consumed indices and check it inside the existing loop.
4. In `internal/tui/commands.go::runCreate` (the task-create path), `tusk task create` must continue to reject `urgency.*` fields as unknown. The existing path already rejects unknown keys via the general unknown-field handler — verify no regression, add a small test `TestRunCreate_RejectsUrgencyFields` in `commands_test.go` that passes `urgency.priority-weight=5` on create and asserts the error mentions the unknown key.

### Task 4.4 — taskJSON and treeNodeJSON new fields

1. In `internal/tui/render.go`, add two new types near `taskJSON`:
   ```go
   // urgencyOverridesJSON is the sparse per-task self overrides; only keys
   // explicitly set on the task appear.
   type urgencyOverridesJSON struct {
       PriorityWeight    *float64 `json:"priority_weight,omitempty"`
       DueWeight         *float64 `json:"due_weight,omitempty"`
       AgeWeight         *float64 `json:"age_weight,omitempty"`
       ActiveWeight      *float64 `json:"active_weight,omitempty"`
       BlockingWeight    *float64 `json:"blocking_weight,omitempty"`
       BlockedWeight     *float64 `json:"blocked_weight,omitempty"`
       TagsWeight        *float64 `json:"tags_weight,omitempty"`
       ProjectWeight     *float64 `json:"project_weight,omitempty"`
       AnnotationsWeight *float64 `json:"annotations_weight,omitempty"`
       WaitingWeight     *float64 `json:"waiting_weight,omitempty"`
   }

   // urgencyWeightsJSON is the full 10-weight resolved table; all fields
   // always present when emitted.
   type urgencyWeightsJSON struct {
       PriorityWeight    float64 `json:"priority_weight"`
       DueWeight         float64 `json:"due_weight"`
       AgeWeight         float64 `json:"age_weight"`
       ActiveWeight      float64 `json:"active_weight"`
       BlockingWeight    float64 `json:"blocking_weight"`
       BlockedWeight     float64 `json:"blocked_weight"`
       TagsWeight        float64 `json:"tags_weight"`
       ProjectWeight     float64 `json:"project_weight"`
       AnnotationsWeight float64 `json:"annotations_weight"`
       WaitingWeight     float64 `json:"waiting_weight"`
   }
   ```
2. Extend `taskJSON` (line ~317) with the two new optional fields:
   ```go
   UrgencyOverrides        *urgencyOverridesJSON `json:"urgency_overrides,omitempty"`
   EffectiveUrgencyWeights *urgencyWeightsJSON   `json:"effective_urgency_weights,omitempty"`
   ```
3. Add a transient in-memory field to `domain.Task`. Because `domain` cannot import `service` (service depends on domain), introduce a matching value-typed struct in `domain/`:

   Create `domain/resolved_urgency_weights.go`:
   ```go
   package domain

   // ResolvedUrgencyWeights is the fully-resolved 10-weight table for a task.
   // Field names mirror UrgencyOverrides (and its JSON tags) to make rendering
   // trivial. Populated from service.UrgencyWeights via the Resolved() adapter.
   type ResolvedUrgencyWeights struct {
       PriorityWeight    float64
       DueWeight         float64
       AgeWeight         float64
       ActiveWeight      float64
       BlockingWeight    float64
       BlockedWeight     float64
       TagsWeight        float64
       ProjectWeight     float64
       AnnotationsWeight float64
       WaitingWeight     float64
   }
   ```

   On `domain.Task`, add:
   ```go
   // EffectiveWeights is populated by the service layer for rendering; not
   // persisted. Nil means the resolved chain matches defaults — renderers
   // omit the `effective_urgency_weights` block. Mirrors the transient
   // Urgency float64 field's pattern.
   EffectiveWeights *ResolvedUrgencyWeights
   ```

   Add the adapter to `service/urgency.go` (note: the service field names are unsuffixed `Priority`, `Due`, etc., while the domain field names are suffixed `PriorityWeight`, `DueWeight`, etc. — this is deliberate, mirroring the existing `domain.UrgencyOverrides` snake_case JSON tags):
   ```go
   // Resolved converts an internal UrgencyWeights to the domain-level,
   // JSON-friendly ResolvedUrgencyWeights shape used by renderers.
   func (w UrgencyWeights) Resolved() domain.ResolvedUrgencyWeights {
       return domain.ResolvedUrgencyWeights{
           PriorityWeight:    w.Priority,
           DueWeight:         w.Due,
           AgeWeight:         w.Age,
           ActiveWeight:      w.Active,
           BlockingWeight:    w.Blocking,
           BlockedWeight:     w.Blocked,
           TagsWeight:        w.Tags,
           ProjectWeight:     w.Project,
           AnnotationsWeight: w.Annotations,
           WaitingWeight:     w.Waiting,
       }
   }
   ```
4. Update `Renderer.toTaskJSON` (line 341):
   - If `t.UrgencyOverrides != nil`, copy its 10 pointer fields into a fresh `urgencyOverridesJSON` and set `tj.UrgencyOverrides`.
   - If `t.EffectiveWeights != nil`, copy its 10 float fields into a fresh `urgencyWeightsJSON` and set `tj.EffectiveUrgencyWeights`.
5. In `internal/tui/tree.go`, `treeNodeJSON` currently embeds task-specific JSON. Confirm it picks up the new fields automatically. If `treeNodeJSON` maintains its own explicit copy of task fields, add the same two fields and the same population logic.

### Task 4.5 — renderTaskInfo: text-mode sections

1. In `internal/tui/render.go::renderTaskInfo` (line 483), after the `Version:` line (around line 619) and before the `Annotations:` block:
   ```go
   if task.UrgencyOverrides != nil {
       if _, err := fmt.Fprintln(r.w); err != nil { return err }
       if _, err := fmt.Fprintln(r.w, r.styledLabel("Urgency Overrides:")); err != nil { return err }
       if err := r.renderSparseUrgencyOverrides(task.UrgencyOverrides); err != nil { return err }
   }
   if task.EffectiveWeights != nil {
       if _, err := fmt.Fprintln(r.w); err != nil { return err }
       if _, err := fmt.Fprintln(r.w, r.styledLabel("Effective Urgency Weights:")); err != nil { return err }
       if err := r.renderResolvedUrgencyWeights(*task.EffectiveWeights); err != nil { return err }
   }
   ```
2. Implement two small renderer helpers in the same file, following the `renderUDASection` pattern (line 657):
   - `renderSparseUrgencyOverrides(o *domain.UrgencyOverrides) error` — iterate the 10 keys in the fixed order of `domain.ValidUrgencyWeightKeys`, skip nil pointers, format each non-nil value with `strconv.FormatFloat(v, 'f', -1, 64)`, emit `"  %-18s %s\n"` (18 is the width of `annotations_weight`, the longest key).
   - `renderResolvedUrgencyWeights(w domain.ResolvedUrgencyWeights) error` — always emit all 10 keys in the same fixed order, same format. Define a small key→float accessor (switch on key name returning the field value) to keep the loop symmetric.

### Task 4.6 — Wire resolution into CLI/MCP get + list + tree paths; add E2E

1. **List/tree path (service/task.go).** Extend `listInBundle` (line ~280) to stamp `EffectiveWeights` on each task after scoring:
   ```go
   for _, t := range tasks {
       if w, ok := effective[t.ID]; ok {
           rw := w.Resolved()
           t.EffectiveWeights = &rw
       }
   }
   ```
   This requires Phase 2's `buildEffectiveWeights` plumbing (already in place) plus the new `Resolved()` method on `UrgencyWeights` (Task 4.4). Place the stamp loop right before `listInBundle` returns.
2. **Get path (service/task.go).** `TaskService.GetByShortID` and related single-task getters must also populate `task.EffectiveWeights`. Add a small helper:
   ```go
   // stampEffectiveWeights populates task.EffectiveWeights if the task's chain
   // contributes any non-default value. Safe to call on any loaded task.
   func (s *TaskService) stampEffectiveWeights(ctx context.Context, task *domain.Task) error {
       w, has, err := s.ResolveEffectiveWeights(ctx, task.ID)
       if err != nil { return err }
       if has {
           rw := w.Resolved()
           task.EffectiveWeights = &rw
       }
       return nil
   }
   ```
   Call it in:
   - `TaskService.GetByShortID` (just before return).
   - `TaskService.Get` (just before return).
   - Any other single-task getter that feeds CLI `task get` or MCP `tusk_task_get` output.
3. **MCP handlers (`internal/mcp/tools.go`).** `handleTaskGet` already calls the service; since `GetByShortID` now stamps the field, no MCP code change is needed. `handleTaskList` / `handleTaskTree` already go through `List` / `ListTree` which use `listInBundle`. The MCP `handleTaskTree` *subtree* branch (the one that uses `GetDescendants` raw) is out of scope here — tracked separately in the hardening initiative.
4. **E2E tests.** Create `tests/e2e/urgency_overrides_test.go` following the existing harness pattern (see `tests/e2e/sibling_ordering_test.go` for the `[]Scenario{{Name:, Steps:}}` shape and the `$0.short_id` reference syntax):
   - `set_single_key` — create task; `task modify <id> urgency.blocking-weight=20`; `task get --output json <id>`; assert `urgency_overrides.blocking_weight == 20`.
   - `clear_single_key` — same task; `task modify <id> urgency.blocking-weight=`; `task get`; assert `urgency_overrides` is null/absent.
   - `clear_all` — set two keys; `task modify <id> urgency.clear=true`; `task get`; assert `urgency_overrides` absent.
   - `delta_against_inherited` — configure a project-level `urgency.blocking-weight=10`; create a task with no self value; `task modify <id> +urgency.blocking-weight=5`; `task get`; assert self `blocking_weight == 15`.
   - `delta_against_self_value` — task with self `blocking_weight = 20`; `task modify <id> +urgency.blocking-weight=5`; assert self `blocking_weight == 25`.
   - `subtree_inheritance` — milestone with `blocking_weight = 100`; add child and grandchild (no overrides); `task list --output json`; assert grandchild's top-level `urgency` score reflects the boost and its `effective_urgency_weights.blocking_weight == 100`.
   - `sibling_without_inheritance` — a task in a separate subtree without overrides anywhere in its chain: assert `effective_urgency_weights` is absent in JSON output.
   - `text_mode_renders_sections` — text-mode `task get`; assert stdout contains `"Urgency Overrides:"` and `"Effective Urgency Weights:"` when applicable.
   - `task_create_rejects_urgency` — `task create "X" urgency.blocking-weight=5`; assert the error names the unknown field.

## User-visible behaviors that must still work after this phase

- `tusk task list`, `tusk task tree`, `tusk task get`, `tusk task modify` (no urgency args), `tusk task create` continue to behave identically when the new urgency args are not passed. JSON output for tasks with no chain overrides is byte-identical to pre-Phase-4 (both new fields use `omitempty`).
- `tusk project modify urgency.<key>=<value>` and `tusk project modify +urgency.<key>=<delta>` continue to work exactly as before via the refactored shared parser helper — guarded by the existing project-modify test suite.
- Text-mode `task get` output remains identical for any task whose chain has no overrides.
- `make test` and `make test-race` pass.

## Bridge code

None. Phase 5 (MCP write surface) plugs into `domain.TaskUpdate` fields that Phase 3 already introduced, so no stubs are required here.

## Changes Introduced

- **New files:**
  - `internal/tui/urgency_parse.go` — `UrgencyParseResult`, `parseUrgencyFields`.
  - `tests/e2e/urgency_overrides_test.go` — CLI E2E scenarios.
  - `domain/resolved_urgency_weights.go` — `ResolvedUrgencyWeights` struct.
- **Modified files:**
  - `domain/task.go` — `Task.EffectiveWeights *ResolvedUrgencyWeights` transient field.
  - `filter/parser.go` — pass-through for `urgency.*` and `urgency.clear` keys so they reach `runModify`'s parser without tripping the unknown-field check; `task create` continues to reject them via `validateKnownFields`.
  - `internal/tui/project_parse.go` — refactored to call `parseUrgencyFields`; local urgency-handling blocks removed.
  - `internal/tui/commands.go` — `runModify` consumes `parseUrgencyFields` result; `runCreate` test guards.
  - `internal/tui/commands_test.go` — `TestRunCreate_RejectsUrgencyFields` (and any modify-urgency test coverage to complement E2E).
  - `internal/tui/render.go` — `urgencyOverridesJSON`, `urgencyWeightsJSON`, `taskJSON` new fields, `toTaskJSON` population, `renderTaskInfo` new sections, two render helpers.
  - `internal/tui/tree.go` — `treeNodeJSON` new fields (if it maintains its own field list).
  - `service/urgency.go` — `UrgencyWeights.Resolved() domain.ResolvedUrgencyWeights` adapter.
  - `service/task.go` — `listInBundle` stamps `t.EffectiveWeights`; `stampEffectiveWeights` helper called from single-task getters.
- **Modified interfaces:** `domain.Task` gains a transient `EffectiveWeights` field (additive, zero-value compatible). `service.UrgencyWeights` gains a `Resolved()` method. No other public signatures change.
- **No new schema migrations, environment variables, or dependencies.**
