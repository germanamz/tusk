package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
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
type treeNodeJSON struct {
	ShortID  string         `json:"short_id"`
	Title    string         `json:"title"`
	Status   string         `json:"status"`
	Priority int            `json:"priority"`
	ParentID *string        `json:"parent_id,omitempty"`
	Children []treeNodeJSON `json:"children"`
}

// toTreeNodeJSON converts a treeNode to its JSON representation recursively.
func toTreeNodeJSON(node *treeNode) treeNodeJSON {
	tj := treeNodeJSON{
		ShortID:  node.Task.ShortID,
		Title:    node.Task.Title,
		Status:   node.Task.Status,
		Priority: node.Task.Priority,
		Children: make([]treeNodeJSON, len(node.Children)),
	}
	if node.Task.ParentID != nil {
		s := node.Task.ParentID.String()
		tj.ParentID = &s
	}
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
