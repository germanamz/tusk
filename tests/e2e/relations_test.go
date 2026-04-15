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
					Args: []string{"task", "create", "Task A"},
				},
				// Step 1: Create task B
				{
					Args: []string{"task", "create", "Task B"},
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
					Args: []string{"task", "get", "$0.short_id"},
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
					Args: []string{"task", "get", "$0.short_id"},
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
					Args: []string{"task", "create", "Task X"},
				},
				{
					Args: []string{"task", "create", "Task Y"},
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
					Args: []string{"task", "create", "Task P"},
				},
				{
					Args: []string{"task", "create", "Task Q"},
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
					Args: []string{"task", "create", "Blocker task"},
				},
				// Step 1: Create task B
				{
					Args: []string{"task", "create", "Blocked task"},
				},
				// Step 2: A blocks B
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
				},
				// Step 3: Info on task B (the target) should show "blocked_by"
				{
					Args: []string{"task", "get", "$1.short_id"},
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

func TestRelationsCycleDetection(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "blocks_direct_cycle",
			Steps: []Step{
				// Step 0: Create task A
				{
					Args: []string{"task", "create", "Task A"},
				},
				// Step 1: Create task B
				{
					Args: []string{"task", "create", "Task B"},
				},
				// Step 2: A blocks B — succeeds
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
				},
				// Step 3: B blocks A — should fail (cycle)
				{
					Args:    []string{"link", "$1.short_id", "blocks", "$0.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "cycle")
					},
				},
			},
		},
		{
			Name: "blocks_transitive_cycle",
			Steps: []Step{
				// Step 0: Task A
				{
					Args: []string{"task", "create", "Task A"},
				},
				// Step 1: Task B
				{
					Args: []string{"task", "create", "Task B"},
				},
				// Step 2: Task C
				{
					Args: []string{"task", "create", "Task C"},
				},
				// Step 3: A blocks B
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
				},
				// Step 4: B blocks C
				{
					Args: []string{"link", "$1.short_id", "blocks", "$2.short_id"},
				},
				// Step 5: C blocks A — should fail (cycle: A->B->C->A)
				{
					Args:    []string{"link", "$2.short_id", "blocks", "$0.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "cycle")
					},
				},
			},
		},
		{
			Name: "blocks_chain_no_cycle",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Task A"},
				},
				{
					Args: []string{"task", "create", "Task B"},
				},
				{
					Args: []string{"task", "create", "Task C"},
				},
				// A blocks B — ok
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
				},
				// B blocks C — ok (chain, not a cycle)
				{
					Args: []string{"link", "$1.short_id", "blocks", "$2.short_id"},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestRelationsErrors(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "link_task_not_found",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Existing task"},
				},
				{
					Args:    []string{"link", "$0.short_id", "blocks", "nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "link_duplicate",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Task A"},
				},
				{
					Args: []string{"task", "create", "Task B"},
				},
				// First link — ok
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
				},
				// Second identical link — error
				{
					Args:    []string{"link", "$0.short_id", "blocks", "$1.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "already exists")
					},
				},
			},
		},
		{
			Name: "link_invalid_type",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Task A"},
				},
				{
					Args: []string{"task", "create", "Task B"},
				},
				{
					Args:    []string{"link", "$0.short_id", "depends_on", "$1.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "invalid relation type")
					},
				},
			},
		},
		{
			Name: "unlink_not_found",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Task A"},
				},
				{
					Args: []string{"task", "create", "Task B"},
				},
				{
					Args:    []string{"unlink", "$0.short_id", "blocks", "$1.short_id"},
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
