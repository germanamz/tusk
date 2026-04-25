# Subtree Urgency Overrides — Design

**Date:** 2026-04-25
**Status:** design approved, awaiting implementation plan
**Roadmap reference:** `ROADMAP.md` → v0.13 → "Initiative: Subtree Urgency Overrides"
**Related hardening initiative:** `ROADMAP.md` → v0.13 → "Initiative: Subtree Urgency Overrides Hardening" (out of scope here, drafted as part of this design review)

## Goal

Let urgency weight overrides be attached to any task and inherit down the parent chain with per-key merge, so a single workspace can host multiple priority zones (e.g., a "ship-critical" milestone subtree boosting `blocking_weight`) without splitting into separate projects.

## Non-goals

- Named, reusable bundles of overrides ("urgency profiles") — deferred to v0.16+ per the existing roadmap.
- Filtering tasks by urgency-override presence (`urgency.has=blocking_weight`) — not in roadmap, no demand.
- `task create`-time overrides — roadmap only lists `task modify`. Same parser helper plugs in later if asked for.
- Customizable rollup formulas / per-team overrides — out of scope for v0.13.
- Materialized/denormalized effective weights — kept as a per-read computation; future optimization.
- Hardening verification stories (deleted-ancestor propagation, `task move` re-walk, cross-project assertion, MCP `tusk_task_tree` urgency parity) — these ship as part of `Initiative: Subtree Urgency Overrides Hardening` in the same milestone, separately implemented.

## Scope

### 1. Storage & domain model

Migration `012_task_urgency_overrides.up.sql`:

```sql
ALTER TABLE tasks ADD COLUMN urgency_overrides TEXT;
-- nullable; when present, contains the JSON shape of domain.UrgencyOverrides.
```

No index — the column is read in batch via the recursive CTE keyed off `id` / `parent_id`, never as a query predicate.

`domain.Task` gains:

```go
UrgencyOverrides *domain.UrgencyOverrides
```

Reuses the existing `domain.UrgencyOverrides` struct (currently used by `ProjectSettings.Urgency`) — sparse, per-key `*float64`. Nil = no overrides on this task.

`domain.TaskUpdate` gains three coordinated fields:

```go
type TaskUpdate struct {
    // ...existing fields...
    UrgencyOverrides  **domain.UrgencyOverrides // ptr-to-ptr: nil = don't touch, *nil = clear all, *value = replace
    UrgencyMergePatch *UrgencyOverridesPatch    // nil = don't touch; otherwise apply RFC 7396-style per-key patch
    UrgencyDelta      map[string]float64        // per-key arithmetic delta to apply after the patch
}
```

with a sibling type in `domain/`:

```go
type UrgencyOverridesPatch struct {
    Set      map[string]float64 // key → new value
    Clear    map[string]bool    // key → true means delete this key
    ClearAll bool               // drop every key
}
```

Why three fields:

- `UrgencyOverrides` covers full-replace (used by JSON import, future restore tooling). Mutually exclusive with the other two fields in a single call.
- `UrgencyMergePatch` is the **incremental** path the CLI and MCP use — explicit "set this key to this float" plus "clear this key".
- `UrgencyDelta` is the CLI's `+`/`-` arithmetic, applied after merge-patch but before persistence.

Path map:

- CLI `tusk task modify`: `UrgencyMergePatch + UrgencyDelta`. Never `UrgencyOverrides`.
- MCP `tusk_task_modify`: `UrgencyMergePatch` only. Never `UrgencyDelta` or `UrgencyOverrides`.
- Future JSON import / restore tooling: `UrgencyOverrides` only (or writes the column directly through the repo, bypassing `Update` entirely).

The service rejects calls that combine `UrgencyOverrides` with either `UrgencyMergePatch` or `UrgencyDelta`.

Application order within a single call, deterministic regardless of map iteration: `ClearAll → Clear keys → Set keys → Delta keys`. Keyspace matches the existing `urgencyCLIToConfigKey` mapping in `internal/tui/project_parse.go` (`priority_weight`, `due_weight`, `age_weight`, `active_weight`, `blocking_weight`, `blocked_weight`, `tags_weight`, `project_weight`, `annotations_weight`, `waiting_weight`).

