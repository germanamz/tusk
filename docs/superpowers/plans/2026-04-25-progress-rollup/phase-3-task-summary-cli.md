# Phase 3 — `tusk task summary` CLI

**Spec:** `docs/superpowers/specs/2026-04-25-progress-rollup-design.md` §4
**Phase size:** 5 tasks

## Inherits From

After Phase 1, the codebase has:

- `domain.StatusCount`, `domain.Rollup`, `domain.SummaryBlock`, `domain.Summary` (in `domain/rollup.go`).
- `domain.AggregateRollup` — pure aggregator.
- `TaskService.SummarizeSubtree(ctx, rootID)` — single subtree rollup.
- `TaskService.SummarizeBlocks(ctx, blockFilter, full)` — block-selecting rollup with optional full-subtree mode.
- Possibly `domain.EvalFilter(expr, *Task) bool` — in-memory filter evaluator.

After Phase 2, the codebase has:

- `tusk task tree --rollup` flag, with branch-decorated text output and uniform JSON rollup fields.
- `rollupJSON` and `statusCountJSON` types defined in `internal/tui/tree.go` — Phase 3 promotes these to a shared TUI location (`internal/tui/render.go` or equivalent) and reuses them. **Without Phase 2, these types do not exist.**
- A `Bold` lipgloss style on the `Renderer` (added by Phase 2 Task 2.4 if not already present), used by both `tree --rollup` and the totals line in `task summary`.
- An `isHighlightStatus` helper on the `Renderer` (added by Phase 2 Task 2.4 if not already present).
- The rollup pipeline is otherwise unchanged from Phase 1.

The existing CLI app pattern: each subcommand is a Cobra `*cobra.Command` registered in `internal/tui/commands.go`. Handlers live in dedicated files (e.g. `tree.go`, `list.go`). The `App` struct on `internal/tui/app.go` carries service references (`taskSvc`, `workflowSvc`, `projectSvc`, `resolver *filter.Resolver`, `format string`) and a renderer factory `newRenderer(...)`. The filter parse/resolve pipeline is wired at app construction (resolver from `filter.NewResolver(...)`), and `runList` (`internal/tui/commands.go:540-557`) is the canonical example of going from `[]string` positionals to a `domain.FilterExpr`.

## Prerequisites

Phase 1 AND Phase 2 must both be complete and merged. Phase 3's JSON renderer reuses the `rollupJSON` and `statusCountJSON` types introduced by Phase 2, and Phase 3's totals line uses the `Bold` style and `isHighlightStatus` helper Phase 2 may have added. Running Phase 3 against a Phase-1-only codebase will fail to compile.

## Goal

Add `tusk task summary [<short_id>] [filter...] [--full]` as a new subcommand that produces single-subtree, filter-scoped, or workspace-wide rollup blocks, in text or JSON. After this phase the rollup is consumable directly via `tusk task summary` from the CLI; no MCP tool exists yet.

## Tasks

### Task 3.1 — Register the subcommand and parse args into a mode

Create `internal/tui/summary.go`. Add the subcommand registration block in `internal/tui/commands.go` (alongside the existing `treeCmd` registration around line 145):

```go
summaryCmd := &cobra.Command{
    Use:   "summary [<short_id> | filter...]",
    Short: "Summarize task progress with descendant rollups",
    Long: `Summarize task progress as a rollup of descendants by status.

With a short_id, summarize that task's subtree.
With filter terms, one block per matching task. The filter restricts both
which tasks become blocks and which descendants are counted, unless
--full is passed (in which case the filter only selects blocks).
With no arguments, summarize each root task plus a totals line.`,
    Example: `  # Single subtree
  tusk task summary a3f8b2c1

  # All root tasks (workspace-wide)
  tusk task summary

  # One block per story; counts limited to story-level descendants
  tusk task summary level=story

  # One block per initiative; counts include the full subtree under each
  tusk task summary --full level=initiative`,
    Args: cobra.ArbitraryArgs,
    RunE: a.runSummary,
}
summaryCmd.Flags().Bool("full", false, "with a filter, count the full subtree under each block (otherwise the filter restricts descendant counting too)")

taskGroup.AddCommand(summaryCmd) // verify the actual parent is `taskGroup` — match the existing tree/list registration sites
```

In `internal/tui/summary.go`, define mode resolution:

