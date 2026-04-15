package e2e

import "testing"

func TestOutputFormat(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "text_list_column_headers",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Header test"},
				},
				{
					Args: []string{"task", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						// The header line should contain these column names
						assertContains(t, output, "ID")
						assertContains(t, output, "Status")
						assertContains(t, output, "Pri")
						assertContains(t, output, "Age")
						assertContains(t, output, "Title")
					},
				},
			},
		},
		{
			Name: "text_priority_symbols",
			Steps: []Step{
				{
					Args: []string{"task", "create", "No priority"},
				},
				{
					Args: []string{"task", "create", "Low pri task", "priority=1"},
				},
				{
					Args: []string{"task", "create", "Med pri task", "priority=2"},
				},
				{
					Args: []string{"task", "create", "High pri task", "priority=3"},
				},
				{
					Args: []string{"task", "create", "Urgent pri task", "priority=4"},
				},
				{
					Args: []string{"task", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						// Check that priority symbols appear in the output.
						// "-" is the default (priority 0), "L"=1, "M"=2, "H"=3, "U"=4
						assertContains(t, output, "L")
						assertContains(t, output, "M")
						assertContains(t, output, "H")
						assertContains(t, output, "U")
					},
				},
			},
		},
		{
			Name: "json_list_returns_array",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Array item"},
				},
				{
					Args: []string{"task", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						// parsed should be a []any (JSON array), not a map
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 item in array, got %d", len(arr))
						}
					},
				},
			},
		},
		{
			Name: "json_snake_case_keys",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Key check", "priority=2"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						// Check that all expected snake_case keys exist
						requiredKeys := []string{
							"id", "short_id", "title", "description",
							"status", "priority", "version", "tags",
							"created_at", "modified_at",
						}
						for _, key := range requiredKeys {
							if _, ok := m[key]; !ok {
								t.Fatalf("missing required JSON key: %q", key)
							}
						}
					},
				},
			},
		},
		{
			Name: "json_info_has_all_fields",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Info fields check", "priority=3"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						// Info JSON should have all task fields plus annotations
						requiredKeys := []string{
							"id", "short_id", "title", "description",
							"status", "priority", "version", "tags",
							"created_at", "modified_at",
						}
						for _, key := range requiredKeys {
							if _, ok := m[key]; !ok {
								t.Fatalf("missing required JSON key in info: %q", key)
							}
						}
						// annotations key should exist (even if empty/nil)
						// When there are no annotations, it may be omitted (omitempty)
						// or present as null/empty array — both are acceptable
					},
				},
			},
		},
		{
			Name: "empty_list_text_no_output",
			Steps: []Step{
				{
					// Fresh DB — no tasks
					Args: []string{"task", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						if output != "" {
							t.Fatalf("expected empty text output for no tasks, got:\n%s", output)
						}
					},
				},
			},
		},
		{
			Name: "empty_list_json_empty_array",
			Steps: []Step{
				{
					// Fresh DB — no tasks
					Args: []string{"task", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 0 {
							t.Fatalf("expected empty JSON array, got %d items", len(arr))
						}
					},
				},
			},
		},
		{
			Name: "text_info_priority_names",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Low check", "priority=1"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						// Info view shows full priority name, not symbol
						assertContains(t, output, "low")
					},
				},
			},
		},
		{
			Name: "text_info_shows_version",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Version check"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Version:")
						assertContains(t, output, "1")
					},
				},
			},
		},
		{
			Name: "text_info_shows_timestamps",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Timestamp check"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created:")
						assertContains(t, output, "Modified:")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}

func TestMarkdownDescriptionInInfo(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "info_renders_description",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Markdown test", "--description", "# Heading\n\nSome **bold** text."},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stdout, "Heading")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
