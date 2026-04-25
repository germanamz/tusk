# Phase 2 — `tusk task tree --rollup`

**Spec:** `docs/superpowers/specs/2026-04-25-progress-rollup-design.md` §3
**Phase size:** 5 tasks

## Inherits From

After Phase 1, the codebase has:

- `domain.StatusCount`, `domain.Rollup`, `domain.SummaryBlock`, `domain.Summary` (in `domain/rollup.go`).
- `domain.AggregateRollup(descendants []*Task, workflowFor func(*Task) *Workflow) Rollup` — pure aggregator, fully unit-tested.
- `TaskService.SummarizeSubtree(ctx, rootID)` and `TaskService.SummarizeBlocks(ctx, blockFilter, full)` — composable rollup APIs (unused by Phase 2, but available).
- Possibly `domain.EvalFilter(expr, *Task) bool` — in-memory filter evaluator (unused by Phase 2).

The existing tree command (`tusk task tree`, `tusk task tree <short_id>`) renders a hierarchy in text or JSON, with `--all` (include deleted) and `--sort` (sibling sort key) flags. See `internal/tui/commands.go:145-166` (Cobra def), `internal/tui/tree.go:179-229` (`runTree` handler), `internal/tui/tree.go:236-244` (`fetchTreeTasks`), `internal/tui/tree.go:88-148` (text + JSON rendering), and `internal/tui/tree.go:16-58` (`treeNode`, `buildTree`).

## Prerequisites

Phase 1 must be complete and merged.

## Goal

Add a `--rollup` flag to `tusk task tree` that decorates branch nodes with `[done/total done, %]` and `(status: count, ...)` in text mode and adds a `rollup` field to every node in JSON mode. After this phase the rollup is consumable from the CLI but no `task summary` subcommand or MCP tool exists yet.

## Tasks

### Task 2.1 — Add the `--rollup` flag

In `internal/tui/commands.go` around line 167 (immediately after the existing `treeCmd.Flags().String("sort", ...)` line), add:

```go
treeCmd.Flags().Bool("rollup", false, "annotate branch nodes with descendant rollup stats")
```

Update the `treeCmd.Long` and `treeCmd.Example` blocks to mention the flag. A reasonable example to add:

```go
  # Show progress rollup on every branch node
  tusk task tree --rollup
```

No other changes in `commands.go`.

### Task 2.2 — Decouple the fetch path from `--all` when `--rollup` is set

In `internal/tui/tree.go`, modify `fetchTreeTasks` (currently lines 236-244) so it accepts the cobra command and inspects both `--all` and `--rollup`:

```go
func (a *App) fetchTreeTasks(ctx context.Context, cmd *cobra.Command, rootID *uuid.UUID) ([]*domain.Task, error) {
    showAll, _ := cmd.Flags().GetBool("all")
    rollup, _ := cmd.Flags().GetBool("rollup")

    filter := domain.TaskFilter{RootID: rootID}
    // When --rollup is on, fetch every status so the aggregator sees the
    // full subtree (delete-role tasks are excluded by the aggregator
    // itself, not by the fetch). Otherwise honor --all as today.
    if !showAll && !rollup {
        filter.Statuses = []string{"pending", "active", "completed"}
    }
    return a.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: filter})
}
```

The existing `runTree` handler at line 179 already passes the cobra command to `fetchTreeTasks`, so no signature change beyond the body.

**Critical**: `--rollup --all` and `--rollup` (without `--all`) MUST both fetch the same full set; the difference is purely *rendering*, addressed in Task 2.3. Without this decoupling, a non-deleted descendant whose ancestor is delete-role would be silently dropped from the fetch and the rollup numbers would underreport.

### Task 2.3 — Compute rollups and decorate nodes

The current rendering pipeline is: `runTree` → `buildTree` → `Renderer.renderTree` → `renderTreeNode` (text) or `toTreeNodeJSON` (JSON). Phase 2 splices the rollup computation between `buildTree` and the renderer.

**Where to compute.** In `runTree` (after `buildTree`, before `r.renderTree`), if `--rollup` is set:

1. Resolve the per-task workflow lookup. Walk the in-memory tree to collect distinct `ProjectID`s. Build a `map[uuid.UUID]*domain.Workflow` once (the workflow service is reachable as `a.workflowSvc` — verify this field name on `*App` in `internal/tui/app.go`; if it has a different name, use the actual one). The closure passed downstream looks like `func(t *domain.Task) *domain.Workflow { return wfMap[t.ProjectID] }`.
2. Walk each tree node bottom-up. For each node, collect the flat slice of strict descendants by traversing `node.Children` recursively into a `[]*domain.Task`. Call `domain.AggregateRollup(descendants, workflowFor)` and stash the result on the node.

**How to stash.** Extend `treeNode` in `internal/tui/tree.go:16-19` with an optional pointer field:

```go
type treeNode struct {
    Task     *domain.Task
    Children []*treeNode
    Rollup   *domain.Rollup // populated only when --rollup is set
}
```

