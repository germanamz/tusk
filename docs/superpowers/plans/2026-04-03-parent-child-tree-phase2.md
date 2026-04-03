# Phase 2: `tusk tree` CLI Command — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `tusk tree` (full hierarchy) and `tusk tree <short_id>` (subtree) with compact indented text output and nested JSON output.

**Architecture:** Tree building and rendering live in `internal/tui/tree.go`. The command handler `runTree` is registered in `app.go`. Data fetching uses existing `List` (for full tree) and `GetByShortID` + `GetDescendants` (for subtrees). Tree is built in memory by grouping tasks by parent ID, then rendered recursively with 2-space indentation.

**Tech Stack:** Go, Cobra (existing), standard library only.

**Depends on:** Phase 1 must be completed first (cycle detection). The tree command itself doesn't depend on cycle detection, but the phases are sequential to keep the branch clean.

---

### Task 1: Tree building logic with unit tests

**Files:**
- Rewrite: `internal/tui/tree.go`
- Create: `internal/tui/tree_test.go`

- [ ] **Step 1: Write the failing test for `buildTree`**

Create `internal/tui/tree_test.go`:

```go
package tui

import (
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

func TestBuildTree_RootsOnly(t *testing.T) {
	a := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Task A"}
	b := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Task B"}

	nodes := buildTree([]*domain.Task{a, b}, nil)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 roots, got %d", len(nodes))
	}
}

func TestBuildTree_ParentAndChildren(t *testing.T) {
	parent := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Parent"}
	child1 := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child 1", ParentID: &parent.ID}
	child2 := &domain.Task{ID: uuid.New(), ShortID: "cccccccc", Title: "Child 2", ParentID: &parent.ID}

	nodes := buildTree([]*domain.Task{parent, child1, child2}, nil)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if len(nodes[0].Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(nodes[0].Children))
	}
}

func TestBuildTree_ThreeLevels(t *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child", ParentID: &root.ID}
	grandchild := &domain.Task{ID: uuid.New(), ShortID: "cccccccc", Title: "Grandchild", ParentID: &child.ID}

	nodes := buildTree([]*domain.Task{root, child, grandchild}, nil)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if len(nodes[0].Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(nodes[0].Children))
	}
	if len(nodes[0].Children[0].Children) != 1 {
		t.Fatalf("expected 1 grandchild, got %d", len(nodes[0].Children[0].Children))
	}
}

func TestBuildTree_SubtreeRoot(t *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child", ParentID: &root.ID}

	// When rootID is provided, only that task is the root — even if the child's
	// ParentID points to it
	nodes := buildTree([]*domain.Task{root, child}, &root.ID)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if nodes[0].Task.ShortID != "aaaaaaaa" {
		t.Fatalf("expected root to be aaaaaaaa, got %s", nodes[0].Task.ShortID)
	}
	if len(nodes[0].Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(nodes[0].Children))
	}
}

func TestBuildTree_OrphanedChildren(t *testing.T) {
	// Child whose parent is not in the task set — should appear as a root
	missingParentID := uuid.New()
	orphan := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Orphan", ParentID: &missingParentID}

	nodes := buildTree([]*domain.Task{orphan}, nil)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 root (orphan promoted), got %d", len(nodes))
	}
}

func TestBuildTree_Empty(t *testing.T) {
	nodes := buildTree([]*domain.Task{}, nil)
	if len(nodes) != 0 {
		t.Fatalf("expected 0 roots, got %d", len(nodes))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail (function doesn't exist yet)**

Run: `go test -v ./internal/tui -run TestBuildTree`
Expected: FAIL — `buildTree` is not defined.

- [ ] **Step 3: Implement `treeNode` and `buildTree` in `tree.go`**

Rewrite `internal/tui/tree.go` (currently just a package declaration):

```go
package tui

