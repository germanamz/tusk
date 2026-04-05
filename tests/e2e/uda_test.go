package e2e

import "testing"

func TestUDA(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "add_with_uda",
			Steps: []Step{
				{
					Args: []string{"add", "UDA task", "--uda", "env=prod", "--uda", "team=backend"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created task")
					},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "UDA:")
						assertContains(t, output, "env:")
						assertContains(t, output, "prod")
						assertContains(t, output, "team:")
						assertContains(t, output, "backend")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						uda, ok := m["uda"].(map[string]any)
						if !ok {
							t.Fatal("expected uda object in JSON")
						}
						assertEqual(t, uda["env"], "prod")
						assertEqual(t, uda["team"], "backend")
					},
				},
			},
		},
		{
			Name: "modify_merge_uda",
			Steps: []Step{
				{
					Args: []string{"add", "Merge test", "--uda", "env=dev"},
				},
				{
					Args: []string{"modify", "$0.short_id", "--uda", "team=backend"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						uda := m["uda"].(map[string]any)
						assertEqual(t, uda["env"], "dev")
						assertEqual(t, uda["team"], "backend")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "env:")
						assertContains(t, output, "dev")
						assertContains(t, output, "team:")
						assertContains(t, output, "backend")
					},
				},
			},
		},
		{
			Name: "modify_overwrite_uda",
			Steps: []Step{
				{
					Args: []string{"add", "Overwrite test", "--uda", "env=dev"},
				},
				{
					Args: []string{"modify", "$0.short_id", "--uda", "env=prod"},
				},
				{
					Args: []string{"info", "$0.short_id"},
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
			Name: "modify_clear_uda_key",
			Steps: []Step{
				{
					Args: []string{"add", "Clear test", "--uda", "env=prod", "--uda", "team=backend"},
				},
				{
					Args: []string{"modify", "$0.short_id", "--uda", "env="},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						uda := m["uda"].(map[string]any)
						if _, exists := uda["env"]; exists {
							t.Fatal("expected env key to be removed")
						}
						assertEqual(t, uda["team"], "backend")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertNotContains(t, output, "env:")
						assertContains(t, output, "team:")
					},
				},
			},
		},
		{
			Name: "uda_in_list_json",
			Steps: []Step{
				{
					Args: []string{"add", "Listed task", "--uda", "env=prod"},
				},
				{
					Args: []string{"list"},
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
			Name: "invalid_uda_format",
			Steps: []Step{
				{
					Args:    []string{"add", "Bad UDA", "--uda", "noequals"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "invalid UDA format")
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
					Args: []string{"add", "Prod task", "--uda", "env=prod"},
				},
				{
					Args: []string{"add", "Dev task", "--uda", "env=dev"},
				},
				{
					Args: []string{"list", "uda.env:prod"},
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
					Args: []string{"add", "Prod backend", "--uda", "env=prod", "--uda", "team=backend"},
				},
				{
					Args: []string{"add", "Prod frontend", "--uda", "env=prod", "--uda", "team=frontend"},
				},
				{
					Args: []string{"list", "uda.env:prod", "uda.team:backend"},
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
					Args: []string{"add", "Has env", "--uda", "env=prod"},
				},
				{
					Args: []string{"add", "No env"},
				},
				{
					Args: []string{"list", "uda.env:"},
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
					Args: []string{"add", "Some task", "--uda", "env=prod"},
				},
				{
					Args: []string{"list", "uda.env:staging"},
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
					Args: []string{"add", "Some task"},
				},
				{
					Args: []string{"list", "uda.nonexistent:value"},
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
