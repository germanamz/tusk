# Progress Rollup

**Branch:** TBD (suggested `feat/v0.13-progress-rollup`)
**Date:** 2026-04-25
**Status:** design approved, awaiting implementation plan
**Roadmap:** v0.13 — `ROADMAP.md` lines 1068–1082

## Goal

Static CLI summary views for per-subtree completion tracking. Two surfaces ship together:

1. `tusk task tree --rollup` — branch nodes in the tree get an inline `[done/total done, %]` and `(status: count, ...)` breakdown.
2. `tusk task summary` — single-id, filter, and workspace-wide rollup blocks; JSON output for agents; MCP tool mirroring the same data.

Both leverage the v0.9 status-role system (`done`, `terminal`, `delete`) so custom workflows work without configuration. Live dashboard rollup is deferred to v0.15.

## Non-goals

- No live updates. Rollups are computed on each invocation; the event log is not consulted.
- No SQL aggregation. The math runs in Go on the same task slices the existing filter pipeline materializes.
- No vocabulary baked in. The feature does not know about "epics", "stories", "milestones"; the level taxonomy is opaque to it.
- No new filter terms. `task summary` accepts the existing filter grammar verbatim.
- No customizable rollup formulas. Weighted progress is deferred to the future Urgency Profiles initiative.
- No `--rollup` flag on `task list` or other commands. Scope is exactly the tree view and the new `summary` command.

## Scope

### 1. Domain types and pure aggregator

Add `domain/rollup.go`:

```go
type StatusCount struct {
    Name  string `json:"name"`
    Count int    `json:"count"`
}

type Rollup struct {
    Done         int           `json:"done"`           // descendants whose status carries `done` role
    Total        int           `json:"total"`          // descendants whose status does NOT carry `delete` role
    Percent      float64       `json:"percent"`        // Done/Total, or 0.0 when Total == 0
    StatusCounts []StatusCount `json:"status_counts"`  // workflow order, zeros included, delete-role buckets excluded
}

type SummaryBlock struct {
    Task   *Task  `json:"task"`
    Rollup Rollup `json:"rollup"`
}

// AggregateRollup classifies each descendant against its own workflow.
// workflowFor returns the workflow that governs a task (typically project-keyed).
// Status order in the breakdown follows the workflow associated with the FIRST
// descendant encountered; statuses from later workflows are appended in
// first-seen order. Names that overlap across workflows merge into one bucket.
func AggregateRollup(descendants []*Task, workflowFor func(*Task) *Workflow) Rollup
```

Rules encoded by the aggregator:

- A task whose status carries the `delete` role in its own workflow is excluded entirely (does not contribute to `Total` or `StatusCounts`).
- A task whose status carries the `done` role in its own workflow contributes to both `Done` and `Total`, and to its status bucket in `StatusCounts`.
- Percent is `float64(Done) / float64(Total)` rounded to nearest integer at render time. JSON carries the unrounded float.
- `Total == 0` ⇒ `Percent == 0.0`. Renderers branch on `Total == 0` to emit `–%`.
- Buckets are keyed by status *name*, not by `(workflow, name)` — two workflows that both have `active` merge into one bucket so the user-visible label is what matters.

### 2. Service primitives

Add two methods to `service.TaskService` in `service/task.go`:

```go
// SummarizeSubtree rolls up a single task's strict descendants.
// All non-delete-role descendants are counted; no filter parameter.
SummarizeSubtree(
    ctx context.Context,
    rootID uuid.UUID,
) (*domain.SummaryBlock, error)

// SummarizeBlocks selects block tasks and rolls up each one's subtree.
//   blockFilter == nil  → blocks are root tasks (parent_id IS NULL)
//   full == false       → blockFilter ALSO restricts which descendants are counted
//   full == true        → blockFilter only selects blocks; full subtree counted
// Blocks are returned in urgency-desc order, short_id as tiebreaker.
SummarizeBlocks(
    ctx context.Context,
    blockFilter domain.FilterExpr,
    full bool,
) ([]*domain.SummaryBlock, error)
```