Why a pointer: `nil` means "no rollup computed (flag off)". A zero-value `Rollup{}` is a legitimate state for a leaf in `--rollup` mode and must be distinguishable from "not computed."

Add a helper on `*App` (or as a free function in `tree.go`):

```go
// computeRollups walks the tree depth-first and assigns Rollup pointers
// to every node. workflowFor maps a task to its governing workflow.
// Pass --rollup mode only.
func computeRollups(nodes []*treeNode, workflowFor func(*domain.Task) *domain.Workflow) {
    for _, n := range nodes {
        descendants := flattenDescendants(n)
        r := domain.AggregateRollup(descendants, workflowFor)
        n.Rollup = &r
        computeRollups(n.Children, workflowFor)
    }
}

// flattenDescendants returns the strict descendants of n in a flat slice.
// The node's own task is NOT included.
func flattenDescendants(n *treeNode) []*domain.Task {
    var out []*domain.Task
    for _, c := range n.Children {
        out = append(out, c.Task)
        out = append(out, flattenDescendants(c)...)
    }
    return out
}
```

In `runTree`, after `nodes := buildTree(...)`, branch on the `--rollup` flag and call `computeRollups(nodes, workflowFor)`.

### Task 2.4 — Text rendering

In `internal/tui/tree.go`, modify `renderTreeNode` (currently lines 149-177). The current line build:

```go
line := fmt.Sprintf("%s%s [%s] %s", indent, node.Task.ShortID, node.Task.Status, node.Task.Title)
```

…stays as-is. After the existing `[level]` suffix block (which appends `[level]` when the project has a taxonomy), add a *new* suffix block for the rollup. Branch on:

- `node.Rollup != nil` — flag is on
- `len(node.Children) > 0` — node is a *visible* branch

But branch detection per the spec is "has at least one non-delete-role child *in the DB*", not "has visible children." Because Task 2.2 fetched the full subtree when `--rollup` is set, `node.Children` already reflects the full DB structure when the flag is on (it includes delete-role children too). So `len(node.Children) > 0` is the correct branch check in `--rollup` mode — it matches the spec semantics because the fetch was decoupled.

Append format (matching spec wording exactly):

```go
if node.Rollup != nil && len(node.Children) > 0 {
    rollupSuffix := r.formatRollup(*node.Rollup, node.Task.ProjectID)
    line += "  " + rollupSuffix
}
```

Add `formatRollup` as a Renderer method:

```go
// formatRollup renders the inline branch decoration for tree --rollup.
// projectID is used to look up workflow-defined highlight/dim role
// styling for the status buckets.
func (r *Renderer) formatRollup(roll domain.Rollup, projectID uuid.UUID) string {
    var pct string
    if roll.Total == 0 {
        pct = "–%"
    } else {
        pct = fmt.Sprintf("%d%%", int(math.Round(roll.Percent*100)))
    }
    progress := fmt.Sprintf("[%d/%d done, %s]", roll.Done, roll.Total, pct)

    parts := make([]string, 0, len(roll.StatusCounts))
    for _, sc := range roll.StatusCounts {
        seg := fmt.Sprintf("%s: %d", sc.Name, sc.Count)
        // If color is on, apply highlight/dim role styling for this
        // status. r.styles is nil when color is off.
        if r.styles != nil {
            if r.isHighlightStatus(sc.Name, projectID) {
                seg = r.styles.Bold.Render(seg)
            } else if r.isDimStatus(sc.Name) {
                seg = r.styles.Dim.Render(seg)
            }
        }
        parts = append(parts, seg)
    }
    breakdown := "(" + strings.Join(parts, ", ") + ")"

    return progress + " " + breakdown
}
```

Notes for the implementer:

- `r.isDimStatus` already exists (used in `renderTreeNode`). `r.isHighlightStatus` may or may not — verify in `internal/tui/styles.go`. If it does not, add it as a small helper that asks the workflow service whether the status carries the `highlight` role, mirroring the existing `dimStatuses` lookup pattern. The `Renderer` already has the dim-status set passed in from `runTree`; either pass a parallel `highlightStatuses` set in or call into the workflow service directly. Keep the implementation symmetric with what already exists for dim.
- `r.styles.Bold` may not exist — check `internal/tui/styles.go:16-24`. If only `Dim` and `Header` are defined, add a `Bold lipgloss.Style` field with `lipgloss.NewStyle().Bold(true)` and populate it in `newStyles`. Match the existing field-init pattern.
- `import "math"` at the top of `tree.go` for `math.Round`.
- Leaves render unchanged: the `len(node.Children) > 0` guard prevents the suffix on leaf nodes even when `node.Rollup != nil`.

### Task 2.5 — JSON rendering and e2e tests

**JSON rendering.** Extend `treeNodeJSON` in `internal/tui/tree.go:63-86` with an optional `Rollup` field:

