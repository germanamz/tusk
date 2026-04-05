package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
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
func toTreeNodeJSON(node *treeNode) treeNodeJSON {
	t := node.Task
	tj := treeNodeJSON{
		ID:          t.ID.String(),
		ShortID:     t.ShortID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
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
	tj.ProjectID = t.ProjectID
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
		tj.Children[i] = toTreeNodeJSON(child)
	}
	return tj
}

// renderTree writes the tree to w in the given format.
// For "text", each task is rendered as: {indent}{short_id} [{status}] {title}
// For "json", the tree is rendered as a nested JSON array with children.
func renderTree(w io.Writer, nodes []*treeNode, format string) error {
	if format == "json" {
		jsonNodes := make([]treeNodeJSON, len(nodes))
		for i, n := range nodes {
			jsonNodes[i] = toTreeNodeJSON(n)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(jsonNodes)
	}

	// Text format
	for _, node := range nodes {
		if err := renderTreeNode(w, node, 0); err != nil {
			return err
		}
	}
	return nil
}

// renderTreeNode recursively renders a single tree node and its children.
// depth controls the indentation level (2 spaces per level).
func renderTreeNode(w io.Writer, node *treeNode, depth int) error {
	indent := strings.Repeat("  ", depth)
	if _, err := fmt.Fprintf(w, "%s%s [%s] %s\n", indent, node.Task.ShortID, node.Task.Status, node.Task.Title); err != nil {
		return err
	}
	for _, child := range node.Children {
		if err := renderTreeNode(w, child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// runTree handles the `tusk tree` and `tusk tree <short_id>` commands.
func (a *App) runTree(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	var tasks []*domain.Task
	var rootID *uuid.UUID

	if len(args) > 0 {
		// Subtree mode: fetch root + descendants
		root, err := a.taskSvc.GetByShortID(ctx, args[0])
		if err != nil {
			return fmt.Errorf("%s", formatError(err, args[0]))
		}
		descendants, err := a.taskSvc.GetDescendants(ctx, root.ID)
		if err != nil {
			return fmt.Errorf("loading descendants: %w", err)
		}
		tasks = append([]*domain.Task{root}, descendants...)
		rootID = &root.ID
	} else {
		// Full tree: fetch all non-deleted tasks
		var err error
		tasks, err = a.fetchTreeTasks(ctx, cmd)
		if err != nil {
			return err
		}
	}

	nodes := buildTree(tasks, rootID)

	if len(nodes) == 0 && a.format != "json" {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "No tasks.")
		return err
	}

	return renderTree(cmd.OutOrStdout(), nodes, a.format)
}

// fetchTreeTasks loads all tasks for the full tree view.
// By default, excludes deleted tasks. If --all is set, includes all statuses.
func (a *App) fetchTreeTasks(ctx context.Context, cmd *cobra.Command) ([]*domain.Task, error) {
	showAll, _ := cmd.Flags().GetBool("all")

	filter := domain.TaskFilter{}
	if !showAll {
		filter.Statuses = []string{"pending", "active", "completed"}
	}
	return a.taskSvc.List(ctx, filter)
}