Composition: each method preloads a `projectID → *Workflow` map once, fetches descendants via existing repository paths (`GetDescendants` for single subtree; `List` for filter resolution + per-block `GetDescendants`), applies the descendant filter in Go when `SummarizeBlocks` runs without `full=true`, and calls `domain.AggregateRollup`. No new SQL.

### 3. `tusk task tree --rollup`

Add a `--rollup` boolean flag to the `tree` Cobra command (`internal/tui/commands.go`).

**Fetch decoupling.** When `--rollup` is set, the tree fetcher pulls the *full* subtree (no status filter) regardless of `--all`. `--all` controls rendering visibility; `--rollup` controls computation. This prevents underreporting when a non-deleted descendant has a delete-role ancestor that the default fetch would skip.

**Branch detection.** A node is a "branch" if it has at least one non-delete-role child *in the DB*, not just in the visible-rendered subset.

**Text rendering** (`internal/tui/tree.go`). Branch nodes append after the existing line:

```
{indent}{short_id} [{status}] {title} [level]  [3/5 done, 60%] (pending: 1, active: 1, completed: 3)
```

- Workflow order, every non-delete-role status included with zeros, comma-separated.
- Counts honor the workflow's `highlight`/`dim` role styling when color is enabled (bold for highlight, faint for dim) — same role mapping the existing TUI uses.
- Leaves render unchanged.
- `–%` printed when `total == 0`.

**JSON rendering.** Every node carries `rollup` when `--rollup` is set (leaves included with zero values), so downstream consumers have a uniform shape. When `--rollup` is not set, the JSON shape is unchanged from today.

**Compute path.** The handler walks the already-fetched subtree once and calls `domain.AggregateRollup(strict_descendants, workflowFor)` per branch node. No extra DB roundtrips.

### 4. `tusk task summary` CLI

New `summary` subcommand on `task` (`internal/tui/summary.go`, registered in `internal/tui/commands.go`).

```
tusk task summary [<short_id>] [filter...] [--full]
```

**Mode resolution:**

- **Single-id**: exactly one positional, matches the existing short_id pattern (no `=`/`+`/`-`/`..`/`:`). `--full` is rejected with a usage error.
- **Filter**: any other positional set, including bare tags (`+urgent`) or fielded terms (`level=story`). Reuses `filter.ParseExpr` + `Resolver.ResolveExpr` exactly as `task list` does.
- **Workspace-wide**: zero positionals. Treated as filter mode with `nil` filter; `SummarizeBlocks` interprets that as "blocks = root tasks." `--full` is meaningless here and rejected.

**Text rendering — single block:**

```
abc12345  Implement v0.13 milestone
  status:    active
  level:     milestone
  progress:  3/5 done, 60%
  breakdown: pending: 1, active: 1, completed: 3
```

Multi-line, label-padded — same shape `task get` already uses for a single task, extended with `progress` and `breakdown`.

**Text rendering — multi-block:**

```
abc12345  Implement v0.13 milestone
  status: active   level: milestone
  progress: 3/5 done, 60%
  breakdown: pending: 1, active: 1, completed: 3

b7c9d4e2  Subtree urgency overrides
  status: completed   level: initiative
  progress: 4/4 done, 100%
  breakdown: pending: 0, active: 0, completed: 4

────────────────────────────────────────
TOTALS    7/9 done, 78%
          pending: 1, active: 1, completed: 7
```

Blocks sorted urgency-desc with short_id as tiebreaker. Totals always emitted in multi-block modes (filter and roots), even when the filter returns one block, so the shape is predictable. Empty result: `No tasks matched.` with exit 0 (matches `task list`).

**JSON output (`--output json`):**

```json
{
  "mode": "filter",
  "blocks": [
    {"task": {...full task struct...}, "rollup": {...}},
    {"task": {...}, "rollup": {...}}
  ],
  "totals": {
    "done": 7, "total": 9, "percent": 0.78,
    "status_counts": [{"name": "pending", "count": 1}, ...]
  }
}
```

