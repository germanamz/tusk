package e2e

import (
	"strings"
	"testing"
)

func TestUDA(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_with_multiple_udas",
			Steps: []Step{
				{
					Args: []string{"task", "create", "UDA inline test", "uda.env=prod", "uda.region=eu"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Created task")
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "env:")
						assertContains(test, output, "prod")
						assertContains(test, output, "region:")
						assertContains(test, output, "eu")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						uda, ok := mapped["uda"].(map[string]any)
						if !ok {
							test.Fatal("expected uda object in JSON")
						}
						assertEqual(test, uda["env"], "prod")
						assertEqual(test, uda["region"], "eu")
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
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Modified task")
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						uda := mapped["uda"].(map[string]any)
						assertEqual(test, uda["env"], "prod")
						assertEqual(test, uda["team"], "backend")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "env:")
						assertContains(test, output, "prod")
						assertContains(test, output, "team:")
						assertContains(test, output, "backend")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						uda := mapped["uda"].(map[string]any)
						if _, exists := uda["env"]; exists {
							test.Fatal("expected env key to be removed")
						}
						assertEqual(test, uda["region"], "eu")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertNotContains(test, output, "env:")
						assertContains(test, output, "region:")
						assertContains(test, output, "eu")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						uda := mapped["uda"].(map[string]any)
						assertEqual(test, uda["env"], "b")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						uda := mapped["uda"].(map[string]any)
						assertEqual(test, uda["env"], "prod")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) == 0 {
							test.Fatal("expected at least 1 task")
						}
						mapped := arr[0].(map[string]any)
						uda, ok := mapped["uda"].(map[string]any)
						if !ok {
							test.Fatal("expected uda in list JSON output")
						}
						assertEqual(test, uda["env"], "prod")
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						combined := strings.ToLower(result.Stderr)
						if !strings.Contains(combined, "uda") {
							test.Fatalf("stderr should mention uda: %s", result.Stderr)
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "unknown field")
						assertStderrContains(test, result, "did you mean uda.env?")
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "unknown field")
						if strings.Contains(result.Stderr, "did you mean") {
							test.Fatalf("stderr should NOT contain hint: %s", result.Stderr)
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "modifier")
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "unknown flag")
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						combined := strings.ToLower(result.Stderr)
						if !strings.Contains(combined, "unknown") {
							test.Fatalf("stderr should contain 'unknown': %s", result.Stderr)
						}
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}

func TestUDAFilter(test *testing.T) {
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 task, got %d", len(arr))
						}
						assertEqual(test, arr[0].(map[string]any)["title"], "Prod task")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Prod task")
						assertNotContains(test, output, "Dev task")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 task, got %d", len(arr))
						}
						assertEqual(test, arr[0].(map[string]any)["title"], "Prod backend")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 task (without env), got %d", len(arr))
						}
						assertEqual(test, arr[0].(map[string]any)["title"], "No env")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "No env")
						assertNotContains(test, output, "Has env")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 0 {
							test.Fatalf("expected 0 tasks, got %d", len(arr))
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 0 {
							test.Fatalf("expected 0 tasks, got %d", len(arr))
						}
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}