```go
type summaryMode int

const (
    summaryModeSingle summaryMode = iota
    summaryModeFilter
    summaryModeRoots
)

// resolveSummaryMode classifies positionals.
//   - exactly one positional and it parses as a short_id (matches the
//     existing short_id pattern: 8 hex chars, no '=', '+', '-', '..', ':')
//     ⇒ summaryModeSingle.
//   - any other positional set ⇒ summaryModeFilter.
//   - zero positionals ⇒ summaryModeRoots.
func resolveSummaryMode(args []string) (summaryMode, string)
```

The short_id pattern is already validated elsewhere — search for `isShortID` or similar in `internal/tui/` (e.g. how `task get` handles it). Reuse that helper. Specifically, the canonical short_id check is "exactly 8 hex chars and no filter-syntax characters" — the implementer should grep for any existing helper before adding a new one.

`runSummary` skeleton (in `internal/tui/summary.go`):

```go
func (a *App) runSummary(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()
    full, _ := cmd.Flags().GetBool("full")

    mode, shortID := resolveSummaryMode(args)

    switch mode {
    case summaryModeSingle:
        if full {
            return fmt.Errorf("--full is not valid in single-id mode")
        }
        return a.runSummarySingle(ctx, cmd, shortID)
    case summaryModeRoots:
        if full {
            return fmt.Errorf("--full is not valid without a filter")
        }
        return a.runSummaryBlocks(ctx, cmd, nil, false)
    case summaryModeFilter:
        expr, err := a.parseSummaryFilter(ctx, args)
        if err != nil {
            return err
        }
        return a.runSummaryBlocks(ctx, cmd, expr, full)
    }
    return nil
}
```

`parseSummaryFilter` is a thin wrapper: call `filter.ParseExpr(strings.Join(args, " "))`, format any parse errors with `filter.FormatErrors`, then call `a.resolver.ResolveExpr(ctx, parsedExpr)` — exactly the pipeline `runList` uses. Match `runList`'s error wrapping verbatim so error messages are consistent across the two commands.

### Task 3.2 — Single-id and filter handlers

Add to `internal/tui/summary.go`:

```go
func (a *App) runSummarySingle(ctx context.Context, cmd *cobra.Command, shortID string) error {
    task, err := a.taskSvc.GetByShortID(ctx, shortID)
    if err != nil {
        return fmt.Errorf("%s", formatError(err, shortID))
    }
    block, err := a.taskSvc.SummarizeSubtree(ctx, task.ID)
    if err != nil {
        return err
    }
    summary := &domain.Summary{
        Mode:   "single",
        Blocks: []*domain.SummaryBlock{block},
        // Totals intentionally nil for single mode — would just duplicate the block's rollup.
    }
    return a.renderSummary(cmd, summary)
}

func (a *App) runSummaryBlocks(ctx context.Context, cmd *cobra.Command, expr domain.FilterExpr, full bool) error {
    blocks, err := a.taskSvc.SummarizeBlocks(ctx, expr, full)
    if err != nil {
        return err
    }
    mode := "filter"
    if expr == nil {
        mode = "roots"
    }
    summary := &domain.Summary{
        Mode:   mode,
        Blocks: blocks,
        Totals: computeTotals(blocks),
    }
    return a.renderSummary(cmd, summary)
}
```

Add `computeTotals`:

```go
// computeTotals sums rollup counts across blocks. Returns the zero rollup
// (Total: 0, Percent: 0.0, StatusCounts: []) when blocks is empty.
// StatusCounts in the totals follow first-seen order across blocks; same
// merging rule as AggregateRollup.
func computeTotals(blocks []*domain.SummaryBlock) *domain.Rollup
```

Implementation: walk `blocks`, sum `Done` and `Total`, compute `Percent`, merge `StatusCounts` by name preserving first-seen order. Always returns a non-nil `*domain.Rollup` (so the "filter" and "roots" mode envelope always has populated `Totals`, even when blocks is empty — the JSON consumer sees `totals: {done:0, total:0, percent:0, status_counts:[]}`).

`computeTotals` lives in `internal/tui/summary.go` (private to the package). It is *not* a candidate for promotion to `domain` — it duplicates a small subset of `AggregateRollup`'s merging logic but operates on already-aggregated `Rollup` values, which is a different shape from `[]*Task`.

### Task 3.3 — Text rendering

Add to `internal/tui/summary.go`:

