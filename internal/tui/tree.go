package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/filter"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// treeNode represents a task in the tree with its children.
// Rollup is populated only when the caller asked for `--rollup`; nil means
// "rollup not computed". A zero-value *Rollup is a legitimate state for a
// leaf in --rollup mode and must stay distinguishable from "not computed",
// hence the pointer.
type treeNode struct {
	Task     *domain.Task
	Children []*treeNode
	Rollup   *domain.Rollup
}

// buildTree constructs a tree from a flat list of tasks.
// If rootID is non-nil, only that task is treated as the root.
// If rootID is nil, all tasks with no parent (or whose parent is not in the set) are roots.
// Children at each level are kept in the order they appear in the input slice.
func buildTree(tasks []*domain.Task, rootID *uuid.UUID) []*treeNode {
	if len(tasks) == 0 {
		return nil
	}

	// Index tasks by ID
	byID := make(map[uuid.UUID]*treeNode, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = &treeNode{Task: task}
	}

	// Group children under parents
	var roots []*treeNode
	for _, task := range tasks {
		node := byID[task.ID]
		if rootID != nil && task.ID == *rootID {
			roots = append(roots, node)
			continue
		}
		if task.ParentID != nil {
			if parent, ok := byID[*task.ParentID]; ok {
				parent.Children = append(parent.Children, node)
				continue
			}
		}
		// No parent or parent not in set — this is a root (unless rootID mode)
		if rootID == nil {
			roots = append(roots, node)
		}
	}

	return roots
}

// treeNodeJSON is the JSON serialization format for a tree node.
// It includes all task fields (matching taskJSON in render.go) plus a children array.
type treeNodeJSON struct {
	ID                      string                `json:"id"`
	ShortID                 string                `json:"short_id"`
	ParentID                *string               `json:"parent_id"`
	ProjectID               string                `json:"project_id"`
	Title                   string                `json:"title"`
	Description             string                `json:"description"`
	Status                  string                `json:"status"`
	Priority                int                   `json:"priority"`
	Order                   *float64              `json:"order,omitempty"`
	Version                 int                   `json:"version"`
	DueAt                   *string               `json:"due_at,omitempty"`
	WaitUntil               *string               `json:"wait_until,omitempty"`
	RecurrenceRule          *string               `json:"recurrence_rule,omitempty"`
	UDA                     map[string]any        `json:"uda,omitempty"`
	CreatedAt               string                `json:"created_at"`
	ModifiedAt              string                `json:"modified_at"`
	UrgencyOverrides        *urgencyOverridesJSON `json:"urgency_overrides,omitempty"`
	EffectiveUrgencyWeights *urgencyWeightsJSON   `json:"effective_urgency_weights,omitempty"`
	Rollup                  *rollupJSON           `json:"rollup,omitempty"`
	Children                []treeNodeJSON        `json:"children"`
}

// toTreeNodeJSON converts a treeNode to its JSON representation recursively.
func (renderer *Renderer) toTreeNodeJSON(node *treeNode) treeNodeJSON {
	task := node.Task
	tj := treeNodeJSON{
		ID:          task.ID.String(),
		ShortID:     task.ShortID,
		Title:       task.Title,
		Description: task.Description,
		Status:      task.Status,
		Priority:    task.Priority,
		Order:       task.Order,
		Version:     task.Version,
		UDA:         task.UDA,
		CreatedAt:   task.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  task.ModifiedAt.Format(time.RFC3339),
		Children:    make([]treeNodeJSON, len(node.Children)),
	}
	if task.ParentID != nil {
		str := task.ParentID.String()
		tj.ParentID = &str
	}
	tj.ProjectID = renderer.projectName(task.ProjectID)
	if task.DueAt != nil {
		str := task.DueAt.Format(time.RFC3339)
		tj.DueAt = &str
	}
	if task.WaitUntil != nil {
		str := task.WaitUntil.Format(time.RFC3339)
		tj.WaitUntil = &str
	}
	tj.RecurrenceRule = task.RecurrenceRule
	if task.UrgencyOverrides != nil {
		tj.UrgencyOverrides = toUrgencyOverridesJSON(task.UrgencyOverrides)
	}
	if task.EffectiveWeights != nil {
		tj.EffectiveUrgencyWeights = toUrgencyWeightsJSON(*task.EffectiveWeights)
	}
	if node.Rollup != nil {
		rollupJSON := toRollupJSON(*node.Rollup)
		tj.Rollup = &rollupJSON
	}
	for index, child := range node.Children {
		tj.Children[index] = renderer.toTreeNodeJSON(child)
	}
	return tj
}

