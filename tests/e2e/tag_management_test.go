package e2e

import "testing"

func TestTagManagement(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "tag_create",
			Steps: []Step{
				{
					Args: []string{"tag", "create", "foo"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created tag foo")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "foo")
					},
				},
				// Duplicate should fail
				{
					Args:    []string{"tag", "create", "foo"},
					WantErr: true,
				},
			},
		},
		{
			Name: "tag_create_with_color",
			Steps: []Step{
				{
					Args: []string{"tag", "create", "colored", "--color", "#ff0000"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "colored")
						assertEqual(t, m["color"], "#ff0000")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created tag colored")
					},
				},
			},
		},
		{
			Name: "tag_list",
			Steps: []Step{
				{Args: []string{"tag", "create", "alpha"}},
				{Args: []string{"tag", "create", "beta"}},
				{
					Args: []string{"tag", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 2 {
							t.Fatalf("expected 2 tags, got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "alpha")
						assertContains(t, output, "beta")
					},
				},
			},
		},
		{
			Name: "tag_list_with_usage",
			Steps: []Step{
				{Args: []string{"tag", "create", "tracked"}},
				{Args: []string{"add", "Task one", "+tracked"}},
				{
					Args: []string{"tag", "list", "--usage"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 tag, got %d", len(arr))
						}
						m := arr[0].(map[string]any)
						assertEqual(t, m["name"], "tracked")
						assertEqual(t, m["task_count"], float64(1))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "tracked")
						assertContains(t, output, "TASKS")
					},
				},
			},
		},
		{
			Name: "tag_list_filter_color",
			Steps: []Step{
				{Args: []string{"tag", "create", "red", "--color", "#ff0000"}},
				{Args: []string{"tag", "create", "plain"}},
				// Filter: only colored tags
				{
					Args: []string{"tag", "list", "--color", "any"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 colored tag, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["name"], "red")
					},
				},
				// Filter: only uncolored tags
				{
					Args: []string{"tag", "list", "--color", "none"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 uncolored tag, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["name"], "plain")
					},
				},
			},
		},
		{
			Name: "tag_modify_color",
			Steps: []Step{
				{Args: []string{"tag", "create", "tocolor"}},
				{
					Args: []string{"tag", "modify", "tocolor", "--color", "#00ff00"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "tocolor")
						assertEqual(t, m["color"], "#00ff00")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified tag tocolor")
					},
				},
			},
		},
		{
			Name: "tag_modify_clear_color",
			Steps: []Step{
				{Args: []string{"tag", "create", "clearme", "--color", "#aabbcc"}},
				{
					Args: []string{"tag", "modify", "clearme", "--color", ""},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "clearme")
						if m["color"] != nil {
							t.Fatalf("expected nil color after clearing, got %v", m["color"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified tag clearme")
					},
				},
			},
		},
		{
			Name: "tag_modify_no_flags",
			Steps: []Step{
				{Args: []string{"tag", "create", "noflag"}},
				{
					Args:    []string{"tag", "modify", "noflag"},
					WantErr: true,
				},
			},
		},
		{
			Name: "tag_rename",
			Steps: []Step{
				{Args: []string{"tag", "create", "oldname"}},
				{
					Args: []string{"tag", "rename", "oldname", "newname"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Renamed tag oldname to newname")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "newname")
					},
				},
				// Verify old name is gone and new name exists
				{
					Args: []string{"tag", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 tag, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["name"], "newname")
					},
				},
			},
		},
		{
			Name: "tag_rename_conflict",
			Steps: []Step{
				{Args: []string{"tag", "create", "aaa"}},
				{Args: []string{"tag", "create", "bbb"}},
				{
					Args:    []string{"tag", "rename", "aaa", "bbb"},
					WantErr: true,
				},
			},
		},
		{
			Name: "tag_delete",
			Steps: []Step{
				{Args: []string{"tag", "create", "temp"}},
				{
					Args: []string{"tag", "delete", "temp"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Deleted tag temp")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "temp")
					},
				},
				// Verify it's gone
				{
					Args: []string{"tag", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 0 {
							t.Fatalf("expected 0 tags after delete, got %d", len(arr))
						}
					},
				},
			},
		},
		{
			Name: "tag_delete_in_use",
			Steps: []Step{
				{Args: []string{"add", "My task", "+busy"}},
				{
					Args:    []string{"tag", "delete", "busy"},
					WantErr: true,
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
