package e2e

import "testing"

func TestTags(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_with_tags",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Tagged task", "+api", "+backend"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						tags := m["tags"].([]any)
						if len(tags) != 2 {
							t.Fatalf("expected 2 tags, got %d", len(tags))
						}
						// Tags may be in any order, check both exist
						tagSet := map[string]bool{}
						for _, tag := range tags {
							tagSet[tag.(string)] = true
						}
						if !tagSet["api"] || !tagSet["backend"] {
							t.Fatalf("expected tags [api, backend], got %v", tags)
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created task")
					},
				},
			},
		},
		{
			Name: "modify_add_tag",
			Steps: []Step{
				{
					Args: []string{"task", "create", "No tags yet"},
				},
				{
					Args: []string{"task", "modify", "$0.short_id", "+newtag"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						tags := m["tags"].([]any)
						if len(tags) != 1 {
							t.Fatalf("expected 1 tag, got %d", len(tags))
						}
						assertEqual(t, tags[0], "newtag")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
			},
		},
		{
			Name: "modify_remove_tag",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Has tag", "+removeme"},
				},
				{
					Args: []string{"task", "modify", "$0.short_id", "--", "-removeme"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						tags := m["tags"].([]any)
						if len(tags) != 0 {
							t.Fatalf("expected 0 tags after removal, got %d: %v", len(tags), tags)
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
			},
		},
		{
			Name: "tags_in_list_output",
			Steps: []Step{
				{
					Args: []string{"task", "create", "List tag task", "+visible"},
				},
				{
					Args: []string{"task", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task, got %d", len(arr))
						}
						tags := arr[0].(map[string]any)["tags"].([]any)
						if len(tags) != 1 || tags[0] != "visible" {
							t.Fatalf("expected tags [visible], got %v", tags)
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "+visible")
					},
				},
			},
		},
		{
			Name: "tags_in_info_output",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Info tag task", "+frontend", "+urgent"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						tags := m["tags"].([]any)
						if len(tags) != 2 {
							t.Fatalf("expected 2 tags, got %d", len(tags))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Tags:")
						assertContains(t, output, "+frontend")
						assertContains(t, output, "+urgent")
					},
				},
			},
		},
		{
			Name: "filter_by_tag_after_modify",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Will get tag"},
				},
				{
					Args: []string{"task", "create", "Never gets tag"},
				},
				{
					Args: []string{"task", "modify", "$0.short_id", "+searchable"},
				},
				{
					Args: []string{"task", "list", "+searchable"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 task with tag, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["title"], "Will get tag")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Will get tag")
						assertNotContains(t, output, "Never gets tag")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