import (
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -v ./internal/tui -run TestBuildTree`
Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tree.go internal/tui/tree_test.go
git commit -m "feat(tui): add tree building logic with treeNode type"
```

---

### Task 2: Tree rendering (text and JSON) with unit tests

**Files:**
- Modify: `internal/tui/tree.go`
- Modify: `internal/tui/tree_test.go`

- [ ] **Step 1: Write the failing test for text rendering**

Add to `internal/tui/tree_test.go`:

```go
import (
	"bytes"
	"encoding/json"
	// ... existing imports
)

func TestRenderTree_Text(t *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root task", Status: "active"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child task", Status: "pending", ParentID: &root.ID}
	grandchild := &domain.Task{ID: uuid.New(), ShortID: "cccccccc", Title: "Grandchild", Status: "pending", ParentID: &child.ID}

	nodes := buildTree([]*domain.Task{root, child, grandchild}, nil)

	var buf bytes.Buffer
	if err := renderTree(&buf, nodes, "text"); err != nil {
		t.Fatalf("renderTree: %v", err)
	}

	output := buf.String()
	// Root at indent 0
	if !strings.Contains(output, "aaaaaaaa [active]  Root task") {
		t.Fatalf("expected root line, got:\n%s", output)
	}
	// Child at indent 2
	if !strings.Contains(output, "  bbbbbbbb [pending] Child task") {
		t.Fatalf("expected child line with 2-space indent, got:\n%s", output)
	}
	// Grandchild at indent 4
	if !strings.Contains(output, "    cccccccc [pending] Grandchild") {
		t.Fatalf("expected grandchild line with 4-space indent, got:\n%s", output)
	}
}

func TestRenderTree_TextEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTree(&buf, nil, "text"); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	if buf.String() != "" {
		t.Fatalf("expected empty output for nil nodes, got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -v ./internal/tui -run TestRenderTree_Text`
Expected: FAIL — `renderTree` is not defined.

- [ ] **Step 3: Write the failing test for JSON rendering**

Add to `internal/tui/tree_test.go`:

```go
func TestRenderTree_JSON(t *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root", Status: "active"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child", Status: "pending", ParentID: &root.ID}

	nodes := buildTree([]*domain.Task{root, child}, nil)

	var buf bytes.Buffer
	if err := renderTree(&buf, nodes, "json"); err != nil {
		t.Fatalf("renderTree: %v", err)
	}

	var parsed []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 root in JSON, got %d", len(parsed))
	}
	if parsed[0]["short_id"] != "aaaaaaaa" {
		t.Fatalf("expected root short_id aaaaaaaa, got %v", parsed[0]["short_id"])
	}
	children, ok := parsed[0]["children"].([]any)
	if !ok {
		t.Fatalf("expected children array, got %T", parsed[0]["children"])
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
}

func TestRenderTree_JSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTree(&buf, nil, "json"); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	var parsed []any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}
	if len(parsed) != 0 {
		t.Fatalf("expected empty JSON array, got %d elements", len(parsed))
	}
}
```

- [ ] **Step 4: Implement `renderTree` in `tree.go`**

Add to `internal/tui/tree.go`:

```go
import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

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
```

Make sure the import block at the top of `tree.go` includes all needed imports. The final import block should be:

```go
import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)
```

- [ ] **Step 5: Run all render tests to verify they pass**

Run: `go test -v ./internal/tui -run TestRenderTree`
Expected: all 4 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/tree.go internal/tui/tree_test.go
git commit -m "feat(tui): add tree rendering for text and JSON output"
```

---

### Task 3: Register `tree` command in CLI

**Files:**
- Modify: `internal/tui/tree.go` (add `runTree` method)
- Modify: `internal/tui/app.go` (register command)
- Modify: `internal/tui/commands_test.go` (add integration-style unit tests)

- [ ] **Step 1: Write the failing test for `runTree` with no tasks**

Add to `internal/tui/commands_test.go`:

```go
func TestRunTree_Empty(t *testing.T) {
	app, _ := testApp(t)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"tree"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree: %v", err)
	}
	if buf.String() != "" {
		t.Fatalf("expected empty output, got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -v ./internal/tui -run TestRunTree_Empty`
Expected: FAIL — `tree` command is not registered.

- [ ] **Step 3: Write the failing test for `runTree` with a hierarchy**

Add to `internal/tui/commands_test.go`:

```go
func TestRunTree_WithHierarchy(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent task"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	child := &domain.Task{Title: "Child task", ParentID: &parent.ID}
	if err := taskSvc.Create(ctx, child); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"tree"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree: %v", err)
	}

	output := buf.String()
	// Parent should appear without indent
	if !strings.Contains(output, parent.ShortID) {
		t.Fatalf("expected parent short_id in output, got:\n%s", output)
	}
	// Child should appear with indent
	if !strings.Contains(output, "  "+child.ShortID) {
		t.Fatalf("expected child with indent in output, got:\n%s", output)
	}
}
```

- [ ] **Step 4: Write the failing test for `runTree` with a subtree root**

Add to `internal/tui/commands_test.go`:

```go
func TestRunTree_Subtree(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	// Create two separate trees
	rootA := &domain.Task{Title: "Root A"}
	if err := taskSvc.Create(ctx, rootA); err != nil {
		t.Fatalf("Create rootA: %v", err)
	}
	childA := &domain.Task{Title: "Child of A", ParentID: &rootA.ID}
	if err := taskSvc.Create(ctx, childA); err != nil {
		t.Fatalf("Create childA: %v", err)
	}

	rootB := &domain.Task{Title: "Root B"}
	if err := taskSvc.Create(ctx, rootB); err != nil {
		t.Fatalf("Create rootB: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"tree", rootA.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree subtree: %v", err)
	}

	output := buf.String()
	// Root A and its child should appear
	if !strings.Contains(output, rootA.ShortID) {
		t.Fatalf("expected rootA in subtree output, got:\n%s", output)
	}
	if !strings.Contains(output, childA.ShortID) {
		t.Fatalf("expected childA in subtree output, got:\n%s", output)
	}
	// Root B should NOT appear
	if strings.Contains(output, rootB.ShortID) {
		t.Fatalf("rootB should not appear in subtree of rootA, got:\n%s", output)
	}
}
```

- [ ] **Step 5: Implement `runTree` in `tree.go`**

Add the `runTree` method to `internal/tui/tree.go`:

```go
import (
	// add "context" to the imports
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

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
```

- [ ] **Step 6: Register the `tree` command in `app.go`**

In `internal/tui/app.go`, add the tree command to the `a.root.AddCommand(...)` call. Add it after the `annotate` command (after line 109) and before the `link` command. Insert this block:

```go
		a.treeCmd(),
```

Then add this method to `internal/tui/tree.go` (or `app.go` — either works, but `tree.go` keeps things together):

Actually, add it to `tree.go` to keep tree-related code together:

```go
// treeCmd builds the Cobra command for `tusk tree`.
func (a *App) treeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tree [short_id]",
		Short: "Display tasks as a tree hierarchy",
		Long:  "Show all tasks in a tree hierarchy. Optionally specify a short_id to show only that subtree.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  a.runTree,
	}
	cmd.Flags().Bool("all", false, "include deleted tasks")
	return cmd
}
```

- [ ] **Step 7: Run the tree tests to verify they pass**

Run: `go test -v ./internal/tui -run TestRunTree`
Expected: all 3 tests PASS.

- [ ] **Step 8: Run the full TUI test suite**

Run: `go test -v ./internal/tui/...`
Expected: all tests PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/tree.go internal/tui/tree_test.go internal/tui/app.go internal/tui/commands_test.go
git commit -m "feat(tui): add tree command with full and subtree views"
```

---

### Task 4: JSON tree output test and `--all` flag test

**Files:**
- Modify: `internal/tui/commands_test.go`

- [ ] **Step 1: Write the test for JSON tree output**

Add to `internal/tui/commands_test.go`:

```go
func TestRunTree_JSON(t *testing.T) {
	app, taskSvc := testApp(t)
	app.format = "json"
	ctx := context.Background()

	parent := &domain.Task{Title: "JSON Parent"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	child := &domain.Task{Title: "JSON Child", ParentID: &parent.ID}
	if err := taskSvc.Create(ctx, child); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"tree"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree: %v", err)
	}

	var parsed []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON unmarshal: %v\nraw: %s", err, buf.String())
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 root, got %d", len(parsed))
	}
	children, ok := parsed[0]["children"].([]any)
	if !ok {
		t.Fatalf("expected children array")
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
}
```

- [ ] **Step 2: Write the test for `--all` flag (includes deleted tasks)**

Add to `internal/tui/commands_test.go`:

```go
func TestRunTree_AllFlag(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	// Create a task, start it, then delete a different task
	alive := &domain.Task{Title: "Alive task"}
	if err := taskSvc.Create(ctx, alive); err != nil {
		t.Fatalf("Create alive: %v", err)
	}

	doomed := &domain.Task{Title: "Doomed task"}
	if err := taskSvc.Create(ctx, doomed); err != nil {
		t.Fatalf("Create doomed: %v", err)
	}
	if _, err := taskSvc.Delete(ctx, doomed.ShortID, doomed.Version); err != nil {
		t.Fatalf("Delete doomed: %v", err)
	}

	// Without --all, deleted task should not appear
	var buf1 bytes.Buffer
	app.root.SetOut(&buf1)
	app.root.SetArgs([]string{"tree"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree: %v", err)
	}
	if strings.Contains(buf1.String(), doomed.ShortID) {
		t.Fatalf("deleted task should not appear without --all:\n%s", buf1.String())
	}

	// With --all, deleted task should appear
	var buf2 bytes.Buffer
	app.root.SetOut(&buf2)
	app.root.SetArgs([]string{"tree", "--all"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree --all: %v", err)
	}
	if !strings.Contains(buf2.String(), doomed.ShortID) {
		t.Fatalf("deleted task should appear with --all:\n%s", buf2.String())
	}
}
```

- [ ] **Step 3: Run the new tests**

Run: `go test -v ./internal/tui -run "TestRunTree_JSON|TestRunTree_AllFlag"`
Expected: PASS.

- [ ] **Step 4: Run the full project test suite**

Run: `make test`
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commands_test.go
git commit -m "test(tui): add JSON and --all flag tests for tree command"
```