// renderTree writes the tree to w in the given format.
// For "text", each task is rendered as: {indent}{short_id} [{status}] {title}
// For "json", the tree is rendered as a nested JSON array with children.
// For "markdown", delegates to renderTreeMarkdown (see tree_markdown.go).
func (renderer *Renderer) renderTree(nodes []*treeNode) error {
	if renderer.format == "json" {
		jsonNodes := make([]treeNodeJSON, len(nodes))
		for index, node := range nodes {
			jsonNodes[index] = renderer.toTreeNodeJSON(node)
		}
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(jsonNodes)
	}

	if renderer.format == "markdown" {
		return renderer.renderTreeMarkdown(nodes)
	}

	// Text format
	for _, node := range nodes {
		if err := renderer.renderTreeNode(node, 0); err != nil {
			return err
		}
	}
	return nil
}

// renderTreeNode recursively renders a single tree node and its children.
// depth controls the indentation level (2 spaces per level).
func (renderer *Renderer) renderTreeNode(node *treeNode, depth int) error {
	indent := strings.Repeat("  ", depth)
	line := fmt.Sprintf("%s%s [%s] %s", indent, node.Task.ShortID, node.Task.Status, node.Task.Title)
	if renderer.hasTaxonomy(node.Task.ProjectID) {
		level := "—"
		if node.Task.Level != nil && *node.Task.Level != "" {
			level = *node.Task.Level
		}
		suffix := fmt.Sprintf(" [%s]", level)
		if renderer.styles != nil {
			suffix = renderer.styles.Dim.Render(suffix)
		}
		line += suffix
	}
	if renderer.isDimStatus(node.Task.Status) {
		line = renderer.styles.Dim.Render(line)
	}
	// Branch decoration for --rollup. Emitted only on visible branch nodes
	// (len(Children) > 0). With --rollup the fetch is decoupled from --all
	// in fetchTreeTasks, so node.Children mirrors the full DB structure
	// (delete-role kids included), making len(Children) > 0 the correct
	// "is a branch in the DB" check that matches the spec.
	if node.Rollup != nil && len(node.Children) > 0 {
		line += "  " + renderer.formatRollup(*node.Rollup)
	}
	if _, err := fmt.Fprintln(renderer.w, line); err != nil {
		return err
	}
	for _, child := range node.Children {
		if err := renderer.renderTreeNode(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// formatRollup renders the inline branch decoration for tree --rollup:
//
//	[done/total done, pct%] (status: count, ...)
//
// pct is rounded to the nearest integer; "–%" is emitted when Total == 0.
// When color is enabled, status segments tagged with the highlight role get
// bold styling and dim-role segments get faint styling.
func (renderer *Renderer) formatRollup(roll domain.Rollup) string {
	var pct string
	if roll.Total == 0 {
		pct = "–%"
	} else {
		pct = fmt.Sprintf("%d%%", int(math.Round(roll.Percent*100)))
	}
	progress := fmt.Sprintf("[%d/%d done, %s]", roll.Done, roll.Total, pct)

	parts := make([]string, 0, len(roll.StatusCounts))
	for _, statusCount := range roll.StatusCounts {
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
	breakdown := "(" + strings.Join(parts, ", ") + ")"

	return progress + " " + breakdown
}

// runTree handles the `tusk tree` and `tusk tree <short_id>` commands.
func (app *App) runTree(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	sortMode, _ := cmd.Flags().GetString("sort")
	if err := validateSortMode(sortMode); err != nil {
		return err
	}
	format := strings.ToLower(strings.TrimSpace(app.format))
	if format != "" && format != "text" && format != "json" && format != "markdown" {
		return fmt.Errorf("invalid format %q: tree supports text, json, or markdown", app.format)
	}
	rollup, _ := cmd.Flags().GetBool("rollup")
	if format == "markdown" && rollup {
		return fmt.Errorf("--rollup is not supported with --format markdown")
	}
	showAll, _ := cmd.Flags().GetBool("all")

	var tasks []*domain.Task
	var rootID *uuid.UUID

	switch {
	case len(args) > 0 && argsAreFilter(args):
		// Filter mode: positional args form an inline filter expression
		// (e.g. `project=tusk-roadmap`, `+tag`). Resolve to a domain filter
		// and apply on top of the same default status restriction
		// fetchTreeTasks would use.
		var err error
		tasks, err = app.fetchTreeTasksFiltered(ctx, cmd, args)
		if err != nil {
			return err
		}
	case len(args) > 0:
		// Subtree mode: fetch root explicitly, then pull its descendants
		// through the urgency-scored List path. Using List (instead of the
		// raw GetDescendants) populates Urgency on every descendant, so
		// `--sort urgency` produces the same behavior as in full-tree mode.
		root, rootErr := app.taskSvc.GetByShortID(ctx, args[0])

		if rootErr != nil {
			return fmt.Errorf("%s", formatError(rootErr, args[0]))
		}

		descendants, descendantsErr := app.fetchTreeTasks(ctx, cmd, &root.ID)

		if descendantsErr != nil {
			return descendantsErr
		}
		tasks = append([]*domain.Task{root}, descendants...)
		rootID = &root.ID
	default:
		// Full tree: fetch all non-deleted tasks
		var err error
		tasks, err = app.fetchTreeTasks(ctx, cmd, nil)
		if err != nil {
			return err
		}
	}

	// buildTree preserves the input slice order within each sibling group;
	// applying the re-sort on the flat slice is enough to control nested
	// rendering.
	sortTasks(tasks, sortMode)

	// Markdown export is single-project only. Reject multi-project trees up
	// front so we never call gatherMarkdownInputs with tasks spanning more
	// than one bundle. An empty tree is allowed and falls through to the
	// renderer, which emits nothing in that case.
	var mdInputs *markdownInputs
	if format == "markdown" && len(tasks) > 0 {
		seen := make(map[uuid.UUID]struct{})
		for _, task := range tasks {
			seen[task.ProjectID] = struct{}{}
			if len(seen) > 1 {
				return fmt.Errorf("--format markdown requires a single project; pass project=<name> or run on a single-project workspace")
			}
		}
		var markdownErr error
		mdInputs, markdownErr = app.gatherMarkdownInputs(ctx, tasks)

		if markdownErr != nil {
			return markdownErr
		}
	}

	nodes := buildTree(tasks, rootID)

	if len(nodes) == 0 && app.format != "json" && app.format != "markdown" {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "No tasks.")
		return err
	}

	if rollup {
		workflowFor, workflowLookupErr := app.buildWorkflowLookup(ctx, tasks)

		if workflowLookupErr != nil {
			return workflowLookupErr
		}
		computeRollups(nodes, workflowFor)
		// Per spec: --all controls rendering visibility; --rollup controls
		// computation. When --rollup is set without --all we fetched the
		// full subtree (so rollups are accurate) but must still hide
		// delete-role nodes from rendering to match the --all=off
		// experience. Rollups stay correct because they were computed
		// before the prune.
		if !showAll {
			nodes = pruneDeleteRoleNodes(nodes, workflowFor)
		}
	}

	// Markdown render needs delete-role nodes pruned the same way the
	// default text view excludes them. fetchTreeTasks already filters by
	// status when not in --rollup mode, but the workflow-driven delete
	// role check is still necessary for any custom workflows that mark
	// non-default statuses with delete role.
	if format == "markdown" && !showAll && mdInputs != nil {
		nodes = pruneDeleteRoleNodes(nodes, mdInputs.workflowFor)
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), app.buildDimStatuses())
	if rollup {
		renderer.highlightStatuses = app.buildHighlightStatuses()
	}
	renderer.setMarkdownInputs(mdInputs)
	return renderer.renderTree(nodes)
}

// buildWorkflowLookup resolves every distinct ProjectID in tasks to its
// governing workflow and returns a closure suitable for AggregateRollup's
// workflowFor parameter. A project that fails to resolve (deleted out from
// under us, or its workflow vanished) maps to nil — AggregateRollup skips
// those tasks rather than miscounting them.
func (app *App) buildWorkflowLookup(ctx context.Context, tasks []*domain.Task) (func(*domain.Task) *domain.Workflow, error) {
	if app.projectSvc == nil || app.workflowSvc == nil {
		return func(*domain.Task) *domain.Workflow { return nil }, nil
	}
	wfByProject := make(map[uuid.UUID]*domain.Workflow)
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if _, ok := wfByProject[task.ProjectID]; ok {
			continue
		}
		wfByProject[task.ProjectID] = nil
		proj, projectErr := app.projectSvc.GetByID(ctx, task.ProjectID)

		if projectErr != nil {
			continue
		}

		workflow, workflowErr := app.workflowSvc.GetByID(ctx, proj.WorkflowID)

		if workflowErr != nil {
			continue
		}

		wfByProject[task.ProjectID] = workflow
	}
	return func(task *domain.Task) *domain.Workflow {
		if task == nil {
			return nil
		}
		return wfByProject[task.ProjectID]
	}, nil
}

// computeRollups walks the tree depth-first and assigns a *Rollup to every
// node, including leaves (which receive a zero-value rollup). Used only in
// --rollup mode.
func computeRollups(nodes []*treeNode, workflowFor func(*domain.Task) *domain.Workflow) {
	for _, node := range nodes {
		descendants := flattenDescendants(node)
		rollup := domain.AggregateRollup(descendants, workflowFor)
		node.Rollup = &rollup
		computeRollups(node.Children, workflowFor)
	}
}

// flattenDescendants returns the strict descendants of n (n's own task is
// NOT included) in a flat slice via depth-first traversal.
func flattenDescendants(node *treeNode) []*domain.Task {
	var out []*domain.Task
	for _, child := range node.Children {
		out = append(out, child.Task)
		out = append(out, flattenDescendants(child)...)
	}
	return out
}

// pruneDeleteRoleNodes removes nodes whose status carries the delete role
// (and their entire subtrees) from the tree. workflowFor resolves a task
// to its governing workflow; if it returns nil for a task, that task is
// kept (we cannot classify it). Used in `--rollup` mode without `--all`
// to keep rendering visibility aligned with the default tree view while
// the rollup numbers (computed earlier) stay accurate.
func pruneDeleteRoleNodes(nodes []*treeNode, workflowFor func(*domain.Task) *domain.Workflow) []*treeNode {
	out := make([]*treeNode, 0, len(nodes))
	for _, node := range nodes {
		if isDeleteRoleTask(node.Task, workflowFor) {
			continue
		}
		node.Children = pruneDeleteRoleNodes(node.Children, workflowFor)
		out = append(out, node)
	}
	return out
}

func isDeleteRoleTask(task *domain.Task, workflowFor func(*domain.Task) *domain.Workflow) bool {
	if task == nil {
		return false
	}
	workflow := workflowFor(task)
	if workflow == nil {
		return false
	}
	statusConfig, ok := workflow.Statuses[task.Status]
	return ok && statusConfig.HasRole(domain.RoleDelete)
}

// fetchTreeTasks loads tasks for the tree view. rootID nil scopes the query
// to the whole workspace; non-nil restricts the result set to descendants of
// that task (the root itself is not included). By default excludes deleted
// tasks; --all includes every status. When --rollup is set we also fetch
// every status (regardless of --all) so the aggregator sees the full subtree
// and a delete-role-rooted branch does not silently hide its non-deleted
// descendants from the rollup totals. Routes through TaskService.List so
// Urgency is populated on every returned task.
func (app *App) fetchTreeTasks(ctx context.Context, cmd *cobra.Command, rootID *uuid.UUID) ([]*domain.Task, error) {
	showAll, _ := cmd.Flags().GetBool("all")
	rollup, _ := cmd.Flags().GetBool("rollup")

	taskFilter := domain.TaskFilter{RootID: rootID}
	if !showAll && !rollup {
		taskFilter.Statuses = []string{"pending", "active", "completed"}
	}
	return app.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: taskFilter})
}

