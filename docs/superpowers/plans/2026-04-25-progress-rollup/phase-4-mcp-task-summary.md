# Phase 4 — MCP `tusk_task_summary`

**Spec:** `docs/superpowers/specs/2026-04-25-progress-rollup-design.md` §5
**Phase size:** 5 tasks

## Inherits From

After Phase 1, the codebase has:

- `domain.StatusCount`, `domain.Rollup`, `domain.SummaryBlock`, `domain.Summary` (in `domain/rollup.go`).
- `domain.AggregateRollup` — pure aggregator.
- `TaskService.SummarizeSubtree(ctx, rootID)` and `TaskService.SummarizeBlocks(ctx, blockFilter, full)`.

After Phase 2, the codebase has:

- `tusk task tree --rollup` flag (text + JSON).

After Phase 3, the codebase has:

- `tusk task summary` subcommand (single, filter, roots modes; `--full` flag; text + JSON).
- The CLI's JSON envelope for summary: `{mode, blocks[], totals?}` — verified by `tests/e2e/summary_test.go`.

The existing MCP tool surface in `internal/mcp/`:

- Tool registration is in `internal/mcp/server.go` (e.g. `tusk_task_list` at line 283, `tusk_task_tree` at line 637).
- Tool allowlist for visibility validation is at `internal/mcp/server.go:112` (`validToolNames`).
- Handler implementations are in `internal/mcp/tools.go`. `handleTaskList` at line 449 is the canonical example of the hybrid filter pattern: it accepts a `filter` string param (parsed via `filter.ParseExpr` + `Resolver.ResolveExpr`) AND a flat set of structured params (`status`, `priority_min`, `priority_max`, `project`, `tags`, `exclude_tags`, `due_after`, `due_before`, `parent`, `root`, `title`, `description`). When `filter` is set, the structured params are ignored.
- Per-invocation helpers: `s.updatePlayerLiveness(ctx, request)` (player liveness), `s.projectNames(ctx)` (project ID → name cache), `toTaskResponse(t, tags, projectNames)` (task wire shape used by every task-returning tool).

## Prerequisites

Phase 1 must be complete and merged. Phase 3 must be complete and merged (Phase 4 reuses the JSON envelope shape and the precedence semantics established by the CLI).

## Goal

Add `tusk_task_summary` as an MCP tool that mirrors the CLI surface — same modes, same precedence, same JSON envelope. Extract the structured-param-to-`domain.TaskFilter` translation that lives inside `handleTaskList` into a shared helper so both tools call the same code. After this phase the rollup is fully consumable from MCP, and the initiative is feature-complete.

## Tasks

### Task 4.1 — Extract `buildTaskFilter` helper

**Goal:** isolate the structured-params-to-`domain.TaskFilter` translation currently inlined inside `handleTaskList` so `handleTaskSummary` can reuse it without copy-paste.

In `internal/mcp/tools.go`, add a new private helper near `handleTaskList`:

```go
// buildTaskFilter constructs a domain.TaskFilter from a request's
// structured filter params. It accepts a SUPERSET of the param keys any
// individual tool registration exposes — each tool's MCP registration
// (in server.go) controls which params are reachable. Unset / unknown
// params are silently no-ops.
//
// The accepted keys are:
//   status (string array), priority_min (number), priority_max (number),
//   project (string, resolved via taskSvc.ResolveProjectName),
//   tags (string array), exclude_tags (string array),
//   due_after (ISO 8601 string), due_before (ISO 8601 string),
//   parent (short_id string), root (short_id string),
//   title (substring string), description (substring string),
//   level (string).
//
// Returns the constructed filter, or an error if a referenced project
// cannot be resolved or a date string is malformed.
func (s *Server) buildTaskFilter(ctx context.Context, request mcp.CallToolRequest) (*domain.TaskFilter, error)
```

Implementation steps:

