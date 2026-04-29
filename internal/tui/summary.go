package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/filter"
	"github.com/spf13/cobra"
)

// summaryMode classifies how a `tusk task summary` invocation maps positional
// arguments onto the underlying service call.
type summaryMode int

const (
	// summaryModeSingle: exactly one positional arg that parses as a short_id.
	summaryModeSingle summaryMode = iota
	// summaryModeFilter: any other positional set (filter expression).
	summaryModeFilter
	// summaryModeRoots: no positional args (workspace-wide rollup of roots).
	summaryModeRoots
)

// resolveSummaryMode classifies the positional arguments. A short_id is
// exactly 8 hex characters with no filter-syntax characters; anything else
// (including longer hex, mixed args, or single args containing `=`/`+`/`-`)
// is treated as a filter expression.
func resolveSummaryMode(args []string) (summaryMode, string) {
	switch len(args) {
	case 0:
		return summaryModeRoots, ""
	case 1:
		if isSummaryShortID(args[0]) {
			return summaryModeSingle, args[0]
		}
		return summaryModeFilter, ""
	default:
		return summaryModeFilter, ""
	}
}

// isSummaryShortID reports whether s looks like a canonical short_id (exactly
// 8 hex characters, lowercase or uppercase). Anything else — including
// strings containing filter-syntax characters — is treated as filter input.
func isSummaryShortID(str string) bool {
	if len(str) != 8 {
		return false
	}
	for i := 0; i < len(str); i++ {
		char := str[i]
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

// runSummary dispatches `tusk task summary` based on its positional args
// and the --full flag.
func (app *App) runSummary(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	full, _ := cmd.Flags().GetBool("full")

	mode, shortID := resolveSummaryMode(args)

	switch mode {
	case summaryModeSingle:
		if full {
			return fmt.Errorf("--full is not valid in single-id mode")
		}
		return app.runSummarySingle(ctx, cmd, shortID)
	case summaryModeRoots:
		if full {
			return fmt.Errorf("--full is not valid without a filter")
		}
		return app.runSummaryBlocks(ctx, cmd, nil, false)
	case summaryModeFilter:
		expr, err := app.parseSummaryFilter(ctx, args)
		if err != nil {
			return err
		}
		return app.runSummaryBlocks(ctx, cmd, expr, full)
	}
	return nil
}

// parseSummaryFilter parses positional args as a filter expression and
// resolves it against the workspace. Mirrors runList's pipeline so error
// messages stay consistent.
func (app *App) parseSummaryFilter(ctx context.Context, args []string) (domain.FilterExpr, error) {
	input := strings.Join(args, " ")
	expr, parseErrs := filter.ParseExpr(input)
	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("filter errors:\n%s", filter.FormatErrors(parseErrs))
	}
	if expr == nil {
		return nil, nil
	}
	resolved, resolveErrs := app.resolver.ResolveExpr(ctx, expr)
	if len(resolveErrs) > 0 {
		return nil, resolveErrs[0]
	}
	return resolved, nil
}

// runSummarySingle handles `tusk task summary <short_id>`.
func (app *App) runSummarySingle(ctx context.Context, cmd *cobra.Command, shortID string) error {
	task, taskErr := app.taskSvc.GetByShortID(ctx, shortID)

	if taskErr != nil {
		return fmt.Errorf("%s", formatError(taskErr, shortID))
	}

	block, blockErr := app.taskSvc.SummarizeSubtree(ctx, task.ID)

	if blockErr != nil {
		return blockErr
	}
	summary := &domain.Summary{
		Mode:   "single",
		Blocks: []*domain.SummaryBlock{block},
	}
	return app.renderSummary(cmd, summary)
}

// runSummaryBlocks handles roots and filter modes. expr == nil means roots.
func (app *App) runSummaryBlocks(ctx context.Context, cmd *cobra.Command, expr domain.FilterExpr, full bool) error {
	blocks, err := app.taskSvc.SummarizeBlocks(ctx, expr, full)
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
	return app.renderSummary(cmd, summary)
}

// computeTotals sums rollup counts across blocks. Returns the zero rollup
// (Total: 0, Percent: 0.0, StatusCounts: []) when blocks is empty.
// StatusCounts in the totals follow first-seen order across blocks; same
// merging rule as AggregateRollup. Always returns a non-nil pointer.
func computeTotals(blocks []*domain.SummaryBlock) *domain.Rollup {
	counts := make(map[string]int)
	order := make([]string, 0)
	seen := make(map[string]bool)

	var done, total int
	for _, block := range blocks {
		if block == nil {
			continue
		}
		done += block.Rollup.Done
		total += block.Rollup.Total
		for _, statusCount := range block.Rollup.StatusCounts {
			if !seen[statusCount.Name] {
				seen[statusCount.Name] = true
				order = append(order, statusCount.Name)
			}
			counts[statusCount.Name] += statusCount.Count
		}
	}

	statusCounts := make([]domain.StatusCount, 0, len(order))
	for _, name := range order {
		statusCounts = append(statusCounts, domain.StatusCount{Name: name, Count: counts[name]})
	}

	var percent float64
	if total > 0 {
		percent = float64(done) / float64(total)
	}

	return &domain.Rollup{
		Done:         done,
		Total:        total,
		Percent:      percent,
		StatusCounts: statusCounts,
	}
}

// renderSummary dispatches between text and JSON output based on the active
// format.
func (app *App) renderSummary(cmd *cobra.Command, summary *domain.Summary) error {
	if app.format == "json" {
		renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
		return renderer.renderSummaryJSON(summary)
	}
	return app.renderSummaryText(cmd, summary)
}

// renderSummaryText writes a text-mode summary. When the result has no
// blocks (filter or roots mode), it prints "No tasks matched." to stderr
// and returns without rendering totals.
func (app *App) renderSummaryText(cmd *cobra.Command, summary *domain.Summary) error {
	out := cmd.OutOrStdout()
	if len(summary.Blocks) == 0 {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "No tasks matched.")
		return err
	}

	renderer := app.newRenderer(cmd.Context(), out, app.buildDimStatuses())
	renderer.highlightStatuses = app.buildHighlightStatuses()
	for index, block := range summary.Blocks {
		if index > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if err := renderBlockText(out, renderer, block); err != nil {
			return err
		}
	}

	if summary.Totals != nil {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out, strings.Repeat("─", 40)); err != nil {
			return err
		}
		if err := renderTotalsText(out, renderer, summary.Totals); err != nil {
			return err
		}
	}
	return nil
}

