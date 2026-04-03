package e2e

import (
	"testing"
)

func TestRelations(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "link_and_info",
			Steps: []Step{
				// Step 0: Create task A
				{
					Args: []string{"add", "Task A"},
				},
				// Step 1: Create task B
				{
					Args: []string{"add", "Task B"},
				},
				// Step 2: Link A blocks B
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["relation_type"], "blocks")
						if m["id"] == nil || m["id"] == "" {
							t.Fatal("expected relation id to be set")
						}
						if m["source_id"] == nil || m["source_id"] == "" {
							t.Fatal("expected source_id to be set")
						}
						if m["target_id"] == nil || m["target_id"] == "" {
							t.Fatal("expected target_id to be set")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Linked")
						assertContains(t, output, "blocks")
					},
				},
				// Step 3: Info on task A should show the relation
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						relations := m["relations"].([]any)
						if len(relations) != 1 {
							t.Fatalf("expected 1 relation, got %d", len(relations))
						}
						rel := relations[0].(map[string]any)
						assertEqual(t, rel["relation_type"], "blocks")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Relations:")
						assertContains(t, output, "blocks")
					},
				},
				// Step 4: Unlink A blocks B
				{
					Args: []string{"unlink", "$0.short_id", "blocks", "$1.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Unlinked")
					},
				},
				// Step 5: Info on task A should show no relations
				{
					Args: []string{"info", "$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertNotContains(t, output, "Relations:")
					},
				},
			},
		},
		{
			Name: "link_relates_to",
			Steps: []Step{
				{
					Args: []string{"add", "Task X"},
				},
				{
					Args: []string{"add", "Task Y"},
				},
				{
					Args: []string{"link", "$0.short_id", "relates_to", "$1.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Linked")
						assertContains(t, output, "relates_to")
					},
				},
			},
		},
		{
			Name: "link_duplicates",
			Steps: []Step{
				{
					Args: []string{"add", "Task P"},
				},
				{
					Args: []string{"add", "Task Q"},
				},
				{
					Args: []string{"link", "$0.short_id", "duplicates", "$1.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Linked")
						assertContains(t, output, "duplicates")
					},
				},
			},
		},
		{
			Name: "info_shows_inverse_relation",
			Steps: []Step{
				// Step 0: Create task A
				{
					Args: []string{"add", "Blocker task"},
				},
				// Step 1: Create task B
				{
					Args: []string{"add", "Blocked task"},
				},
				// Step 2: A blocks B
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
				},
				// Step 3: Info on task B (the target) should show "blocked_by"
				{
					Args: []string{"info", "$1.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "blocked_by")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