1. Read the existing `handleTaskList` body in `internal/mcp/tools.go:449-590` (approx). Identify the contiguous block that constructs `tf := domain.TaskFilter{}` from the structured params (starts around line 497 with `tf := domain.TaskFilter{}`). That block is the body to lift verbatim.
2. Move the block into `buildTaskFilter`. Wrap the existing one-by-one param reads (e.g. `request.GetStringSlice("status", nil)`, `request.RequireString("project")`, etc.) so they all live inside the helper.
3. Add a single new param read for `level`: `if level, err := request.RequireString("level"); err == nil { tf.Level = &level }`. Verify by reading `domain/filter.go` that `domain.TaskFilter` has a `Level *string` field; if not, this initiative does not add it (level filtering is already part of the v0.13 work — verify the field exists). If the field is genuinely absent, raise this in the post-implementation review; do not create the field in this phase.
4. In `handleTaskList`, replace the inlined block with a call to `s.buildTaskFilter(ctx, request)`. Preserve the early-return-on-`filter`-string guard at the top of the handler — that path must NOT call `buildTaskFilter`.
5. Verify `handleTaskList`'s behavior is byte-identical: run the existing MCP e2e tests (`tests/e2e/mcp_*_test.go`), confirm zero diff in responses.

This refactor is a Phase 4 prerequisite, not bridge code — it stays in the codebase permanently.

### Task 4.2 — Register `tusk_task_summary` and add to allowlist

In `internal/mcp/server.go`, add a tool registration block alongside `tusk_task_tree` (around line 637). Mirror the surface of `tusk_task_list` (line 283) and add `short_id`, `level`, and `full`:

```go
s.addTool("task",
    mcp.NewTool("tusk_task_summary",
        mcp.WithDescription("Summarize task progress with descendant rollups. "+
            "Returns one summary block per matching task, with done/total counts, "+
            "% done, and a status breakdown. Single-id mode summarizes one subtree; "+
            "filter mode picks blocks via filters; no args summarizes root tasks. "+
            "Tasks whose status carries the 'delete' role are excluded entirely. "+
            "The root of each block is NOT counted in its own rollup — the rollup "+
            "describes strict descendants only."),
        mcp.WithString("short_id",
            mcp.Description("Single-subtree mode: summarize this task. When set, all filter params are ignored and 'full' is rejected."),
        ),
        mcp.WithString("filter",
            mcp.Description("Filter expression with AND/OR/NOT/parentheses support. When set, structured filter params are ignored."),
        ),
        mcp.WithArray("status",
            mcp.Description("Filter by status name (e.g. [\"pending\", \"active\"])"),
            mcp.WithStringItems(),
        ),
        mcp.WithNumber("priority_min", mcp.Description("Minimum priority (0-4)")),
        mcp.WithNumber("priority_max", mcp.Description("Maximum priority (0-4)")),
        mcp.WithString("project", mcp.Description("Filter by project name")),
        mcp.WithArray("tags",
            mcp.Description("Include tasks with these tags"),
            mcp.WithStringItems(),
        ),
        mcp.WithArray("exclude_tags",
            mcp.Description("Exclude tasks with these tags"),
            mcp.WithStringItems(),
        ),
        mcp.WithString("due_after", mcp.Description("Tasks due after this ISO 8601 date")),
        mcp.WithString("due_before", mcp.Description("Tasks due before this ISO 8601 date")),
        mcp.WithString("parent", mcp.Description("Direct children of this task (short_id)")),
        mcp.WithString("root", mcp.Description("All descendants of this task (short_id)")),
        mcp.WithString("title", mcp.Description("Tasks whose title contains this substring (case-insensitive)")),
        mcp.WithString("description", mcp.Description("Tasks whose description contains this substring (case-insensitive)")),
        mcp.WithString("level", mcp.Description("Filter by level taxonomy name (e.g. \"story\")")),
        mcp.WithBoolean("full",
            mcp.Description("When true, the filter only selects blocks; descendants are counted across the full subtree under each block. Rejected in single-id mode."),
        ),
        mcp.WithString("player_id",
            mcp.Description("Player ID — updates last_seen_at if provided (no auto-register)"),
        ),
    ),
    s.handleTaskSummary,
)
```

