package e2e

import "testing"

func TestTaskLifecycle(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_single_task",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Buy milk", "priority=3"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						taskMap := parsed.(map[string]any)
						assertEqual(test, taskMap["title"], "Buy milk")
						assertEqual(test, taskMap["status"], "pending")
						assertEqual(test, taskMap["priority"], float64(3))
						if taskMap["short_id"] == nil || taskMap["short_id"] == "" {
							test.Fatal("expected short_id to be set")
						}
						if taskMap["version"] != float64(1) {
							test.Fatalf("expected version 1, got %v", taskMap["version"])
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Created task")
					},
				},
			},
		},
		{
			Name: "create_start_done",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Full lifecycle task"},
				},
				{
					Args: []string{"task", "start", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["status"], "active")
						assertEqual(test, m["version"], float64(2))
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Started task")
					},
				},
				{
					Args: []string{"task", "done", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["status"], "completed")
						assertEqual(test, m["version"], float64(3))
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Completed task")
					},
				},
			},
		},
		{
			Name: "create_delete",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Delete me"},
				},
				{
					Args: []string{"task", "delete", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["status"], "deleted")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Deleted task")
					},
				},
			},
		},
		{
			Name: "create_start_back_to_pending",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Back and forth"},
				},
				{
					Args: []string{"task", "start", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						assertEqual(test, parsed.(map[string]any)["status"], "active")
					},
				},
				{
					Args: []string{"task", "modify", "$0.short_id", "status=pending"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["status"], "pending")
						assertEqual(test, m["version"], float64(3))
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Modified task")
					},
				},
			},
		},
		{
			Name: "completed_reopen",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Reopen me"},
				},
				{
					Args: []string{"task", "start", "$0.short_id"},
				},
				{
					Args: []string{"task", "done", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						assertEqual(test, parsed.(map[string]any)["status"], "completed")
					},
				},
				{
					Args: []string{"task", "modify", "$0.short_id", "status=pending"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["status"], "pending")
						assertEqual(test, m["version"], float64(4))
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Modified task")
					},
				},
			},
		},
		{
			Name: "create_with_project",
			Steps: []Step{
				{
					// default project is seeded by config
					Args: []string{"task", "create", "Project task", "project=default"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["title"], "Project task")
						assertEqual(test, m["project_id"], "default")
					},
				},
			},
		},
		{
			Name: "create_multiple_list_shows_all",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Task one"},
				},
				{
					Args: []string{"task", "create", "Task two"},
				},
				{
					Args: []string{"task", "create", "Task three"},
				},
				{
					Args: []string{"task", "list"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 3 {
							test.Fatalf("expected 3 tasks, got %d", len(arr))
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Task one")
						assertContains(test, output, "Task two")
						assertContains(test, output, "Task three")
					},
				},
			},
		},
		{
			Name: "info_shows_task_details",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Detail task", "priority=2"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						taskMap := parsed.(map[string]any)
						assertEqual(test, taskMap["title"], "Detail task")
						assertEqual(test, taskMap["status"], "pending")
						assertEqual(test, taskMap["priority"], float64(2))
						assertEqual(test, taskMap["version"], float64(1))
						if taskMap["created_at"] == nil {
							test.Fatal("expected created_at")
						}
						if taskMap["modified_at"] == nil {
							test.Fatal("expected modified_at")
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Detail task")
						assertContains(test, output, "pending")
						assertContains(test, output, "medium")
					},
				},
			},
		},
		{
			Name: "modify_title_and_priority",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Original title", "priority=1"},
				},
				{
					Args: []string{"task", "modify", "$0.short_id", "New title", "priority=4"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["title"], "New title")
						assertEqual(test, m["priority"], float64(4))
						assertEqual(test, m["version"], float64(2))
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Modified task")
					},
				},
				{
					// Verify via info that the changes persisted
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["title"], "New title")
						assertEqual(test, m["priority"], float64(4))
					},
				},
			},
		},
		{
			Name: "create_task_has_empty_description",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Task without description"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["description"], "")
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["description"], "")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						// Empty description should not appear in text output
						assertNotContains(test, output, "Description:")
					},
				},
			},
		},
		{
			Name: "add_with_inline_description",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Described task", `description="This is the description"`},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["title"], "Described task")
						assertEqual(test, m["description"], "This is the description")
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["description"], "This is the description")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Description:")
						assertContains(test, output, "This is the description")
					},
				},
			},
		},
		{
			Name: "modify_set_description",
			Steps: []Step{
				{
					Args: []string{"task", "create", "No description yet"},
				},
				{
					Args: []string{"task", "modify", "$0.short_id", `description="Added later"`},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["description"], "Added later")
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["description"], "Added later")
					},
				},
			},
		},
		{
			Name: "modify_clear_description",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Has description", `description="Will be cleared"`},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["description"], "Will be cleared")
					},
				},
				{
					Args: []string{"task", "modify", "$0.short_id", `description=`},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["description"], "")
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["description"], "")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertNotContains(test, output, "Description:")
					},
				},
			},
		},
		{
			Name: "add_with_multiline_description",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Multi-line task", `description="Line one
Line two
Line three"`},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["description"], "Line one\nLine two\nLine three")
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Description:")
						assertContains(test, output, "Line one")
						assertContains(test, output, "Line two")
						assertContains(test, output, "Line three")
					},
				},
			},
		},
	}

	runScenarios(test, binPath, scenarios)
}
