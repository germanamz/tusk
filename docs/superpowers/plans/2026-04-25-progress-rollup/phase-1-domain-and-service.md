# Phase 1 — Domain Types, Aggregator, and Service Primitives

**Spec:** `docs/superpowers/specs/2026-04-25-progress-rollup-design.md`
**Phase size:** 5 tasks

## Prerequisites

Base codebase as of commit `b1feca9` (the spec commit). No prior phases required; this is the first phase.

## Goal

Land the rollup data types, the pure aggregator, and the two `TaskService` methods that later phases will compose. After this phase the math is callable from Go and unit-tested across kanban + custom workflows, but no CLI flag, no new subcommand, and no MCP tool exists yet — `tusk task tree`, `tusk task list`, `tusk task get`, and every MCP tool behave identically to before.

## Tasks

### Task 1.1 — Domain types

Create `domain/rollup.go`:

```go
package domain

// StatusCount is one entry in a Rollup's status breakdown.
// Buckets are keyed by status name (case-sensitive), so two workflows
// that both define a status named "active" merge into one bucket — the
// user-visible label is what matters.
type StatusCount struct {
    Name  string `json:"name"`
    Count int    `json:"count"`
}

// Rollup is the aggregated descendant state of a task subtree.
// Tasks whose status carries the `delete` role in their own workflow are
// excluded entirely (they do not contribute to Total or StatusCounts).
type Rollup struct {
    Done         int           `json:"done"`           // count of descendants whose status carries the `done` role
    Total        int           `json:"total"`          // count of descendants whose status does NOT carry the `delete` role
    Percent      float64       `json:"percent"`        // Done/Total in [0.0, 1.0]; 0.0 when Total == 0
    StatusCounts []StatusCount `json:"status_counts"`  // workflow order, zeros included, delete-role buckets excluded
}

// SummaryBlock pairs a block task with its descendant Rollup.
// Used by both the CLI and the MCP layer.
type SummaryBlock struct {
    Task   *Task  `json:"task"`
    Rollup Rollup `json:"rollup"`
}

// Summary is the top-level envelope returned by SummarizeBlocks-shaped
// responses. Mode is one of "single", "filter", or "roots". Totals is
// nil in "single" mode (it would just duplicate the single block's
// Rollup); always populated in "filter" and "roots" mode (the zero
// Rollup when Blocks is empty).
type Summary struct {
    Mode   string          `json:"mode"`
    Blocks []*SummaryBlock `json:"blocks"`
    Totals *Rollup         `json:"totals,omitempty"`
}
```

The `Status` enum constant for the existing `domain.StatusRole` type lives in `domain/workflow.go:16-26` (`RoleDone`, `RoleDelete`, etc.) — nothing new to add there. Confirm Phase 1 introduces no new fields on `Task`, `Workflow`, `Project`, or any existing struct. The four types above are the entire new public domain surface.

### Task 1.2 — Pure aggregator

Add to `domain/rollup.go` below the type definitions:

```go
// AggregateRollup classifies each descendant against its own workflow
// and returns the aggregated Rollup. workflowFor returns the Workflow
// that governs a task — typically a project-keyed lookup. If
// workflowFor returns nil for a given task, that task is skipped (its
// status cannot be classified).
//
// Rules:
//   - A task whose status carries the `delete` role in its workflow is
//     excluded entirely (does not contribute to Total or StatusCounts).
//   - A task whose status carries the `done` role in its workflow
//     contributes to Done, Total, and its name bucket.
//   - All other non-delete tasks contribute to Total and their name
//     bucket.
//   - StatusCounts ordering: the workflow associated with the FIRST
//     non-delete-role descendant encountered seeds the breakdown order
//     (in that workflow's `Statuses` slice ordering, but only including
//     statuses that lack the `delete` role). Statuses appearing later
//     from other workflows are appended in first-seen order. Two
//     workflows with the same status name share one bucket.
//   - Percent is float64(Done) / float64(Total) in the range [0.0, 1.0],
//     or 0.0 when Total == 0.
func AggregateRollup(descendants []*Task, workflowFor func(*Task) *Workflow) Rollup
```

