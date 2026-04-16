package e2e

import (
	"strings"
	"testing"
)

func TestUDA(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_with_multiple_udas",
			Steps: []Step{
				{
					Args: []string{"task", "create", "UDA inline test", "uda.env=prod", "uda.region=eu"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created task")
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "env:")
						assertContains(t, output, "prod")
						assertContains(t, output, "region:")
						assertContains(t, output, "eu")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						uda, ok := m["uda"].(map[string]any)
						if !ok {
							t.Fatal("expected uda object in JSON")
						}
						assertEqual(t, uda["env"], "prod")
						assertEqual(t, uda["region"], "eu")
					},
				},
			},
		},
		{
			Name: "modify_add_uda",
			Steps: []Step{
				{
					Args: []string{"task", "create", "UDA modify base", "uda.env=prod"},
				},
				{
					Args: []string{"task", "modify", "$0.short_id", "uda.team=backend"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						uda := m["uda"].(map[string]any)
						assertEqual(t, uda["env"], "prod")
						assertEqual(t, uda["team"], "backend")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "env:")
						assertContains(t, output, "prod")
						assertContains(t, output, "team:")
						assertContains(t, output, "backend")
					},
				},
			},
		},
		{
			Name: "modify_delete_uda_via_empty",
			Steps: []Step{
				{
					Args: []string{"task", "create", "UDA delete test", "uda.env=prod", "uda.region=eu"},
				},
				{
					Args: []string{"task", "modify", "$0.short_id", "uda.env="},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						uda := m["uda"].(map[string]any)
						if _, exists := uda["env"]; exists {
							t.Fatal("expected env key to be removed")
						}
						assertEqual(t, uda["region"], "eu")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertNotContains(t, output, "env:")
						assertContains(t, output, "region:")
						assertContains(t, output, "eu")
					},
				},
			},
		},
		{
			Name: "duplicate_key_last_wins",
			Steps: []Step{
				{
					Args: []string{"task", "create", "UDA dup test", "uda.env=a", "uda.env=b"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						uda := m["uda"].(map[string]any)
						assertEqual(t, uda["env"], "b")
					},
				},
			},
		},
		{
			Name: "modify_overwrite_uda",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Overwrite test", "uda.env=dev"},
				},
				{
					Args: []string{"task", "modify", "$0.short_id", "uda.env=prod"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						uda := m["uda"].(map[string]any)
						assertEqual(t, uda["env"], "prod")
					},
				},
			},
		},
		{
			Name: "uda_in_list_json",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Listed task", "uda.env=prod"},
				},
				{
					Args: []string{"task", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) == 0 {
							t.Fatal("expected at least 1 task")
						}
						m := arr[0].(map[string]any)
						uda, ok := m["uda"].(map[string]any)
						if !ok {
							t.Fatal("expected uda in list JSON output")
						}
						assertEqual(t, uda["env"], "prod")
					},
				},
			},
		},
		{
			Name: "error_invalid_uda_key",
			Steps: []Step{
				{
					Args:    []string{"task", "create", "bad key", "uda.1env=x"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						combined := strings.ToLower(r.Stderr)
						if !strings.Contains(combined, "uda") {
							t.Fatalf("stderr should mention uda: %s", r.Stderr)
						}
					},
				},
			},
		},
		{
			Name: "error_unknown_field_with_hint",
			Steps: []Step{
				{
					Args:    []string{"task", "create", "unknown field", "env=prod"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "unknown field")
						assertStderrContains(t, r, "did you mean uda.env?")
					},
				},
			},
		},
		{
			Name: "error_unknown_dotted_field_no_hint",
			Steps: []Step{
				{
					Args:    []string{"task", "create", "dotted unknown", "foo.bar=1"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "unknown field")
						if strings.Contains(r.Stderr, "did you mean") {
							t.Fatalf("stderr should NOT contain hint: %s", r.Stderr)
						}
					},
				},
			},
		},
		{
			Name: "error_modifier_on_uda",
			Steps: []Step{
				{
					Args:    []string{"task", "create", "modifier uda", "+uda.env=prod"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "modifier")
					},
				},
			},
		},
		{
			Name: "error_stale_uda_flag",
			Steps: []Step{
				{
					Args:    []string{"task", "create", "--uda", "env=prod", "stale flag"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "unknown flag")
					},
				},
			},
		},
		{
			Name: "error_stale_u_shorthand",
			Steps: []Step{
				{
					Args:    []string{"task", "create", "-u", "env=prod", "stale shorthand"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						combined := strings.ToLower(r.Stderr)
						if !strings.Contains(combined, "unknown") {
							t.Fatalf("stderr should contain 'unknown': %s", r.Stderr)
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestUDAFilter(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "filter_uda_match",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Prod task", "uda.env=prod"},
				},
				{
					Args: []string{"task", "create", "Dev task", "uda.env=dev"},
				},
				{
					Args: []string{"task", "list", "uda.env=prod"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Prod task")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Prod task")
						assertNotContains(t, output, "Dev task")
					},
				},
			},
		},
		{
			Name: "filter_uda_and",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Prod backend", "uda.env=prod", "uda.team=backend"},
				},
				{
					Args: []string{"task", "create", "Prod frontend", "uda.env=prod", "uda.team=frontend"},
				},
				{
					Args: []string{"task", "list", "uda.env=prod", "uda.team=backend"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Prod backend")
					},
				},
			},
		},
		{
			Name: "filter_uda_absent",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Has env", "uda.env=prod"},
				},
				{
					Args: []string{"task", "create", "No env"},
				},
				{
					Args: []string{"task", "list", "uda.env="},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task (without env), got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "No env")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "No env")
						assertNotContains(t, output, "Has env")
					},
				},
			},
		},
		{
			Name: "filter_uda_no_match",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Some task", "uda.env=prod"},
				},
				{
					Args: []string{"task", "list", "uda.env=staging"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 0 {
							t.Fatalf("expected 0 tasks, got %d", len(arr))
						}
					},
				},
			},
		},
		{
			Name: "filter_uda_nonexistent_key",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Some task"},
				},
				{
					Args: []string{"task", "list", "uda.nonexistent=value"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 0 {
							t.Fatalf("expected 0 tasks, got %d", len(arr))
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