In `internal/mcp/server.go:112`, add to `validToolNames`:

```go
"tusk_task_summary":    true,
```

Place the entry in the same block as `tusk_task_list`, alphabetized.

### Task 4.3 — Implement `handleTaskSummary` precedence and dispatch

In `internal/mcp/tools.go`, add `handleTaskSummary`. Pattern after `handleTaskList`:

```go
// handleTaskSummary handles the tusk_task_summary tool.
func (s *Server) handleTaskSummary(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    ctx = s.updatePlayerLiveness(ctx, request)

    // Single-id mode: short_id wins over everything.
    if shortID, err := request.RequireString("short_id"); err == nil && shortID != "" {
        if request.GetBool("full", false) {
            return mcp.NewToolResultError("full is not valid in single-id mode"), nil
        }
        task, err := s.taskSvc.GetByShortID(ctx, shortID)
        if err != nil {
            return toolError(err, shortID), nil
        }
        block, err := s.taskSvc.SummarizeSubtree(ctx, task.ID)
        if err != nil {
            return nil, err
        }
        return s.summaryToolResult(ctx, "single", []*domain.SummaryBlock{block}, nil)
    }

    full := request.GetBool("full", false)

    // Filter-string mode: parse + resolve, ignore structured params.
    if filterStr, err := request.RequireString("filter"); err == nil && filterStr != "" {
        expr, parseErrs := filter.ParseExpr(filterStr)
        if len(parseErrs) > 0 {
            return mcp.NewToolResultError("filter parse error: " + filter.FormatErrors(parseErrs)), nil
        }
        var filterExpr domain.FilterExpr
        if expr != nil {
            resolver := s.newResolver(ctx)
            var resolveErrs []error
            filterExpr, resolveErrs = resolver.ResolveExpr(ctx, expr)
            if len(resolveErrs) > 0 {
                return mcp.NewToolResultError(resolveErrs[0].Error()), nil
            }
        }
        blocks, err := s.taskSvc.SummarizeBlocks(ctx, filterExpr, full)
        if err != nil {
            return nil, err
        }
        return s.summaryToolResult(ctx, "filter", blocks, computeMCPTotals(blocks))
    }

    // Structured-params mode (or no params at all → roots).
    tf, err := s.buildTaskFilter(ctx, request)
    if err != nil {
        return toolError(err, ""), nil
    }
    var filterExpr domain.FilterExpr
    mode := "roots"
    if !isEmptyTaskFilter(tf) {
        filterExpr = &domain.TermFilter{TaskFilter: *tf}
        mode = "filter"
    }
    blocks, err := s.taskSvc.SummarizeBlocks(ctx, filterExpr, full)
    if err != nil {
        return nil, err
    }
    return s.summaryToolResult(ctx, mode, blocks, computeMCPTotals(blocks))
}
```

Helpers to add in the same file:

```go
// isEmptyTaskFilter reports whether tf has no fields set. Used to
// distinguish "filter mode with empty filter" (should pick blocks =
// every task) from "roots mode" (no filter at all → blocks = root
// tasks). Reads only the fields buildTaskFilter populates.
func isEmptyTaskFilter(tf *domain.TaskFilter) bool

// computeMCPTotals duplicates the CLI's computeTotals (Phase 3,
// internal/tui/summary.go) for the MCP layer. Walks blocks, sums Done
// and Total, computes Percent, merges StatusCounts by name preserving
// first-seen order. Always returns a non-nil *domain.Rollup so the
// envelope's "totals" field is consistently present in filter and
// roots modes.
func computeMCPTotals(blocks []*domain.SummaryBlock) *domain.Rollup
```

