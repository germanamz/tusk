package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
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
	for _, t := range tasks {
		byID[t.ID] = &treeNode{Task: t}
	}

	// Group children under parents
	var roots []*treeNode
	for _, t := range tasks {
		node := byID[t.ID]
		if rootID != nil && t.ID == *rootID {
			roots = append(roots, node)
			continue
		}
		if t.ParentID != nil {
			if parent, ok := byID[*t.ParentID]; ok {
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
func (r *Renderer) toTreeNodeJSON(node *treeNode) treeNodeJSON {
	t := node.Task
	tj := treeNodeJSON{
		ID:          t.ID.String(),
		ShortID:     t.ShortID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		Order:       t.Order,
		Version:     t.Version,
		UDA:         t.UDA,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  t.ModifiedAt.Format(time.RFC3339),
		Children:    make([]treeNodeJSON, len(node.Children)),
	}
	if t.ParentID != nil {
		s := t.ParentID.String()
		tj.ParentID = &s
	}
	tj.ProjectID = r.projectName(t.ProjectID)
	if t.DueAt != nil {
		s := t.DueAt.Format(time.RFC3339)
		tj.DueAt = &s
	}
	if t.WaitUntil != nil {
		s := t.WaitUntil.Format(time.RFC3339)
		tj.WaitUntil = &s
	}
	tj.RecurrenceRule = t.RecurrenceRule
	if t.UrgencyOverrides != nil {
		tj.UrgencyOverrides = toUrgencyOverridesJSON(t.UrgencyOverrides)
	}
	if t.EffectiveWeights != nil {
		tj.EffectiveUrgencyWeights = toUrgencyWeightsJSON(*t.EffectiveWeights)
	}
	if node.Rollup != nil {
		rj := toRollupJSON(*node.Rollup)
		tj.Rollup = &rj
	}
	for i, child := range node.Children {
		tj.Children[i] = r.toTreeNodeJSON(child)
	}
	return tj
}

// renderTree writes the tree to w in the given format.
// For "text", each task is rendered as: {indent}{short_id} [{status}] {title}
// For "json", the tree is rendered as a nested JSON array with children.
// For "markdown", delegates to renderTreeMarkdown (see tree_markdown.go).
func (r *Renderer) renderTree(nodes []*treeNode) error {
	if r.format == "json" {
		jsonNodes := make([]treeNodeJSON, len(nodes))
		for i, n := range nodes {
			jsonNodes[i] = r.toTreeNodeJSON(n)
		}
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(jsonNodes)
	}

	if r.format == "markdown" {
		return r.renderTreeMarkdown(nodes)
	}

	// Text format
	for _, node := range nodes {
		if err := r.renderTreeNode(node, 0); err != nil {
			return err
		}
	}
	return nil
}

// renderTreeNode recursively renders a single tree node and its children.
// depth controls the indentation level (2 spaces per level).
func (r *Renderer) renderTreeNode(node *treeNode, depth int) error {
	indent := strings.Repeat("  ", depth)
	line := fmt.Sprintf("%s%s [%s] %s", indent, node.Task.ShortID, node.Task.Status, node.Task.Title)
	if r.hasTaxonomy(node.Task.ProjectID) {
		level := "—"
		if node.Task.Level != nil && *node.Task.Level != "" {
			level = *node.Task.Level
		}
		suffix := fmt.Sprintf(" [%s]", level)
		if r.styles != nil {
			suffix = r.styles.Dim.Render(suffix)
		}
		line += suffix
	}
	if r.isDimStatus(node.Task.Status) {
		line = r.styles.Dim.Render(line)
	}
	// Branch decoration for --rollup. Emitted only on visible branch nodes
	// (len(Children) > 0). With --rollup the fetch is decoupled from --all
	// in fetchTreeTasks, so node.Children mirrors the full DB structure
	// (delete-role kids included), making len(Children) > 0 the correct
	// "is a branch in the DB" check that matches the spec.
	if node.Rollup != nil && len(node.Children) > 0 {
		line += "  " + r.formatRollup(*node.Rollup)
	}
	if _, err := fmt.Fprintln(r.w, line); err != nil {
		return err
	}
	for _, child := range node.Children {
		if err := r.renderTreeNode(child, depth+1); err != nil {
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
func (r *Renderer) formatRollup(roll domain.Rollup) string {
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
	breakdown := "(" + strings.Join(parts, ", ") + ")"

	return progress + " " + breakdown
}

// runTree handles the `tusk tree` and `tusk tree <short_id>` commands.
func (a *App) runTree(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	sortMode, _ := cmd.Flags().GetString("sort")
	if err := validateSortMode(sortMode); err != nil {
		return err
	}
	format := strings.ToLower(strings.TrimSpace(a.format))
	if format != "" && format != "text" && format != "json" && format != "markdown" {
		return fmt.Errorf("invalid format %q: tree supports text, json, or markdown", a.format)
	}
	rollup, _ := cmd.Flags().GetBool("rollup")
	if format == "markdown" && rollup {
		return fmt.Errorf("--rollup is not supported with --format markdown")
	}
	showAll, _ := cmd.Flags().GetBool("all")

	var tasks []*domain.Task
	var rootID *uuid.UUID

	if len(args) > 0 {
		// Subtree mode: fetch root explicitly, then pull its descendants
		// through the urgency-scored List path. Using List (instead of the
		// raw GetDescendants) populates Urgency on every descendant, so
		// `--sort urgency` produces the same behavior as in full-tree mode.
		root, err := a.taskSvc.GetByShortID(ctx, args[0])
		if err != nil {
			return fmt.Errorf("%s", formatError(err, args[0]))
		}
		descendants, err := a.fetchTreeTasks(ctx, cmd, &root.ID)
		if err != nil {
			return err
		}
		tasks = append([]*domain.Task{root}, descendants...)
		rootID = &root.ID
	} else {
		// Full tree: fetch all non-deleted tasks
		var err error
		tasks, err = a.fetchTreeTasks(ctx, cmd, nil)
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
		for _, t := range tasks {
			seen[t.ProjectID] = struct{}{}
			if len(seen) > 1 {
				return fmt.Errorf("--format markdown requires a single project; pass project=<name> or run on a single-project workspace")
			}
		}
		var err error
		mdInputs, err = a.gatherMarkdownInputs(ctx, tasks)
		if err != nil {
			return err
		}
	}

	nodes := buildTree(tasks, rootID)

	if len(nodes) == 0 && a.format != "json" && a.format != "markdown" {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "No tasks.")
		return err
	}

	if rollup {
		workflowFor, err := a.buildWorkflowLookup(ctx, tasks)
		if err != nil {
			return err
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

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), a.buildDimStatuses())
	if rollup {
		r.highlightStatuses = a.buildHighlightStatuses()
	}
	r.setMarkdownInputs(mdInputs)
	return r.renderTree(nodes)
}

// buildWorkflowLookup resolves every distinct ProjectID in tasks to its
// governing workflow and returns a closure suitable for AggregateRollup's
// workflowFor parameter. A project that fails to resolve (deleted out from
// under us, or its workflow vanished) maps to nil — AggregateRollup skips
// those tasks rather than miscounting them.
func (a *App) buildWorkflowLookup(ctx context.Context, tasks []*domain.Task) (func(*domain.Task) *domain.Workflow, error) {
	if a.projectSvc == nil || a.workflowSvc == nil {
		return func(*domain.Task) *domain.Workflow { return nil }, nil
	}
	wfByProject := make(map[uuid.UUID]*domain.Workflow)
	for _, t := range tasks {
		if t == nil {
			continue
		}
		if _, ok := wfByProject[t.ProjectID]; ok {
			continue
		}
		wfByProject[t.ProjectID] = nil
		proj, err := a.projectSvc.GetByID(ctx, t.ProjectID)
		if err != nil {
			continue
		}
		wf, err := a.workflowSvc.GetByID(ctx, proj.WorkflowID)
		if err != nil {
			continue
		}
		wfByProject[t.ProjectID] = wf
	}
	return func(t *domain.Task) *domain.Workflow {
		if t == nil {
			return nil
		}
		return wfByProject[t.ProjectID]
	}, nil
}

// computeRollups walks the tree depth-first and assigns a *Rollup to every
// node, including leaves (which receive a zero-value rollup). Used only in
// --rollup mode.
func computeRollups(nodes []*treeNode, workflowFor func(*domain.Task) *domain.Workflow) {
	for _, n := range nodes {
		descendants := flattenDescendants(n)
		roll := domain.AggregateRollup(descendants, workflowFor)
		n.Rollup = &roll
		computeRollups(n.Children, workflowFor)
	}
}

// flattenDescendants returns the strict descendants of n (n's own task is
// NOT included) in a flat slice via depth-first traversal.
func flattenDescendants(n *treeNode) []*domain.Task {
	var out []*domain.Task
	for _, c := range n.Children {
		out = append(out, c.Task)
		out = append(out, flattenDescendants(c)...)
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
	for _, n := range nodes {
		if isDeleteRoleTask(n.Task, workflowFor) {
			continue
		}
		n.Children = pruneDeleteRoleNodes(n.Children, workflowFor)
		out = append(out, n)
	}
	return out
}

func isDeleteRoleTask(t *domain.Task, workflowFor func(*domain.Task) *domain.Workflow) bool {
	if t == nil {
		return false
	}
	wf := workflowFor(t)
	if wf == nil {
		return false
	}
	cfg, ok := wf.Statuses[t.Status]
	return ok && cfg.HasRole(domain.RoleDelete)
}

// fetchTreeTasks loads tasks for the tree view. rootID nil scopes the query
// to the whole workspace; non-nil restricts the result set to descendants of
// that task (the root itself is not included). By default excludes deleted
// tasks; --all includes every status. When --rollup is set we also fetch
// every status (regardless of --all) so the aggregator sees the full subtree
// and a delete-role-rooted branch does not silently hide its non-deleted
// descendants from the rollup totals. Routes through TaskService.List so
// Urgency is populated on every returned task.
func (a *App) fetchTreeTasks(ctx context.Context, cmd *cobra.Command, rootID *uuid.UUID) ([]*domain.Task, error) {
	showAll, _ := cmd.Flags().GetBool("all")
	rollup, _ := cmd.Flags().GetBool("rollup")

	filter := domain.TaskFilter{RootID: rootID}
	if !showAll && !rollup {
		filter.Statuses = []string{"pending", "active", "completed"}
	}
	return a.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: filter})
}
