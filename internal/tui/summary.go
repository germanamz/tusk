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
func isSummaryShortID(s string) bool {
	if len(s) != 8 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// runSummary dispatches `tusk task summary` based on its positional args
// and the --full flag.
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

// parseSummaryFilter parses positional args as a filter expression and
// resolves it against the workspace. Mirrors runList's pipeline so error
// messages stay consistent.
func (a *App) parseSummaryFilter(ctx context.Context, args []string) (domain.FilterExpr, error) {
	input := strings.Join(args, " ")
	expr, parseErrs := filter.ParseExpr(input)
	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("filter errors:\n%s", filter.FormatErrors(parseErrs))
	}
	if expr == nil {
		return nil, nil
	}
	resolved, resolveErrs := a.resolver.ResolveExpr(ctx, expr)
	if len(resolveErrs) > 0 {
		return nil, resolveErrs[0]
	}
	return resolved, nil
}

// runSummarySingle handles `tusk task summary <short_id>`.
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
	}
	return a.renderSummary(cmd, summary)
}

// runSummaryBlocks handles roots and filter modes. expr == nil means roots.
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

// computeTotals sums rollup counts across blocks. Returns the zero rollup
// (Total: 0, Percent: 0.0, StatusCounts: []) when blocks is empty.
// StatusCounts in the totals follow first-seen order across blocks; same
// merging rule as AggregateRollup. Always returns a non-nil pointer.
func computeTotals(blocks []*domain.SummaryBlock) *domain.Rollup {
	counts := make(map[string]int)
	order := make([]string, 0)
	seen := make(map[string]bool)

	var done, total int
	for _, b := range blocks {
		if b == nil {
			continue
		}
		done += b.Rollup.Done
		total += b.Rollup.Total
		for _, sc := range b.Rollup.StatusCounts {
			if !seen[sc.Name] {
				seen[sc.Name] = true
				order = append(order, sc.Name)
			}
			counts[sc.Name] += sc.Count
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
func (a *App) renderSummary(cmd *cobra.Command, summary *domain.Summary) error {
	if a.format == "json" {
		r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
		return r.renderSummaryJSON(summary)
	}
	return a.renderSummaryText(cmd, summary)
}

// renderSummaryText writes a text-mode summary. When the result has no
// blocks (filter or roots mode), it prints "No tasks matched." to stderr
// and returns without rendering totals.
func (a *App) renderSummaryText(cmd *cobra.Command, summary *domain.Summary) error {
	w := cmd.OutOrStdout()
	if len(summary.Blocks) == 0 {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "No tasks matched.")
		return err
	}

	r := a.newRenderer(cmd.Context(), w, a.buildDimStatuses())
	r.highlightStatuses = a.buildHighlightStatuses()
	for i, block := range summary.Blocks {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := renderBlockText(w, r, block); err != nil {
			return err
		}
	}

	if summary.Totals != nil {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, strings.Repeat("─", 40)); err != nil {
			return err
		}
		if err := renderTotalsText(w, r, summary.Totals); err != nil {
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
func renderBlockText(w io.Writer, r *Renderer, block *domain.SummaryBlock) error {
	t := block.Task
	header := fmt.Sprintf("%s  %s", t.ShortID, t.Title)
	if r.styles != nil {
		header = r.styles.Header.Render(header)
	}
	if _, err := fmt.Fprintln(w, header); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  %-10s %s\n", "status:", t.Status); err != nil {
		return err
	}
	if t.Level != nil && *t.Level != "" {
		if _, err := fmt.Fprintf(w, "  %-10s %s\n", "level:", *t.Level); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "  %-10s %s\n", "progress:", formatProgressLine(block.Rollup)); err != nil {
		return err
	}
	if len(block.Rollup.StatusCounts) > 0 {
		if _, err := fmt.Fprintf(w, "  %-10s %s\n", "breakdown:", formatBreakdown(r, block.Rollup.StatusCounts)); err != nil {
			return err
		}
	}
	return nil
}

// renderTotalsText writes the totals line for filter/roots modes:
//
//	TOTALS    {done}/{total} done, {pct}%
//	          name: count, ...   (omitted when StatusCounts is empty)
func renderTotalsText(w io.Writer, r *Renderer, totals *domain.Rollup) error {
	label := "TOTALS"
	if r.styles != nil {
		label = r.styles.Bold.Render(label)
	}
	if _, err := fmt.Fprintf(w, "%s    %s\n", label, formatProgressLine(*totals)); err != nil {
		return err
	}
	if len(totals.StatusCounts) > 0 {
		if _, err := fmt.Fprintf(w, "          %s\n", formatBreakdown(r, totals.StatusCounts)); err != nil {
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
func formatBreakdown(r *Renderer, counts []domain.StatusCount) string {
	parts := make([]string, 0, len(counts))
	for _, sc := range counts {
		seg := fmt.Sprintf("%s: %d", sc.Name, sc.Count)
		if r.styles != nil {
			switch {
			case r.isHighlightStatus(sc.Name):
				seg = r.styles.Bold.Render(seg)
			case r.isDimStatus(sc.Name):
				seg = r.styles.Dim.Render(seg)
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
func (r *Renderer) renderSummaryJSON(summary *domain.Summary) error {
	out := summaryJSON{
		Mode:   summary.Mode,
		Blocks: make([]summaryBlockJSON, 0, len(summary.Blocks)),
	}
	for _, b := range summary.Blocks {
		if b == nil {
			continue
		}
		out.Blocks = append(out.Blocks, summaryBlockJSON{
			Task:   r.toTaskJSON(b.Task, nil),
			Rollup: toRollupJSON(b.Rollup),
		})
	}
	if summary.Totals != nil {
		rj := toRollupJSON(*summary.Totals)
		out.Totals = &rj
	}
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