Implementation notes for the implementer:

- `domain.Workflow` exposes `StatusByRole(role StatusRole) (string, bool)` (see `domain/workflow.go:68-75`) and `Statuses map[string]StatusConfig` plus an ordered list of status names. Verify by reading `domain/workflow.go` whether the workflow keeps an ordered slice of statuses; if it only has the map, fall back to constructing a deterministic order from `wf.StatusByRole` for `done`/`delete` and the map keys for the rest. The spec requires *workflow-defined* order for `StatusCounts`, so look for an existing ordered slice (e.g. a `StatusOrder []string` field or similar) before adding new state.
- The function is pure and synchronous. No context, no error return — bad input (e.g. an empty slice, a nil workflow lookup) returns the zero `Rollup{StatusCounts: []StatusCount{}}` (note: empty slice, not nil — JSON consumers expect a stable shape).
- Walk the descendants in input order. Maintain (a) a `map[string]int` for bucket counts, (b) a slice `bucketOrder []string` recording first-seen order, and (c) the `Done`/`Total` counters. After the walk, materialize `StatusCounts` by iterating `bucketOrder`; this preserves first-seen ordering across workflow boundaries.
- For the *seed* workflow's order: when the first non-delete descendant is encountered, look up its workflow once and pre-seed `bucketOrder` with that workflow's status names (excluding any with the `delete` role, including with zero counts). Subsequent descendants from other workflows append any new names they introduce. This satisfies "zeros included" for the seed workflow.
- Status names that exist in the seed workflow but do not appear in any descendant still appear in `StatusCounts` with `Count: 0`.

### Task 1.3 — Aggregator unit tests

Create `domain/rollup_test.go`. Use table-driven tests covering:

1. Empty input → `Rollup{Done: 0, Total: 0, Percent: 0.0, StatusCounts: []StatusCount{}}`.
2. All descendants in a `done`-role status (kanban: `completed`) → `Done == Total`, `Percent == 1.0`, single bucket.
3. All descendants in a `delete`-role status (kanban: `deleted`) → `Total == 0`, `Percent == 0.0`, `StatusCounts == []`.
4. Mixed kanban: 1 pending, 1 active, 3 completed, 1 deleted → `Done: 3, Total: 5, Percent: 0.6, StatusCounts: [{pending:1},{active:1},{completed:3}]` (workflow order, deleted bucket absent).
5. Custom workflow with a non-`completed` `done`-role status (e.g. `shipped`) — verify `done` role lookup is per-workflow, not hardcoded to `completed`.
6. Custom workflow with no `done` role assigned (legal: spec says zero `done` ⇒ all `Done` counts stay 0) — verify aggregator does not panic and `Done == 0`.
7. Multi-workflow descendants: half from kanban (statuses `pending`, `active`, `completed`), half from a custom workflow with statuses `triage`, `shipped` — verify `StatusCounts` orders kanban first (seed workflow), then `triage`, `shipped` appended in first-seen order. Same-named buckets across workflows merge: e.g. add a custom workflow that also has `active` and verify the count combines.
8. `workflowFor` returns nil for some tasks — those tasks are silently skipped; the rest aggregate normally.

Use kanban from the existing test fixture (`domain.DefaultKanbanWorkflow()` if such a helper exists; otherwise construct a `*domain.Workflow` literal in the test). For custom workflows, build literals inline.

### Task 1.4 — `TaskService.SummarizeSubtree`

Add to `service/task.go`. Place the method definition near `GetDescendants` (around line 646) so subtree-shaped reads cluster.

