package e2e

import "testing"

func TestErrorHandling(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "info_not_found",
			Steps: []Step{
				{
					Args:    []string{"info", "nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "modify_not_found",
			Steps: []Step{
				{
					Args:    []string{"modify", "nonexist", "New title"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "start_not_found",
			Steps: []Step{
				{
					Args:    []string{"start", "nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "done_not_found",
			Steps: []Step{
				{
					Args:    []string{"done", "nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "delete_not_found",
			Steps: []Step{
				{
					Args:    []string{"delete", "nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "done_from_pending_invalid_transition",
			Steps: []Step{
				{
					Args: []string{"add", "Cannot skip to done"},
				},
				{
					// pending -> completed is not an allowed transition
					Args:    []string{"done", "$0.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not allowed")
					},
				},
			},
		},
		{
			Name: "add_no_args",
			Steps: []Step{
				{
					Args:    []string{"add"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						// Cobra enforces MinimumNArgs(1) — error goes to stderr
						combined := r.Stderr + r.Stdout
						if combined == "" {
							t.Fatal("expected some error output")
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
					Args:    []string{"add", "priority=3"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "title is required")
					},
				},
			},
		},
		{
			Name: "annotate_not_found",
			Steps: []Step{
				{
					Args:    []string{"annotate", "nonexist", "A note"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "start_from_completed_invalid_transition",
			Steps: []Step{
				{
					Args: []string{"add", "Already active"},
				},
				{
					Args: []string{"start", "$0.short_id"},
				},
				{
					Args: []string{"done", "$0.short_id"},
				},
				{
					// completed -> active is not an allowed transition
					Args:    []string{"start", "$0.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not allowed")
					},
				},
			},
		},
		{
			Name: "add_invalid_project",
			Steps: []Step{
				{
					Args:    []string{"add", "Bad project", "project=nonexistent_project"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "invalid_filter_field",
			Steps: []Step{
				{
					Args:    []string{"list", "badfield=value"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "unknown field")
					},
				},
			},
		},
		{
			Name: "start_already_active_is_idempotent",
			Steps: []Step{
				{
					Args: []string{"add", "Will be active"},
				},
				{
					Args: []string{"start", "$0.short_id"},
				},
				{
					// active -> active: service skips workflow check for same-status,
					// so this succeeds as a no-op (just bumps version)
					Args: []string{"start", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "active")
						assertEqual(t, m["version"], float64(3))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Started task")
					},
				},
			},
		},
		{
			Name: "done_already_completed_is_idempotent",
			Steps: []Step{
				{
					Args: []string{"add", "Will be completed"},
				},
				{
					Args: []string{"start", "$0.short_id"},
				},
				{
					Args: []string{"done", "$0.short_id"},
				},
				{
					// completed -> completed: same-status, succeeds as no-op
					Args: []string{"done", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "completed")
						assertEqual(t, m["version"], float64(4))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Completed task")
					},
				},
			},
		},
		{
			Name: "delete_already_deleted_is_idempotent",
			Steps: []Step{
				{
					Args: []string{"add", "Will be deleted"},
				},
				{
					Args: []string{"delete", "$0.short_id"},
				},
				{
					// deleted -> deleted: same-status, succeeds as no-op
					Args: []string{"delete", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "deleted")
						assertEqual(t, m["version"], float64(3))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Deleted task")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