```go
func (a *App) renderSummary(cmd *cobra.Command, summary *domain.Summary) error {
    if a.format == "json" {
        return renderSummaryJSON(cmd.OutOrStdout(), summary)
    }
    return a.renderSummaryText(cmd, summary)
}

func (a *App) renderSummaryText(cmd *cobra.Command, summary *domain.Summary) error {
    w := cmd.OutOrStdout()
    if len(summary.Blocks) == 0 {
        _, err := fmt.Fprintln(cmd.ErrOrStderr(), "No tasks matched.")
        return err
    }

    r := a.newRenderer(cmd.Context(), w, a.buildDimStatuses())
    for i, block := range summary.Blocks {
        if i > 0 {
            fmt.Fprintln(w)
        }
        renderBlockText(w, r, block)
    }

    if summary.Totals != nil {
        fmt.Fprintln(w)
        fmt.Fprintln(w, strings.Repeat("─", 40))
        renderTotalsText(w, r, summary.Totals)
    }
    return nil
}
```

`renderBlockText` produces:

```
abc12345  Implement v0.13 milestone
  status:    active
  level:     milestone
  progress:  3/5 done, 60%
  breakdown: pending: 1, active: 1, completed: 3
```

Implementation:

- First line: `{short_id}  {title}` (two spaces between). Apply existing `r.styles.Header` if present (mirror how `task get` styles its header — verify in `internal/tui/get.go` or the equivalent file).
- Subsequent lines: 2-space indent, label-padded. Use `r.paddedLabel("status:", value)` if such a helper exists (Explore mapped it to `internal/tui/styles.go:90-121` as `paddedLabel`). Otherwise format manually with `fmt.Sprintf("  %-10s %s\n", "status:", ...)`.
- The `level:` line is omitted entirely when the block's task has no level (i.e. `Task.Level == nil` or empty string — match what `task get` does).
- The `progress:` line uses the same `–%` rule as the tree: `progress: 3/5 done, 60%` when `Total > 0`, `progress: 0/0 done, –%` when `Total == 0`.
- The `breakdown:` line lists `name: count` pairs separated by `, `. When `len(StatusCounts) == 0` (delete-only or empty subtree), omit the `breakdown:` line entirely.

`renderTotalsText` produces:

```
TOTALS    7/9 done, 78%
          pending: 1, active: 1, completed: 7
```

Implementation:

- First line: `TOTALS` (uppercase, possibly bold via `r.styles.Bold` if added in Phase 2 — otherwise plain), 4-space gap, `{done}/{total} done, {pct}%` formatted identically to the per-block progress line.
- Second line: 10-space lead-in, status counts joined by `, `. Omit if `len(StatusCounts) == 0`.
- The horizontal rule above (40 `─` chars) is rendered by `renderSummaryText` itself; `renderTotalsText` only writes the two lines.

When the result is empty in single mode (cannot happen — `GetByShortID` errors first), no special handling needed. When empty in filter or roots mode, `renderSummaryText` already prints `No tasks matched.` and returns early; `Totals` (which is non-nil but zero) is not rendered. That matches the `task list` empty-result behavior (exit 0, message to stderr).

### Task 3.4 — JSON rendering

Add to `internal/tui/summary.go`:

```go
func renderSummaryJSON(w io.Writer, summary *domain.Summary) error {
    enc := json.NewEncoder(w)
    enc.SetIndent("", "  ")
    return enc.Encode(toSummaryJSON(summary))
}

// summaryJSON, summaryBlockJSON, rollupJSON, statusCountJSON mirror the
// domain types but with project IDs resolved to project names on each
// block's Task, matching the shape `tusk task get --output json` uses.
type summaryJSON struct {
    Mode   string             `json:"mode"`
    Blocks []summaryBlockJSON `json:"blocks"`
    Totals *rollupJSON        `json:"totals,omitempty"`
}

type summaryBlockJSON struct {
    Task   taskJSON   `json:"task"`
    Rollup rollupJSON `json:"rollup"`
}
```

`taskJSON` already exists (`internal/tui/render.go`, used by `task get` and `task list`). Reuse it. `rollupJSON` and `statusCountJSON` were introduced by Phase 2 in `internal/tui/tree.go` — move them to `internal/tui/render.go` (the natural home for shared TUI wire types) so both `tree.go` and `summary.go` import them from one location. Update `tree.go`'s references after the move; verify `make build` is green before moving on.