```go
// SummarizeSubtree rolls up a single task's strict descendants. The root
// task itself is NOT counted — its own status answers a different
// question. All non-delete-role descendants count toward Total; those
// whose status carries the `done` role count toward Done.
//
// Returns ErrNotFound if rootID does not exist. Returns a SummaryBlock
// whose Task is the root and whose Rollup describes its descendants.
func (s *TaskService) SummarizeSubtree(
    ctx context.Context,
    rootID uuid.UUID,
) (*domain.SummaryBlock, error)
```

Implementation steps:

1. Resolve the bundle and root task: reuse the existing `s.bundleForID(ctx, rootID)` helper (used by `GetDescendants` at line 646 and many other methods). This returns `(bundle, task, error)` and emits `domain.ErrNotFound` for unknown IDs.
2. Fetch descendants: `bundle.Tasks.GetDescendants(ctx, rootID)` — same call site as the existing `GetDescendants` service method.
3. Build a `workflowFor` closure: walk the descendant slice once to collect distinct `ProjectID`s (plus the root's own project for the seed when descendants is empty), then for each project resolve `*domain.Workflow` via `s.workflowSvc` (or whatever helper already exists for project→workflow lookups — check `service/task.go` for `s.resolveWorkflow(ctx, projectID)` or similar; reuse rather than create).
4. Cache the resolved workflows in a `map[uuid.UUID]*domain.Workflow` keyed by project ID. The closure passed to `AggregateRollup` looks up by `task.ProjectID`.
5. Call `domain.AggregateRollup(descendants, workflowFor)`.
6. Return `&domain.SummaryBlock{Task: root, Rollup: rollup}`.

The root task is NOT included in the descendant slice — `GetDescendants` returns strict descendants. Do not add it.

### Task 1.5 — `TaskService.SummarizeBlocks` and service unit tests

Add to `service/task.go`, immediately after `SummarizeSubtree`:

```go
// SummarizeBlocks selects block tasks via blockFilter and rolls up each
// one's strict descendants. blockFilter == nil means "blocks are root
// tasks (parent_id IS NULL)". When full == false, blockFilter ALSO
// restricts which descendants are counted under each block; when full
// == true, blockFilter only selects blocks and the full subtree under
// each block is counted. Blocks are returned in urgency-desc order with
// short_id ascending as tiebreaker.
func (s *TaskService) SummarizeBlocks(
    ctx context.Context,
    blockFilter domain.FilterExpr,
    full bool,
) ([]*domain.SummaryBlock, error)
```

Implementation steps:

1. Resolve the candidate block set:
   - `blockFilter == nil` → call `bundle.Tasks.List` with a `domain.TermFilter` whose `ParentIDIsNull` flag (or equivalent — verify the existing `TaskFilter` shape in `domain/filter.go:9-30`; if no such flag exists, post-filter the workspace tasks for `ParentID == nil`). The implementer should reuse whatever mechanism `task list parent=null` would use; if none exists, post-filter in Go is acceptable for this initiative.
   - `blockFilter != nil` → call `s.List(ctx, blockFilter)` (the urgency-scoring `List` from line 296 — gives sorted, scored tasks).
2. For each block task, fetch its descendants via `bundle.Tasks.GetDescendants(ctx, blockID)`.
3. If `full == false` and `blockFilter != nil`, apply the same `blockFilter` to the descendant slice. The filter is a `domain.FilterExpr` — verify whether it has an `Evaluate(*domain.Task) bool` method; if so, walk the slice and keep matches. If the existing filter type has no in-memory evaluator, this is the place to add one (small helper, kept private to the service package; document in the implementation that this is the natural in-memory complement to the SQL evaluator the repository uses).
4. Build the `workflowFor` closure exactly as in `SummarizeSubtree` (resolve all distinct project IDs upfront).
5. Call `AggregateRollup` per block.
6. Sort blocks: urgency desc (which `List` already does for the `blockFilter != nil` path), short_id ascending as tiebreaker. For the `nil`-filter root-tasks path, score and sort manually using the same urgency pipeline `List` uses, or call `s.engine.ScoreAndSort` directly with a fresh scoring context (mirror the pattern from `listInBundle` at line 312).
7. Return `[]*domain.SummaryBlock`.

Note on the in-memory filter evaluator: if `domain.FilterExpr` does not currently support in-memory evaluation, this phase introduces such a helper. Place it at `domain/filter_eval.go` (new file) with signature `func EvalFilter(expr FilterExpr, t *Task) bool`. This is *not* bridge code — it is permanent infrastructure used in this and future features. Keep it minimal: only the term shapes the existing rollup test cases require. (Project, Status, Tags, Level, Priority, Parent/Tree are sufficient.) Document in the file header that the SQL-side evaluator in `sqlite/` is the authoritative one for top-level queries; this helper is for service-layer post-filtering.

Add unit tests in `service/task_test.go` (or a new `service/task_summary_test.go` if the existing test file is large):

- `TestSummarizeSubtree_Leaf` — single task, no children → `Rollup{0, 0, 0.0, []}`.
- `TestSummarizeSubtree_OneLevel` — root + 3 children, mixed statuses → expected Rollup.
- `TestSummarizeSubtree_DeepTree` — 4-level deep, mixed statuses, deleted descendants excluded.
- `TestSummarizeSubtree_NotFound` — unknown UUID → `domain.ErrNotFound`.
- `TestSummarizeBlocks_NilFilterReturnsRoots` — fixture with 2 root tasks, each with descendants → 2 blocks, urgency-sorted.
- `TestSummarizeBlocks_FilterScopesBoth` — filter selects level=story; blocks are stories; descendant counts include only story-level descendants under each.
- `TestSummarizeBlocks_FilterFull` — same fixture as previous, with `full=true` → blocks are stories; descendant counts include all subtree descendants regardless of level.
- `TestSummarizeBlocks_EmptyResult` — filter that matches nothing → empty slice, no error.

Use the existing `service/task_test.go` test harness pattern (mock repos via `repository/mock` if present, or a real SQLite test DB). Match the style of the surrounding tests.

## User-visible behaviors that must still work after this phase

- Every existing CLI command (`tusk task list`, `tusk task tree`, `tusk task get`, `tusk task next`, `tusk task pop`, etc.) produces byte-identical output to pre-Phase-1.
- Every existing MCP tool produces byte-identical responses to pre-Phase-1.
- `make build`, `make test`, `make test-race`, `make vet`, and `make lint` all pass.
- The new `domain.Rollup`, `domain.SummaryBlock`, and `domain.Summary` types compile and are reachable, but are not referenced by any existing CLI/MCP code path.
- `TaskService.SummarizeSubtree` and `TaskService.SummarizeBlocks` are exported and unit-tested but unused by the interface layer.

## Bridge code

None. Phase 1 introduces only additive surface — no flags, no new commands, no new tools. Later phases are the consumers.

## Changes Introduced

- **New files:**
  - `domain/rollup.go` — `StatusCount`, `Rollup`, `SummaryBlock`, `Summary` types + `AggregateRollup` function.
  - `domain/rollup_test.go` — table-driven aggregator tests.
  - `domain/filter_eval.go` (only if `domain.FilterExpr` lacks an in-memory evaluator today) — `EvalFilter(expr, *Task) bool` helper.
  - Possibly `service/task_summary_test.go` if the implementer judges `service/task_test.go` is too large to extend.
- **Modified files:**
  - `service/task.go` — adds `SummarizeSubtree` and `SummarizeBlocks` methods on `*TaskService`. No changes to existing methods.
  - `service/task_test.go` — adds the test cases listed in Task 1.5 (or place them in the new file if created).
- **Modified interfaces:** none (`TaskService` is a concrete type, not an interface, so no consumer must update; verify by grepping for `service.TaskService` to be sure no caller embeds an interface for it).
- **No new schema migrations, environment variables, or dependencies.**
