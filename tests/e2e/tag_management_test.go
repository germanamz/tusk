package e2e

import "testing"

func TestTagManagement(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "tag_create",
			Steps: []Step{
				{
					Args: []string{"tag", "create", "foo"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Created tag foo")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["name"], "foo")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["name"], "colored")
						assertEqual(test, m["color"], "#ff0000")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Created tag colored")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 2 {
							test.Fatalf("expected 2 tags, got %d", len(arr))
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "alpha")
						assertContains(test, output, "beta")
					},
				},
			},
		},
		{
			Name: "tag_list_with_usage",
			Steps: []Step{
				{Args: []string{"tag", "create", "tracked"}},
				{Args: []string{"task", "create", "Task one", "+tracked"}},
				{
					Args: []string{"tag", "list", "--usage"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 tag, got %d", len(arr))
						}
						m := arr[0].(map[string]any)
						assertEqual(test, m["name"], "tracked")
						assertEqual(test, m["task_count"], float64(1))
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "tracked")
						assertContains(test, output, "TASKS")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 colored tag, got %d", len(arr))
						}
						assertEqual(test, arr[0].(map[string]any)["name"], "red")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "red")
						assertNotContains(test, output, "plain")
					},
				},
				// Filter: only uncolored tags
				{
					Args: []string{"tag", "list", "--color", "none"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 uncolored tag, got %d", len(arr))
						}
						assertEqual(test, arr[0].(map[string]any)["name"], "plain")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "plain")
						assertNotContains(test, output, "red")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["name"], "tocolor")
						assertEqual(test, m["color"], "#00ff00")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Modified tag tocolor")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["name"], "clearme")
						if m["color"] != nil {
							test.Fatalf("expected nil color after clearing, got %v", m["color"])
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Modified tag clearme")
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
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Renamed tag oldname to newname")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["name"], "newname")
					},
				},
				// Verify old name is gone and new name exists
				{
					Args: []string{"tag", "list"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 tag, got %d", len(arr))
						}
						assertEqual(test, arr[0].(map[string]any)["name"], "newname")
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
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Deleted tag temp")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["name"], "temp")
					},
				},
				// Verify it's gone
				{
					Args: []string{"tag", "list"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 0 {
							test.Fatalf("expected 0 tags after delete, got %d", len(arr))
						}
					},
				},
			},
		},
		{
			Name: "tag_delete_in_use",
			Steps: []Step{
				{Args: []string{"task", "create", "My task", "+busy"}},
				{
					Args:    []string{"tag", "delete", "busy"},
					WantErr: true,
				},
			},
		},
	}

	runScenarios(test, binPath, scenarios)
}

func TestTagColorSetAndClear(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "tag_color_set_and_clear",
			Steps: []Step{
				{Args: []string{"tag", "create", "urgent"}},
				{
					Args: []string{"tag", "modify", "urgent", "--color", "#ff4444"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						assertEqual(test, m["color"], "#ff4444")
					},
				},
				{
					Args: []string{"tag", "list"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						found := false
						for _, item := range arr {
							m := item.(map[string]any)
							if m["name"] == "urgent" {
								assertEqual(test, m["color"], "#ff4444")
								found = true
							}
						}
						if !found {
							test.Fatal("tag 'urgent' not found in list")
						}
					},
				},
				{
					Args: []string{"tag", "modify", "urgent", "--color", ""},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						m := parsed.(map[string]any)
						if m["color"] != nil {
							test.Errorf("expected color to be null after clear, got %v", m["color"])
						}
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}