New repository surface:

```go
// In repository/task.go interface:
GetAncestorOverrides(ctx context.Context, taskIDs []uuid.UUID) ([]AncestorOverride, error)

type AncestorOverride struct {
    TaskID    uuid.UUID
    ParentID  *uuid.UUID
    ProjectID uuid.UUID
    Overrides *domain.UrgencyOverrides // nil if none on this row
}
```

The SQLite implementation runs one recursive CTE: starting from `taskIDs`, walks up via `parent_id` collecting `(id, parent_id, project_id, urgency_overrides)` until `parent_id IS NULL`. Returns one row per visited node (input tasks plus all ancestors). Caller indexes by `TaskID` and walks parent chains in memory.

### 2. Resolution chain in the urgency engine

`ScoringContext` (`service/urgency.go`) gains a per-task resolved-weights map:

```go
type ScoringContext struct {
    BlockingCount    map[uuid.UUID]int
    BlockedByCount   map[uuid.UUID]int
    AnnotationCount  map[uuid.UUID]int
    TagCount         map[uuid.UUID]int
    ProjectWeights   map[uuid.UUID]*UrgencyWeights // unchanged: per-project, project-level merge already applied
    EffectiveWeights map[uuid.UUID]*UrgencyWeights // NEW: per-task fully-resolved weights (project + ancestor + self)
}
```

`UrgencyEngine.weightsFor(...)` becomes `weightsFor(task *domain.Task, ctx ScoringContext)`:

1. If `ctx.EffectiveWeights[task.ID]` exists → return it.
2. Else fall through to existing `ProjectWeights[task.ProjectID]` lookup.
3. Else `e.defaults`.

Invariant: `EffectiveWeights` is only populated for tasks whose chain (project + ancestor + self) contributes a non-default value somewhere. Tasks with no chain overrides keep going through the cheap project-weights path — preserves the current fast path for the common case.

New service helper `buildEffectiveWeights` (`service/task.go`, sibling of `buildProjectWeights`):

```go
func (s *TaskService) buildEffectiveWeights(
    ctx context.Context,
    bundle *RepoBundle,
    tasks []*domain.Task,
    projectWeights map[uuid.UUID]*UrgencyWeights,
) (map[uuid.UUID]*UrgencyWeights, error)
```

Steps:

1. Collect input task IDs.
2. Short-circuit: if no input task has its own `urgency_overrides` non-nil and no input task has a `parent_id` (= no ancestors to walk), return nil. Common-case fast path with zero extra DB work.
3. Otherwise call `bundle.Tasks.GetAncestorOverrides(ctx, taskIDs)` → maps `parentByID`, `overridesByID`, `projectByID`.
4. For each input task `t`:
   - Walk from `t.ID` up via `parentByID` to collect ancestor IDs (root → self).
   - Start with `projectWeights[t.ProjectID]` if present, else engine defaults.
   - For each node in root → self order, `MergeWeights(current, node.Overrides)`.
   - Store in result map only if at least one node in the chain (incl. self) had non-nil overrides.
5. Return.

`MergeWeights` in `service/urgency.go` is unchanged — it already takes a `*domain.UrgencyOverrides` and performs per-key merge. Calling it once per ancestor in chain order produces the cumulative root → self merge that the roadmap specifies.

`listInBundle` (`service/task.go`) wires it in:

```go
projectWeights := s.buildProjectWeights(ctx, tasks)
effective, err := s.buildEffectiveWeights(ctx, bundle, tasks, projectWeights)
if err != nil { return nil, err }

sctx := ScoringContext{
    // ...existing...
    ProjectWeights:   projectWeights,
    EffectiveWeights: effective,
}
```

A new public helper `TaskService.ResolveEffectiveWeights(ctx, taskID) (UrgencyWeights, bool, error)` returns the resolved weights and a `chainHasOverrides` bool for single-task callers (`task get` rendering). Implemented on top of the same batch helper with a one-task input.

#### Decided edge-case behaviors

