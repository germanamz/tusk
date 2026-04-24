package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// treeNode represents a task in the tree with its children.
type treeNode struct {
	Task     *domain.Task
	Children []*treeNode
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
	ID             string         `json:"id"`
	ShortID        string         `json:"short_id"`
	ParentID       *string        `json:"parent_id"`
	ProjectID      string         `json:"project_id"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	Priority       int            `json:"priority"`
	Order          *float64       `json:"order,omitempty"`
	Version        int            `json:"version"`
	DueAt          *string        `json:"due_at,omitempty"`
	WaitUntil      *string        `json:"wait_until,omitempty"`
	RecurrenceRule *string        `json:"recurrence_rule,omitempty"`
	UDA            map[string]any `json:"uda,omitempty"`
	CreatedAt      string         `json:"created_at"`
	ModifiedAt     string         `json:"modified_at"`
	Children       []treeNodeJSON `json:"children"`
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
	for i, child := range node.Children {
		tj.Children[i] = r.toTreeNodeJSON(child)
	}
	return tj
}

// renderTree writes the tree to w in the given format.
// For "text", each task is rendered as: {indent}{short_id} [{status}] {title}
// For "json", the tree is rendered as a nested JSON array with children.
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

// runTree handles the `tusk tree` and `tusk tree <short_id>` commands.
func (a *App) runTree(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	sortMode, _ := cmd.Flags().GetString("sort")
	if err := validateSortMode(sortMode); err != nil {
		return err
	}

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

	nodes := buildTree(tasks, rootID)

	if len(nodes) == 0 && a.format != "json" {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "No tasks.")
		return err
	}

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), a.buildDimStatuses())
	return r.renderTree(nodes)
}

// fetchTreeTasks loads tasks for the tree view. rootID nil scopes the query
// to the whole workspace; non-nil restricts the result set to descendants of
// that task (the root itself is not included). By default excludes deleted
// tasks; --all includes every status. Routes through TaskService.List so
// Urgency is populated on every returned task.
func (a *App) fetchTreeTasks(ctx context.Context, cmd *cobra.Command, rootID *uuid.UUID) ([]*domain.Task, error) {
	showAll, _ := cmd.Flags().GetBool("all")

	filter := domain.TaskFilter{RootID: rootID}
	if !showAll {
		filter.Statuses = []string{"pending", "active", "completed"}
	}
	return a.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: filter})
}
