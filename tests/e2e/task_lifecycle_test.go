package e2e

import "testing"

func TestTaskLifecycle(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_single_task",
			Steps: []Step{
				{
					Args: []string{"add", "Buy milk", "priority:3"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "Buy milk")
						assertEqual(t, m["status"], "pending")
						assertEqual(t, m["priority"], float64(3))
						if m["short_id"] == nil || m["short_id"] == "" {
							t.Fatal("expected short_id to be set")
						}
						if m["version"] != float64(1) {
							t.Fatalf("expected version 1, got %v", m["version"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created task")
					},
				},
			},
		},
		{
			Name: "create_start_done",
			Steps: []Step{
				{
					Args: []string{"add", "Full lifecycle task"},
				},
				{
					Args: []string{"start", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "active")
						assertEqual(t, m["version"], float64(2))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Started task")
					},
				},
				{
					Args: []string{"done", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "completed")
						assertEqual(t, m["version"], float64(3))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Completed task")
					},
				},
			},
		},
		{
			Name: "create_delete",
			Steps: []Step{
				{
					Args: []string{"add", "Delete me"},
				},
				{
					Args: []string{"delete", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "deleted")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Deleted task")
					},
				},
			},
		},
		{
			Name: "create_start_back_to_pending",
			Steps: []Step{
				{
					Args: []string{"add", "Back and forth"},
				},
				{
					Args: []string{"start", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						assertEqual(t, parsed.(map[string]any)["status"], "active")
					},
				},
				{
					Args: []string{"modify", "$0.short_id", "status:pending"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "pending")
						assertEqual(t, m["version"], float64(3))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
			},
		},
		{
			Name: "completed_reopen",
			Steps: []Step{
				{
					Args: []string{"add", "Reopen me"},
				},
				{
					Args: []string{"start", "$0.short_id"},
				},
				{
					Args: []string{"done", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						assertEqual(t, parsed.(map[string]any)["status"], "completed")
					},
				},
				{
					Args: []string{"modify", "$0.short_id", "status:pending"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "pending")
						assertEqual(t, m["version"], float64(4))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
			},
		},
		{
			Name: "create_with_project",
			Steps: []Step{
				{
					// default project is seeded by config
					Args: []string{"add", "Project task", "project:default"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "Project task")
						assertEqual(t, m["project_id"], "default")
					},
				},
			},
		},
		{
			Name: "create_multiple_list_shows_all",
			Steps: []Step{
				{
					Args: []string{"add", "Task one"},
				},
				{
					Args: []string{"add", "Task two"},
				},
				{
					Args: []string{"add", "Task three"},
				},
				{
					Args: []string{"list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 3 {
							t.Fatalf("expected 3 tasks, got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Task one")
						assertContains(t, output, "Task two")
						assertContains(t, output, "Task three")
					},
				},
			},
		},
		{
			Name: "info_shows_task_details",
			Steps: []Step{
				{
					Args: []string{"add", "Detail task", "priority:2"},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "Detail task")
						assertEqual(t, m["status"], "pending")
						assertEqual(t, m["priority"], float64(2))
						assertEqual(t, m["version"], float64(1))
						if m["created_at"] == nil {
							t.Fatal("expected created_at")
						}
						if m["modified_at"] == nil {
							t.Fatal("expected modified_at")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Detail task")
						assertContains(t, output, "pending")
						assertContains(t, output, "medium")
					},
				},
			},
		},
		{
			Name: "modify_title_and_priority",
			Steps: []Step{
				{
					Args: []string{"add", "Original title", "priority:1"},
				},
				{
					Args: []string{"modify", "$0.short_id", "New title", "priority:4"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "New title")
						assertEqual(t, m["priority"], float64(4))
						assertEqual(t, m["version"], float64(2))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
				{
					// Verify via info that the changes persisted
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "New title")
						assertEqual(t, m["priority"], float64(4))
					},
				},
			},
		},
		{
			Name: "create_task_has_empty_description",
			Steps: []Step{
				{
					Args: []string{"add", "Task without description"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["description"], "")
					},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["description"], "")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						// Empty description should not appear in text output
						assertNotContains(t, output, "Description:")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