- **Deleted/terminal ancestors:** the walk does NOT filter by status. A deleted parent's overrides still apply to surviving descendants. No precedent in tusk for status-driven inheritance changes (tags, project, level all ignore parent status). Locked in by a hardening-initiative regression test.
- **Re-parenting:** zero special handling. The override JSON stays on the moved task; next read re-walks against the new chain. Already covered by the existing `task move` code path. Locked in by a hardening-initiative regression test.
- **Cross-project ancestry:** doesn't happen — tasks inherit `project_id` from parent on creation, and `task move` keeps subtrees pinned to one project. The hardening initiative adds a defensive assertion in `buildEffectiveWeights` so a future regression that breaks the invariant fails loud.

### 3. CLI surface

Inline syntax extension on `tusk task modify`. Lives in a new helper `internal/tui/urgency_parse.go` shared between `parseProjectModify` (already does ~95% of this for projects) and a new task-modify wiring path:

| Form | Effect |
|---|---|
| `urgency.<weight>=<float>` | Set self override for `<weight>` to the literal value. |
| `urgency.<weight>=` | Clear self override for `<weight>` (drops just that key from the JSON; other keys preserved). |
| `+urgency.<weight>=<delta>` | Arithmetic delta against the resolved-effective weight at the self position (existing self value if present; otherwise the inherited value from project + ancestor chain). Stored as a new absolute `*float64`. |
| `-urgency.<weight>=<delta>` | Same as `+`, with the sign flipped. |
| `urgency.clear=true` | Drop every self override on this task in one call. Sets the column back to NULL. `urgency.clear=false` is a no-op (parser accepts it for symmetry with `taxonomy.disable=`). |

Composition rules within a single `tusk task modify` invocation:

1. `urgency.clear=true` applied first if present.
2. Empty-value clears applied next.
3. Absolute sets applied next (`urgency.<weight>=<float>`).
4. Deltas applied last (`+/-urgency.<weight>=<delta>`).

The order of arguments on the command line is irrelevant; the operation order is by category, deterministic. So `tusk task modify abc urgency.clear=true urgency.priority-weight=5 +urgency.due-weight=2` resolves as: clear all → set priority-weight=5 → add 2 to the inherited due-weight value.

Delta evaluation occurs **inside the same transaction** that writes the update so deltas can't race with concurrent ancestor edits. The transaction reads the pre-update task plus its ancestors (single recursive-CTE batch), computes the resolved-inherited value per requested delta key, applies the delta, writes the merged JSON.

Parser helper signature:

```go
type UrgencyParseResult struct {
    ClearAll bool
    Clear    map[string]bool
    Set      map[string]float64
    Delta    map[string]float64
}

func parseUrgencyFields(fs syntax.FieldSet) (UrgencyParseResult, error)
```

Called from both `parseProjectModify` (existing) and a new `parseTaskModify` urgency branch. Reuses `urgencyCLIToConfigKey` for keyspace validation.

`tusk task create` does NOT accept urgency overrides — roadmap lists `modify` only. `tusk config show` is unchanged — task-level overrides are task data, not config.

### 4. MCP surface

`tusk_task_modify` tool gains one new top-level input field plus one sibling control flag:

```jsonc
{
  "short_id": "a3f8b2c1",
  "version": 7,
  // ...existing fields...
  "urgency_overrides": {              // NEW: RFC 7396 JSON merge patch
    "blocking_weight": 20.0,          //   set to 20.0
    "due_weight": null,               //   delete this key from existing overrides
    "priority_weight": 5.0
    // absent keys: unchanged
  },
  "urgency_overrides_clear": false    // NEW: when true, drop ALL self overrides; precedes the patch
}
```

Semantics (mirror the CLI; no arithmetic deltas):

- `urgency_overrides_clear: true` → existing column set to `NULL` first.
- Then `urgency_overrides` patch applied, key by key:
  - explicit float value → set that key.
  - explicit `null` → delete that key.
  - absent key → unchanged.