Implementation: walk `summary.Blocks`, build `taskJSON` for each `block.Task` via the existing `toTaskJSON(...)` helper (or whatever it is named — match `task list`'s usage). Build `rollupJSON` via a small `toRollupJSON` helper that converts `domain.Rollup` to the wire shape, ensuring `StatusCounts` serializes as `[]` not `null` when empty. `Totals` is `nil` in single mode (omitempty drops it), non-nil in filter/roots mode (always emitted, even when zero).

JSON envelope shape (verify against the spec):

```json
{
  "mode": "filter",
  "blocks": [
    {"task": { ... }, "rollup": { "done": 3, "total": 5, "percent": 0.6, "status_counts": [...] }}
  ],
  "totals": { "done": 7, "total": 9, "percent": 0.78, "status_counts": [...] }
}
```

### Task 3.5 — Unit and e2e tests

**Unit tests** in `internal/tui/summary_test.go`:

- `Test_resolveSummaryMode` — table-driven over inputs: `[]` → roots; `["a3f8b2c1"]` → single; `["level=story"]` → filter; `["+urgent"]` → filter; `["a3f8b2c1", "level=story"]` → filter (more than one positional is always filter mode); `["bogus"]` (8 chars, but not hex) → filter (not a short_id); `["AAAA1234"]` (uppercase hex) — match what `isShortID` accepts; `["12345678"]` (8 digits) → single (matches hex pattern).
- `Test_computeTotals` — empty input → zero rollup; one block → identical numbers; multiple blocks → summed Done/Total; status buckets merge by name preserving first-seen order; same-name buckets across blocks combine counts.
- Optional: `Test_renderBlockText` — golden-string assertion on a small fixture so a layout regression surfaces in unit tests not just e2e.

**E2E tests** in `tests/e2e/summary_test.go`. Mirror the harness pattern from `tests/e2e/hierarchy_test.go`:

1. Single-id mode on a leaf: `tusk task summary <leaf-short-id>` → text mode shows `progress: 0/0 done, –%`; JSON has `mode: "single"`, single-element `blocks`, no `totals`.
2. Single-id mode on a branch: `tusk task summary <root-short-id>` → text shows the multi-line block with the right counts; JSON shape matches.
3. No-args mode with two root tasks each with descendants → text shows two blocks, separator, and totals line; JSON has `mode: "roots"`, two-element `blocks`, populated `totals`.
4. Filter mode with `level=story`: `tusk task summary level=story` → blocks are story-level tasks; counts limited to story-level descendants. Verify both text and JSON.
5. Filter + `--full`: same fixture, `tusk task summary --full level=story` → blocks are stories; counts include all subtree descendants regardless of level. Verify the difference vs case 4 by counting deltas.
6. `tusk task summary --full <short-id>` → exits non-zero with usage error containing "single-id mode".
7. `tusk task summary --full` (no args) → exits non-zero with usage error containing "without a filter".
8. Empty filter result: `tusk task summary level=nonexistent` → text mode prints `No tasks matched.` to stderr and exits 0; JSON mode emits `{mode: "filter", blocks: [], totals: {done:0, total:0, percent:0, status_counts: []}}`.
9. Custom-workflow project where the `done` role lives on a non-`completed` status — counts still flow correctly through the same code path.

The e2e harness runs every scenario in 4 permutations (DB config × output format), so you only need to write each scenario once and let the harness handle the cross-product.

## User-visible behaviors that must still work after this phase

- `tusk task tree`, `tusk task tree --rollup`, `tusk task tree --all`, `tusk task tree <short_id>`, `tusk task tree --sort ...` all behave identically to post-Phase-2.
- `tusk task list`, `tusk task get`, `tusk task next`, `tusk task pop` all unchanged.
- `tusk task summary --help` produces the expected synopsis and example block.
- The MCP tool surface is unchanged — no `tusk_task_summary` registered yet.
- `make build`, `make test`, `make test-race`, `make vet`, and `make lint` all pass.

## Bridge code

None.

## Changes Introduced

- **New files:**
  - `internal/tui/summary.go` — subcommand handler (`runSummary`, `runSummarySingle`, `runSummaryBlocks`), mode resolver, text and JSON renderers, `computeTotals`.
  - `internal/tui/summary_test.go` — `resolveSummaryMode`, `computeTotals`, optional golden render tests.
  - `tests/e2e/summary_test.go` — scenarios listed in Task 3.5.
- **Modified files:**
  - `internal/tui/commands.go` — adds `summaryCmd` registration in the `task` group; registers `--full` flag.
  - `internal/tui/render.go` — hosts `rollupJSON` and `statusCountJSON` after the move from `tree.go`.
  - `internal/tui/tree.go` — removes the now-shared `rollupJSON`/`statusCountJSON` definitions; imports them from `render.go` (or the same package, no import needed since both are in `package tui`). Verify `toRollupJSON` and any other helpers move alongside if they were defined next to the types.
- **Modified interfaces:** none.
- **No new schema migrations, environment variables, or dependencies.**
