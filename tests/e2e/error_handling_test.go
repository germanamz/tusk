package e2e

import "testing"

func TestErrorHandling(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "info_not_found",
			Steps: []Step{
				{
					Args:    []string{"task", "get", "nonexist"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "not found")
					},
				},
			},
		},
		{
			Name: "modify_not_found",
			Steps: []Step{
				{
					Args:    []string{"task", "modify", "nonexist", "New title"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "not found")
					},
				},
			},
		},
		{
			Name: "start_not_found",
			Steps: []Step{
				{
					Args:    []string{"task", "start", "nonexist"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "not found")
					},
				},
			},
		},
		{
			Name: "done_not_found",
			Steps: []Step{
				{
					Args:    []string{"task", "done", "nonexist"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "not found")
					},
				},
			},
		},
		{
			Name: "delete_not_found",
			Steps: []Step{
				{
					Args:    []string{"task", "delete", "nonexist"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "not found")
					},
				},
			},
		},
		{
			Name: "done_from_pending_invalid_transition",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Cannot skip to done"},
				},
				{
					// pending -> completed is not an allowed transition
					Args:    []string{"task", "done", "$0.short_id"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "not allowed")
					},
				},
			},
		},
		{
			Name: "add_no_args",
			Steps: []Step{
				{
					Args:    []string{"task", "create"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						// Cobra enforces MinimumNArgs(1) — error goes to stderr
						combined := result.Stderr + result.Stdout
						if combined == "" {
							test.Fatal("expected some error output")
						}
					},
				},
			},
		},
		{
			Name: "add_no_title_only_filters",
			Steps: []Step{
				{
					// Only key=value args, no free text for title
					Args:    []string{"task", "create", "priority=3"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "title is required")
					},
				},
			},
		},
		{
			Name: "annotate_not_found",
			Steps: []Step{
				{
					Args:    []string{"task", "annotate", "nonexist", "A note"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "not found")
					},
				},
			},
		},
		{
			Name: "start_from_completed_invalid_transition",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Already active"},
				},
				{
					Args: []string{"task", "start", "$0.short_id"},
				},
				{
					Args: []string{"task", "done", "$0.short_id"},
				},
				{
					// completed -> active is not an allowed transition
					Args:    []string{"task", "start", "$0.short_id"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "not allowed")
					},
				},
			},
		},
		{
			Name: "add_invalid_project",
			Steps: []Step{
				{
					Args:    []string{"task", "create", "Bad project", "project=nonexistent_project"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "not found")
					},
				},
			},
		},
		{
			Name: "invalid_filter_field",
			Steps: []Step{
				{
					Args:    []string{"task", "list", "badfield=value"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "unknown field")
					},
				},
			},
		},
		{
			Name: "start_already_active_is_idempotent",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Will be active"},
				},
				{
					Args: []string{"task", "start", "$0.short_id"},
				},
				{
					// active -> active: service skips workflow check for same-status,
					// so this succeeds as a no-op (just bumps version)
					Args: []string{"task", "start", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						assertEqual(test, mapped["status"], "active")
						assertEqual(test, mapped["version"], float64(3))
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Started task")
					},
				},
			},
		},
		{
			Name: "done_already_completed_is_idempotent",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Will be completed"},
				},
				{
					Args: []string{"task", "start", "$0.short_id"},
				},
				{
					Args: []string{"task", "done", "$0.short_id"},
				},
				{
					// completed -> completed: same-status, succeeds as no-op
					Args: []string{"task", "done", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						assertEqual(test, mapped["status"], "completed")
						assertEqual(test, mapped["version"], float64(4))
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Completed task")
					},
				},
			},
		},
		{
			Name: "delete_already_deleted_is_idempotent",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Will be deleted"},
				},
				{
					Args: []string{"task", "delete", "$0.short_id"},
				},
				{
					// deleted -> deleted: same-status, succeeds as no-op
					Args: []string{"task", "delete", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						assertEqual(test, mapped["status"], "deleted")
						assertEqual(test, mapped["version"], float64(3))
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Deleted task")
					},
				},
			},
		},
	}

	runScenarios(test, binPath, scenarios)
}