- `mode ∈ {"single", "filter", "roots"}`.
- `blocks` always present (single-element array in single-id mode).
- `totals` present in `filter` and `roots` modes; absent in `single` mode.
- `task` matches the same struct used by `tusk task get --output json`.

**Compute path.**

- Single-id: `TaskService.SummarizeSubtree(ctx, rootID)`.
- Filter: parse + resolve → `TaskService.SummarizeBlocks(ctx, expr, full)`.
- No-args: `TaskService.SummarizeBlocks(ctx, nil, false)`.

### 5. MCP `tusk_task_summary`

Register a new MCP tool (`internal/mcp/server.go`, handler in `internal/mcp/tools.go`).

**Tool surface mirrors `tusk_task_list`** — same vocabulary, same precedence semantics. Adds `short_id`, `level`, and `full` on top:

| Param | Type | Notes |
|---|---|---|
| `short_id` | string | Single-subtree mode. When set, all filter params are ignored. |
| `filter` | string | Filter expression. When set, structured filter params are ignored. |
| `status` | array<string> | Status names to include. |
| `priority_min`/`priority_max` | number | Priority range (0–4). |
| `project` | string | Project name. |
| `tags` / `exclude_tags` | array<string> | Include/exclude. |
| `due_after` / `due_before` | string | ISO 8601. |
| `parent` / `root` | string | Direct-children / subtree short_id. |
| `title` / `description` | string | Substring match. |
| `level` | string | New — matches the `level=` filter term. |
| `full` | boolean | Decouples block selection from descendant scope. Rejected in single-id mode. |
| `player_id` | string | Updates `last_seen_at`. |

**Param precedence:**

1. `short_id` set ⇒ single-id mode; other filter params ignored. `full` rejected.
2. `filter` set (no `short_id`) ⇒ parsed via the filter pipeline; structured params ignored. Honors `full`.
3. Structured filter params set (no `short_id`, no `filter`) ⇒ built into a `domain.TaskFilter` via the helper extracted from `handleTaskList`, wrapped in a `domain.FilterExpr`. Honors `full`.
4. Nothing set ⇒ workspace-wide (blocks = root tasks). `full` ignored.

**Conflict surfaces:**

- `short_id` + `full=true` → `INVALID_ARGUMENT: full is not valid in single-id mode`.
- `short_id` + filter params → silently ignored (matches `tusk_task_list` behavior).
- `filter` parse error → `INVALID_ARGUMENT` carrying the parser's error message.

**Response shape** is identical to the CLI JSON envelope in §4 — `{mode, blocks[], totals?}`. `task` fields are populated via the existing `s.projectNames(ctx)` resolution path.

**Filter helper extraction.** The translation `tusk_task_list` does today (flat params → `domain.TaskFilter`) is promoted to a private package helper (`buildTaskFilter`) so both `handleTaskList` and `handleTaskSummary` share one source of truth. The helper accepts a superset of the flat-param fields (including `level`); each tool's MCP registration controls which params actually reach it. `tusk_task_list`'s registration is unchanged in this initiative — extending it to expose `level` is out of scope.

**Visibility.** `tusk_task_summary` joins the existing tools allowlist alongside `tusk_task_list`.

## Files touched

| Phase | New | Modified |
|---|---|---|
| 1 | `domain/rollup.go`, `domain/rollup_test.go` | `service/task.go` (add `SummarizeSubtree`, `SummarizeBlocks`), `service/task_test.go` |
| 2 | `tests/e2e/tree_rollup_test.go` | `internal/tui/commands.go` (add `--rollup` flag), `internal/tui/tree.go` (fetch path + render) |
| 3 | `internal/tui/summary.go`, `internal/tui/summary_test.go`, `tests/e2e/summary_test.go` | `internal/tui/commands.go` (register `summary` subcommand) |
| 4 | `tests/e2e/mcp_summary_test.go` | `internal/mcp/server.go` (register tool, allowlist), `internal/mcp/tools.go` (`handleTaskSummary` + extract `buildTaskFilter`) |
| docs | `docs/releases/v0.13.md` (new entry) | `PRODUCT.md` — verify the existing Progress Rollup section still matches what shipped |

