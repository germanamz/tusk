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
					Args: []string{"add", "Child task", "parent:$0.short_id"},
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
					Args: []string{"modify", "$1.short_id", "parent:$0.short_id"},
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
					Args: []string{"add", "The child", "parent:$0.short_id"},
				},
				// Step 2: Clear child's parent
				{
					Args: []string{"modify", "$1.short_id", "parent:"},
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