// renderBlockText writes a single block as:
//
//	{short_id}  {title}
//	  status:    {status}
//	  level:     {level}        (omitted when the task has no level)
//	  progress:  {done}/{total} done, {pct}%
//	  breakdown: name: count, ... (omitted when StatusCounts is empty)
func renderBlockText(writer io.Writer, renderer *Renderer, block *domain.SummaryBlock) error {
	task := block.Task
	header := fmt.Sprintf("%s  %s", task.ShortID, task.Title)
	if renderer.styles != nil {
		header = renderer.styles.Header.Render(header)
	}
	if _, err := fmt.Fprintln(writer, header); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "  %-10s %s\n", "status:", task.Status); err != nil {
		return err
	}
	if task.Level != nil && *task.Level != "" {
		if _, err := fmt.Fprintf(writer, "  %-10s %s\n", "level:", *task.Level); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "  %-10s %s\n", "progress:", formatProgressLine(block.Rollup)); err != nil {
		return err
	}
	if len(block.Rollup.StatusCounts) > 0 {
		if _, err := fmt.Fprintf(writer, "  %-10s %s\n", "breakdown:", formatBreakdown(renderer, block.Rollup.StatusCounts)); err != nil {
			return err
		}
	}
	return nil
}

// renderTotalsText writes the totals line for filter/roots modes:
//
//	TOTALS    {done}/{total} done, {pct}%
//	          name: count, ...   (omitted when StatusCounts is empty)
func renderTotalsText(writer io.Writer, renderer *Renderer, totals *domain.Rollup) error {
	label := "TOTALS"
	if renderer.styles != nil {
		label = renderer.styles.Bold.Render(label)
	}
	if _, err := fmt.Fprintf(writer, "%s    %s\n", label, formatProgressLine(*totals)); err != nil {
		return err
	}
	if len(totals.StatusCounts) > 0 {
		if _, err := fmt.Fprintf(writer, "          %s\n", formatBreakdown(renderer, totals.StatusCounts)); err != nil {
			return err
		}
	}
	return nil
}

// formatProgressLine produces "{done}/{total} done, {pct}%" with the same
// "–%" rule as the tree branch decoration: dash percent when Total == 0.
func formatProgressLine(roll domain.Rollup) string {
	if roll.Total == 0 {
		return "0/0 done, –%"
	}
	pct := int(math.Round(roll.Percent * 100))
	return fmt.Sprintf("%d/%d done, %d%%", roll.Done, roll.Total, pct)
}

// formatBreakdown renders status counts as "name: count, name: count, ..."
// with bold/dim styling applied to highlight/dim role statuses.
func formatBreakdown(renderer *Renderer, counts []domain.StatusCount) string {
	parts := make([]string, 0, len(counts))
	for _, statusCount := range counts {
		seg := fmt.Sprintf("%s: %d", statusCount.Name, statusCount.Count)
		if renderer.styles != nil {
			switch {
			case renderer.isHighlightStatus(statusCount.Name):
				seg = renderer.styles.Bold.Render(seg)
			case renderer.isDimStatus(statusCount.Name):
				seg = renderer.styles.Dim.Render(seg)
			}
		}
		parts = append(parts, seg)
	}
	return strings.Join(parts, ", ")
}

// summaryJSON is the wire envelope for `tusk task summary --format json`.
type summaryJSON struct {
	Mode   string             `json:"mode"`
	Blocks []summaryBlockJSON `json:"blocks"`
	Totals *rollupJSON        `json:"totals,omitempty"`
}

// summaryBlockJSON pairs a task with its rollup. The task shape mirrors the
// taskJSON used by `task get`/`task list`, with project ID resolved to the
// project name.
type summaryBlockJSON struct {
	Task   taskJSON   `json:"task"`
	Rollup rollupJSON `json:"rollup"`
}

// renderSummaryJSON emits the summary envelope. Project IDs are resolved
// to project names through the renderer's cache, matching the shape
// `tusk task get --format json` uses.
func (renderer *Renderer) renderSummaryJSON(summary *domain.Summary) error {
	out := summaryJSON{
		Mode:   summary.Mode,
		Blocks: make([]summaryBlockJSON, 0, len(summary.Blocks)),
	}
	for _, block := range summary.Blocks {
		if block == nil {
			continue
		}
		out.Blocks = append(out.Blocks, summaryBlockJSON{
			Task:   renderer.toTaskJSON(block.Task, nil),
			Rollup: toRollupJSON(block.Rollup),
		})
	}
	if summary.Totals != nil {
		rollupJSON := toRollupJSON(*summary.Totals)
		out.Totals = &rollupJSON
	}
	enc := json.NewEncoder(renderer.w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
