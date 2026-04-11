package e2e

import (
	"testing"
)

func TestHierarchy(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_with_parent",
			Steps: []Step{
				// Step 0: Create parent task
				{
					Args: []string{"add", "Parent task"},
				},
				// Step 1: Create child with parent reference
				{
					Args: []string{"add", "Child task", "parent=$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created task")
					},
				},
				// Step 2: Verify child's parent via info
				{
					Args: []string{"info", "$1.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["parent_id"] == nil || m["parent_id"] == "" {
							t.Fatal("expected parent_id to be set")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Parent:")
					},
				},
			},
		},
		{
			Name: "modify_set_parent",
			Steps: []Step{
				// Step 0: Create task A
				{
					Args: []string{"add", "Task A"},
				},
				// Step 1: Create task B (no parent)
				{
					Args: []string{"add", "Task B"},
				},
				// Step 2: Set B's parent to A
				{
					Args: []string{"modify", "$1.short_id", "parent=$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
				// Step 3: Verify B's parent is A
				{
					Args: []string{"info", "$1.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["parent_id"] == nil || m["parent_id"] == "" {
							t.Fatal("expected parent_id to be set after modify")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Parent:")
					},
				},
			},
		},
		{
			Name: "modify_clear_parent",
			Steps: []Step{
				// Step 0: Create parent
				{
					Args: []string{"add", "The parent"},
				},
				// Step 1: Create child with parent
				{
					Args: []string{"add", "The child", "parent=$0.short_id"},
				},
				// Step 2: Clear child's parent
				{
					Args: []string{"modify", "$1.short_id", "parent="},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
				// Step 3: Verify parent is cleared
				{
					Args: []string{"info", "$1.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["parent_id"] != nil {
							t.Fatalf("expected parent_id to be nil after clearing, got %v", m["parent_id"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertNotContains(t, output, "Parent:")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestTree(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "tree_full_view",
			Steps: []Step{
				// Step 0: Create parent
				{
					Args: []string{"add", "Root task"},
				},
				// Step 1: Create child 1
				{
					Args: []string{"add", "Child one", "parent=$0.short_id"},
				},
				// Step 2: Create child 2
				{
					Args: []string{"add", "Child two", "parent=$0.short_id"},
				},
				// Step 3: Run tree — should show all three
				{
					Args: []string{"tree"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 root in tree, got %d", len(arr))
						}
						root := arr[0].(map[string]any)
						children := root["children"].([]any)
						if len(children) != 2 {
							t.Fatalf("expected 2 children, got %d", len(children))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Root task")
						assertContains(t, output, "Child one")
						assertContains(t, output, "Child two")
					},
				},
			},
		},
		{
			Name: "tree_subtree_view",
			Steps: []Step{
				// Step 0: Create root A
				{
					Args: []string{"add", "Root A"},
				},
				// Step 1: Create child of A
				{
					Args: []string{"add", "Child of A", "parent=$0.short_id"},
				},
				// Step 2: Create grandchild of A
				{
					Args: []string{"add", "Grandchild of A", "parent=$1.short_id"},
				},
				// Step 3: Create root B (separate tree)
				{
					Args: []string{"add", "Root B"},
				},
				// Step 4: Run tree with root A — should show A's subtree only
				{
					Args: []string{"tree", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 root, got %d", len(arr))
						}
						root := arr[0].(map[string]any)
						assertEqual(t, root["title"], "Root A")
						children := root["children"].([]any)
						if len(children) != 1 {
							t.Fatalf("expected 1 child, got %d", len(children))
						}
						child := children[0].(map[string]any)
						grandchildren := child["children"].([]any)
						if len(grandchildren) != 1 {
							t.Fatalf("expected 1 grandchild, got %d", len(grandchildren))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Root A")
						assertContains(t, output, "Child of A")
						assertContains(t, output, "Grandchild of A")
						assertNotContains(t, output, "Root B")
					},
				},
			},
		},
		{
			Name: "tree_empty",
			Steps: []Step{
				// No tasks created — text prints "No tasks." to stderr, json prints empty array
				{
					Args: []string{"tree"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 0 {
							t.Fatalf("expected empty tree, got %d roots", len(arr))
						}
					},
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						// In text mode, "No tasks." goes to stderr; stdout stays empty
						if r.Stdout == "" && r.Stderr != "" {
							assertStderrContains(t, r, "No tasks.")
						}
					},
				},
			},
		},
		{
			Name: "tree_subtree_not_found",
			Steps: []Step{
				{
					Args:    []string{"tree", "nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestHierarchyErrors(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "circular_parent_direct",
			Steps: []Step{
				// Step 0: Create A
				{
					Args: []string{"add", "Task A"},
				},
				// Step 1: Create B with parent A
				{
					Args: []string{"add", "Task B", "parent=$0.short_id"},
				},
				// Step 2: Try to set A's parent to B — should fail (A->B->A cycle)
				{
					Args:    []string{"modify", "$0.short_id", "parent=$1.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "cycle")
					},
				},
			},
		},
		{
			Name: "circular_parent_transitive",
			Steps: []Step{
				// Step 0: Create A
				{
					Args: []string{"add", "Task A"},
				},
				// Step 1: Create B with parent A
				{
					Args: []string{"add", "Task B", "parent=$0.short_id"},
				},
				// Step 2: Create C with parent B
				{
					Args: []string{"add", "Task C", "parent=$1.short_id"},
				},
				// Step 3: Try to set A's parent to C — should fail (A->B->C->A cycle)
				{
					Args:    []string{"modify", "$0.short_id", "parent=$2.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "cycle")
					},
				},
			},
		},
		{
			Name: "parent_invalid_short_id",
			Steps: []Step{
				{
					Args:    []string{"add", "Orphan task", "parent=nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "non-hex character")
					},
				},
			},
		},
		{
			Name: "parent_not_found",
			Steps: []Step{
				{
					Args:    []string{"add", "Orphan task", "parent=deadbeef"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