```go
type treeNodeJSON struct {
    // ... existing fields ...
    Rollup *rollupJSON `json:"rollup,omitempty"`
    Children []treeNodeJSON `json:"children"`
}

type rollupJSON struct {
    Done         int               `json:"done"`
    Total        int               `json:"total"`
    Percent      float64           `json:"percent"`
    StatusCounts []statusCountJSON `json:"status_counts"`
}

type statusCountJSON struct {
    Name  string `json:"name"`
    Count int    `json:"count"`
}
```

Add helpers `toRollupJSON(domain.Rollup) rollupJSON` and (if not already present) wire it through `toTreeNodeJSON` (currently lines 84-124). When `node.Rollup != nil`, set `tj.Rollup = ptr(toRollupJSON(*node.Rollup))`. When nil, leave `tj.Rollup` nil so `omitempty` drops it from the output — JSON shape is unchanged when `--rollup` is not set.

When `--rollup` is set, every node — branch and leaf — gets a non-nil `Rollup`, including leaves with the zero rollup `{done:0, total:0, percent:0.0, status_counts:[]}`. JSON consumers thus see a uniform shape. Make sure `StatusCounts` is encoded as an empty JSON array `[]`, not `null`, when it is empty: initialize the slice in `toRollupJSON` to `make([]statusCountJSON, 0)` if `len(input) == 0`.

**E2E tests.** Create `tests/e2e/tree_rollup_test.go`. Mirror the patterns in `tests/e2e/hierarchy_test.go` (or whichever existing tree-related e2e test file is closest). Cover:

1. Tree with one root, three children (1 pending, 1 active, 1 completed). `tusk task tree --rollup` text output contains `[1/3 done, 33%]` and `(pending: 1, active: 1, completed: 1)` on the root line, and the children render unchanged. JSON output (`--output json`) has a `rollup` field on the root with `done:1, total:3, percent` ≈ `0.333`, and on each leaf has `rollup: {done:0, total:0, percent:0, status_counts:[]}`.
2. Same fixture, but mark one child as `deleted`. Without `--all`, default tree omits the deleted child from rendering. With `--rollup` (and no `--all`), the rollup says `[done/2 done]` (2 = total non-deleted descendants) — the deleted child is excluded from `Total` per the aggregator, and the deleted child does NOT render in text. With `--rollup --all`, the deleted child renders, but the rollup numbers are unchanged (delete-role still excluded).
3. Custom-workflow project where the `done` role lives on a status named `shipped` (not `completed`). Verify rollup counts honor the per-task workflow.
4. Empty subtree (a single root task with no children) — `tusk task tree --rollup <leaf>` renders the leaf with no rollup suffix in text; in JSON the leaf has `rollup: {done:0, total:0, percent:0, status_counts:[]}`.
5. JSON envelope shape stability: `tusk task tree` (no `--rollup`) emits the same JSON it did pre-Phase-2 (no `rollup` key anywhere).

Tests run through the existing harness (`tests/e2e/harness.go`), which executes each scenario in 4 permutations (DB config × output format). Filter format-specific assertions appropriately — the JSON checks gate on `format == "json"`.

## User-visible behaviors that must still work after this phase

- `tusk task tree` without `--rollup` renders byte-identical text and byte-identical JSON to pre-Phase-2.
- `tusk task tree --all` continues to include deleted tasks in rendering.
- `tusk task tree <short_id>` continues to scope to a subtree.
- `tusk task tree --sort urgency|order|created|priority|due` continues to control sibling order.
- `tusk task summary` is NOT yet a valid subcommand (Phase 3 introduces it). `tusk task summary` invocations should fail with the standard Cobra "unknown command" error.
- The MCP tool surface is unchanged — no `tusk_task_summary` registered yet.
- `make build`, `make test`, `make test-race`, `make vet`, and `make lint` all pass.

## Bridge code

None.

## Changes Introduced

- **New files:**
  - `tests/e2e/tree_rollup_test.go` — scenarios listed in Task 2.5.
- **Modified files:**
  - `internal/tui/commands.go` — adds `--rollup` flag to the `tree` command (line ~167); updates `Long`/`Example` strings.
  - `internal/tui/tree.go` — adds `Rollup *domain.Rollup` to `treeNode`; adds `computeRollups` and `flattenDescendants` helpers; modifies `fetchTreeTasks` to fetch full subtree when `--rollup` is set; modifies `renderTreeNode` to append rollup suffix on branch nodes; adds `formatRollup` Renderer method; adds `Rollup *rollupJSON` to `treeNodeJSON`; adds `rollupJSON`, `statusCountJSON` types; adds `toRollupJSON` helper; modifies `toTreeNodeJSON` to populate `tj.Rollup`. Adds `import "math"`.
  - `internal/tui/styles.go` — possibly adds a `Bold` style if not already present (verify before adding).
  - `internal/tui/render.go` or wherever `Renderer` lives — possibly adds a `highlightStatuses` field and `isHighlightStatus` method, mirroring the existing dim-status pattern (verify location and add only if not already present).
- **Modified interfaces:** none.
- **No new schema migrations, environment variables, or dependencies.**