// argsAreFilter reports whether the positional args look like an inline
// filter expression rather than a short_id. Any token containing `=`, or
// starting with `+`/`-` (the registered modifier prefixes), is filter
// syntax. A bare short_id never contains those characters.
func argsAreFilter(args []string) bool {
	for _, arg := range args {
		if strings.ContainsRune(arg, '=') {
			return true
		}
		if len(arg) > 0 && (arg[0] == '+' || arg[0] == '-') {
			return true
		}
	}
	return false
}

// fetchTreeTasksFiltered parses args as an inline filter expression and
// returns the matching tasks, AND-combined with the same default status
// restriction fetchTreeTasks applies (pending,active,completed unless --all
// or --rollup). Used for invocations like `tusk task tree project=<name>`.
func (app *App) fetchTreeTasksFiltered(ctx context.Context, cmd *cobra.Command, args []string) ([]*domain.Task, error) {
	showAll, _ := cmd.Flags().GetBool("all")
	rollup, _ := cmd.Flags().GetBool("rollup")

	input := strings.Join(args, " ")
	expr, parseErrs := filter.ParseExpr(input)
	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("filter errors:\n%s", filter.FormatErrors(parseErrs))
	}
	// Use ResolveExprAllStatuses so the resolver does not inject a `task list`-
	// style pending/active default; tree's default is broader (also includes
	// completed) so we apply it explicitly below.
	resolved, resolveErrs := app.resolver.ResolveExprAllStatuses(ctx, expr)
	if len(resolveErrs) > 0 {
		return nil, resolveErrs[0]
	}

	if !showAll && !rollup {
		statusTerm := &domain.TermFilter{TaskFilter: domain.TaskFilter{
			Statuses: []string{"pending", "active", "completed"},
		}}
		resolved = &domain.AndFilter{Children: []domain.FilterExpr{resolved, statusTerm}}
	}
	return app.taskSvc.List(ctx, resolved)
}