Note: `computeTotals` already exists in `internal/tui/summary.go` from Phase 3. Duplicating the small helper into `internal/mcp/tools.go` (renamed `computeMCPTotals`) is preferable to creating an `internal/...` shared package for this initiative — the function is tiny and the import direction (internal/mcp depending on internal/tui or vice versa) is fragile. If the implementer judges the duplication unacceptable, an alternative is to promote the helper to `domain` (it operates on `[]*domain.SummaryBlock` and produces a `*domain.Rollup`, no UI concerns). Either is acceptable; flag the choice in the PR description.

### Task 4.4 — Implement `summaryToolResult`

In `internal/mcp/tools.go`, add the response builder:

```go
// summaryToolResult builds the MCP JSON envelope for a summary response.
// The envelope shape mirrors the CLI's `{mode, blocks[], totals?}` —
// see internal/tui/summary.go and the spec §4.
//
// Each block's task is rendered through toTaskResponse (the same
// task-shaped wire type tusk_task_list, tusk_task_get, etc. emit), so
// MCP consumers see the same task fields they already know.
func (s *Server) summaryToolResult(
    ctx context.Context,
    mode string,
    blocks []*domain.SummaryBlock,
    totals *domain.Rollup,
) (*mcp.CallToolResult, error) {
    type rollupResponse struct {
        Done         int                 `json:"done"`
        Total        int                 `json:"total"`
        Percent      float64             `json:"percent"`
        StatusCounts []domain.StatusCount `json:"status_counts"`
    }
    type summaryBlockResponse struct {
        Task   taskResponse   `json:"task"`
        Rollup rollupResponse `json:"rollup"`
    }
    type summaryResponse struct {
        Mode   string                 `json:"mode"`
        Blocks []summaryBlockResponse `json:"blocks"`
        Totals *rollupResponse        `json:"totals,omitempty"`
    }

    // Resolve project names + tags for every block's task. Mirror how
    // handleTaskList loads tags in batch (single GetTaskTagsBatch call)
    // to avoid N+1.
    taskIDs := make([]uuid.UUID, len(blocks))
    for i, b := range blocks {
        taskIDs[i] = b.Task.ID
    }
    tagsByTask, err := s.tagSvc.GetTaskTagsBatch(ctx, taskIDs)
    if err != nil {
        return nil, err
    }
    names := s.projectNames(ctx)

    out := summaryResponse{
        Mode:   mode,
        Blocks: make([]summaryBlockResponse, len(blocks)),
    }
    for i, b := range blocks {
        out.Blocks[i] = summaryBlockResponse{
            Task:   toTaskResponse(b.Task, tagsByTask[b.Task.ID], names),
            Rollup: toRollupResponse(b.Rollup),
        }
    }
    if totals != nil {
        r := toRollupResponse(*totals)
        out.Totals = &r
    }
    return toolResultJSON(out)
}

func toRollupResponse(r domain.Rollup) rollupResponse {
    counts := r.StatusCounts
    if counts == nil {
        counts = []domain.StatusCount{}
    }
    return rollupResponse{
        Done:         r.Done,
        Total:        r.Total,
        Percent:      r.Percent,
        StatusCounts: counts,
    }
}
```

Critical detail: ensure `StatusCounts` serializes as `[]` not `null` when empty. The `if counts == nil` guard above forces an empty slice. The same trick must be applied to `Totals.StatusCounts` when totals exists with no blocks. JSON consumers (especially TypeScript clients) routinely choke on `null` arrays — this matters.

`rollupResponse` is type-scoped inside `summaryToolResult` here; it's fine to lift it to package level if Task 4.3's helpers need to reference it. Match whatever pattern is cleaner once you see the surrounding code.

### Task 4.5 — E2E tests and documentation pointer

Create `tests/e2e/mcp_summary_test.go`. Mirror the harness used by `tests/e2e/mcp_player_test.go` and `tests/e2e/mcp_task_queue_test.go` (the existing MCP-targeting e2e tests). Cover:

