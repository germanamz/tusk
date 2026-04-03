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
					// Only key:value args, no free text for title
					Args:    []string{"add", "priority:3"},
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
					Args:    []string{"add", "Bad project", "project:nonexistent_project"},
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