## Verification

**Unit:**
- `domain.AggregateRollup` table-driven over: empty input, all-done, all-deleted, all-pending, mixed-status, multi-workflow with name overlap, multi-workflow with disjoint names, workflow without `done` role, workflow without `delete` role.
- `service.TaskService.SummarizeSubtree`: leaf, single-level descendants, deep tree, mixed-status descendants, custom-workflow root.
- `service.TaskService.SummarizeBlocks`: nil filter ⇒ root tasks; filter ⇒ matching blocks; `full=false` vs `full=true`; urgency tiebreaker by short_id.

**E2E (`tests/e2e/`):**
- `tree --rollup` on a workspace with a deep tree, custom workflow, mixed statuses; `--rollup --all` for delete-role visibility; JSON envelope check.
- `task summary <id>` on a leaf (`0/0, –%`), branch, root.
- `task summary` no args with multiple roots, including totals line.
- `task summary level=story`, `task summary +urgent`, `task summary --full project=backend`.
- `task summary --full` paired with `<short_id>` ⇒ usage error.
- Custom-workflow project where `done` role is on a non-`completed` status: counts still flow correctly.
- MCP tool: each precedence path (`short_id`, `filter`, structured, none); `short_id + full` rejection; `filter` parse error surface; multi-workflow rollup envelope.

Reuses the existing 4-permutation harness (DB config × output format) for the e2e tests.

## Design decisions

| Decision | Choice | Rationale |
|---|---|---|
| Where the math lives | Pure `domain.AggregateRollup` + thin service composition | Reused by `tree --rollup` (already-fetched tasks) and `task summary` (service-fetched); a pure aggregator avoids forking the math. |
| In-memory vs SQL aggregation | In-memory | Filter pipeline is already Go-side; SQL aggregation would fork the rollup off the filter contract. Roadmap-scale workspaces don't justify the complexity. |
| Self-inclusion | Strict descendants only | Root's status answers a different question (its own state); rollup answers "the work below it." |
| Filter dual role | Filter scopes both block selection AND descendant counting by default | `level=story` means "give me a per-story rollup of stories under each story" naturally; degenerates cleanly when no filter is given. |
| `--full` escape hatch | Decouples descendant scope from block selection | Required to express "blocks at this level, full subtree counted" without sacrificing the dual-role default. |
| Status breakdown ordering | Workflow-defined order; zeros included; delete-role excluded | Stable visual width for diffability; matches the workflow's intended sequence; aligns with the spec's "leverages status roles" promise. |
| Multi-workflow buckets | Merge by status name | The user-visible label is what matters; `(workflow, name)` keying would surface a distinction users don't have a vocabulary for. |
| Tree `--rollup` fetch path | Always fetch full subtree when `--rollup` is set | Decouples `--all` (visibility) from `--rollup` (computation); prevents underreporting under delete-role ancestors. |
| MCP input shape | Mirror `tusk_task_list`: structured params + `filter` string escape hatch, "more-specific wins" precedence | Matches existing MCP precedent; agents stay in the structured-input regime by default; power cases remain expressible. |
| Workspace-wide totals line | Always emit in multi-block modes | Predictable shape across `roots` and `filter` modes; consumers don't have to branch on block count. |
| JSON envelope | `{mode, blocks[], totals?}` | `mode` lets consumers branch on intent; `totals` omitted in single-id mode (would just duplicate the block's rollup). |
| Filter helper extraction | Promote `handleTaskList`'s flat-params helper to shared `buildTaskFilter` | Keeps one source of truth for structured-param translation. |

## Out of scope

- Live dashboard rollup driven by event-log deltas — explicitly deferred to v0.15.
- Cross-team rollups across multiple workspaces — Teams initiative (later milestone).
- Customizable rollup formulas / weighted progress — Urgency Profiles initiative.
- `--rollup` on commands other than `tree` (e.g., `task list`).
- A filter on `tree --rollup`. The spec attaches the filter grammar to `task summary` only; adding it to tree is a separate UX decision.
- Caching. Each invocation recomputes from the DB.