1. **Single-id mode.** Create a fixture (root + 3 children, mixed statuses). Call the tool with `short_id: <root-short-id>`. Assert response: `mode == "single"`, `blocks` has one element, `blocks[0].task.short_id == <root-short-id>`, `blocks[0].rollup.done` and `blocks[0].rollup.total` match the fixture, `totals` is absent (omitempty).
2. **Single-id mode + `full=true`.** Call with `short_id: X` and `full: true`. Assert error response: payload contains `"single-id mode"`.
3. **Filter-string mode.** Call with `filter: "level=story"`. Assert response: `mode == "filter"`, blocks are story-level tasks, `totals` populated. Assert structured params are ignored: same call but with `level: "task"` in the same request as `filter: "level=story"` — verify the output matches `level=story`, not `level=task`.
4. **Structured-params mode.** Call with `level: "initiative"` (no `filter` string, no `short_id`). Assert response: `mode == "filter"`, blocks are initiative-level tasks, `totals` populated.
5. **Roots mode.** Call with no params at all (only `player_id` optional). Assert response: `mode == "roots"`, blocks are root tasks (one per root), `totals` populated.
6. **Filter parse error.** Call with `filter: "level=story AND ("` (unclosed paren). Assert error response: payload contains `"filter parse error"`.
7. **Empty result.** Call with `level: "nonexistent"`. Assert response: `blocks: []`, `totals: {done:0, total:0, percent:0, status_counts: []}` (NOT `null` for status_counts).
8. **Custom-workflow project.** Same fixture as Phase 2's case 3 — verify the `done`-role lookup is per-task workflow.
9. **Tool visibility.** Verify `tusk_task_summary` appears in the tool list response from `tools/list` (the harness's MCP listing call).
10. **`tusk_task_list` regression check.** Run a known `tusk_task_list` scenario from the existing `tests/e2e/mcp_*` suite and confirm output is byte-identical to pre-Phase-4. The Task 4.1 refactor must not change `tusk_task_list`'s behavior.

Update the MCP server instructions block (currently in `internal/mcp/server.go` — search for the `s.SetInstructions(...)` call or equivalent) to mention the new tool. Match the existing one-line-per-tool style. Example sentence: "Summarize task progress with descendant rollups via tusk_task_summary."

## User-visible behaviors that must still work after this phase

- `tusk task tree` and `tusk task tree --rollup` unchanged.
- `tusk task summary` (single, filter, roots, `--full`) unchanged from Phase 3.
- `tusk task list`, `tusk task get`, `tusk task next`, `tusk task pop`, etc. unchanged.
- `tusk_task_list` MCP tool produces byte-identical responses to pre-Phase-4 (the `buildTaskFilter` extraction is behavior-preserving).
- Every other MCP tool — `tusk_task_get`, `tusk_task_create`, `tusk_task_modify`, `tusk_task_tree`, `tusk_task_next`, `tusk_task_pop`, `tusk_project_list`, `tusk_workflow_list`, etc. — produces byte-identical responses.
- `make build`, `make test`, `make test-race`, `make vet`, and `make lint` all pass.
- The MCP server, when started fresh, lists `tusk_task_summary` in `tools/list`.

## Bridge code

None.

## Changes Introduced

- **New files:**
  - `tests/e2e/mcp_summary_test.go` — scenarios listed in Task 4.5.
- **Modified files:**
  - `internal/mcp/server.go` — adds `"tusk_task_summary": true` to `validToolNames` (line 112 area); adds the `addTool("task", mcp.NewTool("tusk_task_summary", ...), s.handleTaskSummary)` registration block alongside `tusk_task_tree` (line 637 area); updates the `SetInstructions` block to mention the new tool.
  - `internal/mcp/tools.go` — adds `buildTaskFilter` (lifted from `handleTaskList`), refactors `handleTaskList` to call it, adds `handleTaskSummary`, `summaryToolResult`, `toRollupResponse`, `computeMCPTotals`, `isEmptyTaskFilter`, and possibly package-level `rollupResponse`/`summaryBlockResponse`/`summaryResponse` types if hoisted from the function body.
- **Modified interfaces:** none.
- **No new schema migrations, environment variables, or dependencies.**