- After both, if no keys remain set, the column is stored as `NULL` (we don't keep an empty `{}`).
- `urgency_overrides: {}` is a no-op (consistent with merge-patch semantics).
- `urgency_overrides: null` (top-level null) is **rejected** with a clear error suggesting `urgency_overrides_clear: true` instead — too easy to confuse with "clear single key".

Validator: `domain.ValidateUrgencyOverridesPatch(map[string]any) error` accepts only the 10 known weight keys, requires each value to be a JSON number or explicit JSON `null`, and rejects unknown keys with a typo-friendly error (`unknown urgency weight "blokking_weight"; valid keys: priority_weight, due_weight, …`). Lives in `domain/` so MCP and any future REST handler reuse it.

Field-level blocked-fields gate (v0.12): two new entries in the configurable block list — `urgency_overrides` and `urgency_overrides_clear`. Both honor the same `Allow`/`Deny` semantics as existing fields. Configuration shape unchanged — these are just two more known field names.

Resource representation: `tusk://tasks/{short_id}` already serializes via `taskJSON`; extending `taskJSON` (§5) keeps the resource and tool reads in sync.

Other MCP tools — `tusk_task_get`, `tusk_task_list`, `tusk_task_tree`, `tusk_task_pop`, `tusk_task_next`, `tusk_task_available` — all serialize through the shared `taskJSON` shape, so they automatically pick up the new fields with no per-tool changes. (The MCP `handleTaskTree` subtree branch is missing urgency scoring entirely; that's tracked in the hardening initiative, not here.)

No MCP arithmetic. Per the design choice in §3, agents compute and `set`. They already read the task before modifying (to obtain `version`), so they have the current effective weights in hand.

### 5. Rendering & visibility

`taskJSON` in `internal/tui/render.go` gains two optional fields:

```go
type taskJSON struct {
    // ...existing fields...
    UrgencyOverrides        *urgencyOverridesJSON `json:"urgency_overrides,omitempty"`
    EffectiveUrgencyWeights *urgencyWeightsJSON   `json:"effective_urgency_weights,omitempty"`
}

// Sparse: only keys explicitly set on this task. Mirrors domain.UrgencyOverrides.
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

// Full table — all 10 weights with their resolved float values. No omitempty.
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

When each is populated:

- `urgency_overrides`: emitted when the task's own column is non-NULL. Sparse — only keys set on this task.
- `effective_urgency_weights`: emitted when the resolved chain differs from the workspace global default — i.e., when any of project, ancestors, or self contributes a non-default value. The 10-weight table is always complete (no sparse keys). When the chain matches defaults exactly, the field is omitted to keep ordinary task output clean.

Source of the resolved table: `TaskService.ResolveEffectiveWeights(ctx, taskID) (UrgencyWeights, bool, error)`. The bool drives the emit/omit decision. `task get` calls this once per rendered task. `task list` and `task tree` populate `Effective` on every task naturally as a byproduct of `buildEffectiveWeights` (§2) and pass it through to render.

`tusk task get` text mode (`renderTaskInfo` in `internal/tui/render.go`) adds two optional sections after `Version:` and before `Annotations:`, only printed when present:

```
Urgency Overrides:
  blocking_weight  20
  due_weight       3.5

Effective Urgency Weights:
  priority_weight    1
  due_weight         3.5
  age_weight         0.1
  active_weight      4
  blocking_weight    20
  blocked_weight     0
  tags_weight        0.5
  project_weight     1
  annotations_weight 0.5
  waiting_weight    -3
```

Two-column aligned, fixed key order matching domain field order, floats formatted with `strconv.FormatFloat(v, 'f', -1, 64)` (matches existing `Order` rendering convention). Section headers go through `r.styledLabel` like the existing `UDA:` block.

`tusk task list` and `tusk task tree`: unchanged columns. We do not add a column for urgency overrides — they're a tuning attribute, not a status indicator, and the table is already wide. Users who want detail drill into `task get`.

`tusk task list --output json` and `tusk task tree --output json`: every task in the array carries `urgency_overrides` and `effective_urgency_weights` per the same rule as `get`. Agent path; we want full information without forcing a follow-up `get`.

`treeNodeJSON` in `internal/tui/tree.go` embeds `taskJSON` already, so it inherits the change once the fields are added to `taskJSON`.

### 6. Testing & verification

Three layers:

**Domain unit tests (`domain/`):**

- `TestValidateUrgencyOverridesPatch` — valid keys + numeric values pass; unknown keys, non-numeric values, nested objects, and arrays all rejected with the actionable error message.
- Existing `MergeWeights` tests in `service/urgency_test.go` already cover per-key merge; add cases for chained merges (default → project → ancestor1 → ancestor2 → self) verifying that closer scopes win on conflict and unspecified keys inherit.

**Service unit tests (`service/`):**

- `TestBuildEffectiveWeights` — fixture trees with overrides at various depths:
  - Task with no chain overrides → not in `EffectiveWeights` (falls through to `ProjectWeights`).
  - Task with overrides only on self.
  - Task with overrides only on root ancestor.
  - Task with overrides on multiple ancestors (closer wins on conflict; others inherited per-key).
  - Task with self + ancestor + project all overriding the same key (self wins).
  - Project + ancestor override different keys (both inherited, no conflict).
- `TestResolveEffectiveWeightsSingleTask` — single-task path used by `task get` returns the same numeric result as a list-based path on the same fixture; `chainHasOverrides` bool flips correctly.
- `TestUpdateUrgencyOverridesPatch` — driving `domain.TaskUpdate` with each shape (`UrgencyOverrides` full replace, `UrgencyMergePatch` set/clear/clearAll, `UrgencyDelta`) yields the expected persisted JSON; mixed-call ordering (`ClearAll → Clear → Set → Delta`) is deterministic.
- `TestUpdateUrgencyDeltaInheritsResolvedValue` — when self has no value for a key, `+urgency.x=2` stores `(inherited resolved value) + 2` as a new self override.

**Repository tests (`sqlite/`):**

- `TestGetAncestorOverrides` — fixture with a 4-deep parent chain plus a sibling branch; assert the CTE returns exactly the visited nodes (input + ancestors), nil overrides on rows without them, populated overrides on rows that have them, terminating at root.
- Round-trip: insert a task with `urgency_overrides`, read it back, verify JSON deserializes to identical `*domain.UrgencyOverrides`. Empty-object vs NULL distinguished correctly.

**E2E tests (new file `tests/e2e/urgency_overrides_test.go`):**

- Set / clear single key via CLI; `task get --output json` reflects the change in `urgency_overrides`.
- `urgency.clear=true` drops every override.
- `+urgency.blocking-weight=5` on a task with no self value uses the inherited value as the base; same delta on a task with an existing self value adds to the self value.
- Subtree inheritance: place a heavy `blocking_weight` override on a milestone, create a grandchild, list with urgency sort, verify the grandchild's `urgency` reflects the boosted weight.
- `task list --output json` carries `effective_urgency_weights` on every task in the boosted subtree; sibling tasks outside the subtree don't.
- MCP path: drive `tusk_task_modify` with the merge-patch shape (set + null + absent + clear flag) through the existing MCP harness; verify resulting state and that the v0.12 blocked-fields config rejects the call when `urgency_overrides` is on the deny list.

The four hardening-initiative stories (deleted-ancestor propagation, `task move` re-walk, cross-project assertion, MCP `tusk_task_tree` urgency parity) are out of scope for this spec's tests — they live in `Initiative: Subtree Urgency Overrides Hardening` and ship as their own follow-ups.

`make test-race` must pass — the new `EffectiveWeights` map is built per-call inside `listInBundle` and never shared across goroutines, so no new locking is needed beyond the existing `UrgencyEngine.mu` guarding `defaults`.

Migration verification: `migrations/012_task_urgency_overrides.up.sql` runs cleanly on a database seeded with the existing migration set; `down.sql` drops the column without error. Cover both via the existing migration test harness in `sqlite/store_test.go`.

## Open questions

None at design time. Implementation may surface concrete questions about transaction boundaries for the CLI delta path or about how `domain.TaskUpdate` composes with existing services that already call `Update`; those are implementation-plan concerns.
