package e2e

import "testing"

func TestTags(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_with_tags",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Tagged task", "+api", "+backend"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						tags := mapped["tags"].([]any)
						if len(tags) != 2 {
							test.Fatalf("expected 2 tags, got %d", len(tags))
						}
						// Tags may be in any order, check both exist
						tagSet := map[string]bool{}
						for _, tag := range tags {
							tagSet[tag.(string)] = true
						}
						if !tagSet["api"] || !tagSet["backend"] {
							test.Fatalf("expected tags [api, backend], got %v", tags)
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Created task")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						tags := mapped["tags"].([]any)
						if len(tags) != 1 {
							test.Fatalf("expected 1 tag, got %d", len(tags))
						}
						assertEqual(test, tags[0], "newtag")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Modified task")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						tags := mapped["tags"].([]any)
						if len(tags) != 0 {
							test.Fatalf("expected 0 tags after removal, got %d: %v", len(tags), tags)
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Modified task")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 task, got %d", len(arr))
						}
						tags := arr[0].(map[string]any)["tags"].([]any)
						if len(tags) != 1 || tags[0] != "visible" {
							test.Fatalf("expected tags [visible], got %v", tags)
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "+visible")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						tags := mapped["tags"].([]any)
						if len(tags) != 2 {
							test.Fatalf("expected 2 tags, got %d", len(tags))
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Tags:")
						assertContains(test, output, "+frontend")
						assertContains(test, output, "+urgent")
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 task with tag, got %d", len(arr))
						}
						assertEqual(test, arr[0].(map[string]any)["title"], "Will get tag")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Will get tag")
						assertNotContains(test, output, "Never gets tag")
					},
				},
			},
		},
	}

	runScenarios(test, binPath, scenarios)
}
