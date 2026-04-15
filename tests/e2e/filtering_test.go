package e2e

import "testing"

func TestFiltering(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "list_status_active_only",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Pending task"},
				},
				{
					Args: []string{"task", "create", "Active task"},
				},
				{
					Args: []string{"task", "start", "$1.short_id"},
				},
				{
					Args: []string{"task", "list", "status=active"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 active task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Active task")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Active task")
						assertNotContains(t, output, "Pending task")
					},
				},
			},
		},
		{
			Name: "list_status_pending_and_active",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Pending one"},
				},
				{
					Args: []string{"task", "create", "Active one"},
				},
				{
					Args: []string{"task", "start", "$1.short_id"},
				},
				{
					Args: []string{"task", "list", "status=pending,active"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 2 {
							t.Fatalf("expected 2 tasks, got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Pending one")
						assertContains(t, output, "Active one")
					},
				},
			},
		},
		{
			Name: "list_include_tag",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Tagged task", "+api"},
				},
				{
					Args: []string{"task", "create", "Untagged task"},
				},
				{
					Args: []string{"task", "list", "+api"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 tagged task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Tagged task")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Tagged task")
						assertNotContains(t, output, "Untagged task")
					},
				},
			},
		},
		{
			Name: "list_exclude_tag",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Has docs tag", "+docs"},
				},
				{
					Args: []string{"task", "create", "No docs tag"},
				},
				{
					// "--" is required so cobra does not interpret "-docs" as a shorthand flag.
					Args: []string{"task", "list", "--", "-docs"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task without docs tag, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "No docs tag")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "No docs tag")
						assertNotContains(t, output, "Has docs tag")
					},
				},
			},
		},
		{
			Name: "list_priority_exact",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Low pri", "priority=1"},
				},
				{
					Args: []string{"task", "create", "High pri", "priority=3"},
				},
				{
					Args: []string{"task", "list", "priority=3"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "High pri")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "High pri")
						assertNotContains(t, output, "Low pri")
					},
				},
			},
		},
		{
			Name: "list_priority_range",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Low", "priority=1"},
				},
				{
					Args: []string{"task", "create", "Medium", "priority=2"},
				},
				{
					Args: []string{"task", "create", "High", "priority=3"},
				},
				{
					Args: []string{"task", "create", "Urgent", "priority=4"},
				},
				{
					Args: []string{"task", "list", "priority=3..4"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 2 {
							t.Fatalf("expected 2 tasks (high+urgent), got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertNotContains(t, output, "Low")
						assertNotContains(t, output, "Medium")
						// High and Urgent should be present
						assertContains(t, output, "H")
						assertContains(t, output, "U")
					},
				},
			},
		},
		{
			Name: "list_project_filter",
			Steps: []Step{
				{
					// default project is seeded by config; tasks without an explicit
					// project are also assigned to default, so both tasks appear.
					Args: []string{"task", "create", "First task", "project=default"},
				},
				{
					// No project arg — still gets default by the service layer.
					Args: []string{"task", "create", "Second task"},
				},
				{
					Args: []string{"task", "list", "project=default"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 2 {
							t.Fatalf("expected 2 tasks in default project, got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "First task")
						assertContains(t, output, "Second task")
					},
				},
			},
		},
		{
			Name: "list_combined_filters",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Match all", "priority=3", "+api"},
				},
				{
					Args: []string{"task", "create", "Wrong priority", "priority=1", "+api"},
				},
				{
					Args: []string{"task", "create", "Wrong tag", "priority=3", "+docs"},
				},
				{
					Args: []string{"task", "start", "$0.short_id"},
				},
				{
					Args: []string{"task", "list", "status=active", "+api", "priority=3"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task matching all filters, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Match all")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Match all")
						assertNotContains(t, output, "Wrong priority")
						assertNotContains(t, output, "Wrong tag")
					},
				},
			},
		},
		{
			Name: "list_no_results",
			Steps: []Step{
				{
					// No tasks in the DB — list should return empty
					Args: []string{"task", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 0 {
							t.Fatalf("expected 0 tasks, got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						if output != "" {
							t.Fatalf("expected empty output for no tasks, got:\n%s", output)
						}
					},
				},
			},
		},
		{
			Name: "list_default_hides_completed_and_deleted",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Pending visible"},
				},
				{
					Args: []string{"task", "create", "Will complete"},
				},
				{
					Args: []string{"task", "start", "$1.short_id"},
				},
				{
					Args: []string{"task", "done", "$1.short_id"},
				},
				{
					Args: []string{"task", "create", "Will delete"},
				},
				{
					Args: []string{"task", "delete", "$4.short_id"},
				},
				{
					// Default list should only show pending/active tasks
					Args: []string{"task", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 visible task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Pending visible")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Pending visible")
						assertNotContains(t, output, "Will complete")
						assertNotContains(t, output, "Will delete")
					},
				},
			},
		},
		{
			Name: "filter_by_title",
			Steps: []Step{
				{Args: []string{"task", "create", "Implement auth middleware"}},
				{Args: []string{"task", "create", "Write unit tests"}},
				{
					Args: []string{"task", "list", `title="auth"`, "status=pending"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Implement auth middleware")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "auth middleware")
						assertNotContains(t, output, "unit tests")
					},
				},
			},
		},
		{
			Name: "filter_by_description",
			Steps: []Step{
				{Args: []string{"task", "create", "Task A", `description="handles authentication"`}},
				{Args: []string{"task", "create", "Task B", `description="handles logging"`}},
				{
					Args: []string{"task", "list", `description="authentication"`, "status=pending"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Task A")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Task A")
						assertNotContains(t, output, "Task B")
					},
				},
			},
		},
		{
			Name: "filter_or_operator",
			Steps: []Step{
				{Args: []string{"task", "create", "Active task"}},
				{Args: []string{"task", "start", "$0.short_id"}},
				{Args: []string{"task", "create", "Pending task"}},
				{Args: []string{"task", "create", "Done task"}},
				{Args: []string{"task", "start", "$3.short_id"}},
				{Args: []string{"task", "done", "$3.short_id"}},
				{
					Args: []string{"task", "list", "status=active", "OR", "status=completed"},
					AssertJSON: func(t *testing.T, parsed any) {
						arr := jsonArray(t, parsed)
						if len(arr) != 2 {
							t.Fatalf("expected 2 tasks (active + completed), got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						assertContains(t, output, "Active task")
						assertContains(t, output, "Done task")
						assertNotContains(t, output, "Pending task")
					},
				},
			},
		},
		{
			Name: "filter_not_operator",
			Steps: []Step{
				{Args: []string{"task", "create", "Keep this"}},
				{Args: []string{"task", "create", "Delete this"}},
				{Args: []string{"task", "delete", "$1.short_id"}},
				{
					Args: []string{"task", "list", "NOT", "status=deleted"},
					AssertJSON: func(t *testing.T, parsed any) {
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Keep this")
					},
					AssertText: func(t *testing.T, output string) {
						assertContains(t, output, "Keep this")
						assertNotContains(t, output, "Delete this")
					},
				},
			},
		},
		{
			Name: "filter_parenthesized_grouping",
			Steps: []Step{
				{Args: []string{"task", "create", "Active tagged", "+api"}},
				{Args: []string{"task", "start", "$0.short_id"}},
				{Args: []string{"task", "create", "Pending tagged", "+api"}},
				{Args: []string{"task", "create", "Active untagged"}},
				{Args: []string{"task", "start", "$3.short_id"}},
				{
					// Only active tasks with +api tag, or any pending task
					Args: []string{"task", "list", "(", "status=active", "AND", "+api", ")", "OR", "status=pending"},
					AssertJSON: func(t *testing.T, parsed any) {
						arr := jsonArray(t, parsed)
						if len(arr) != 2 {
							t.Fatalf("expected 2 tasks, got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						assertContains(t, output, "Active tagged")
						assertContains(t, output, "Pending tagged")
						assertNotContains(t, output, "Active untagged")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
