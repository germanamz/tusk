package e2e

import (
	"strings"
	"testing"
)

// TestLevelsReassign moves a task between projects with differing taxonomies.
// Moving between projects whose taxonomies share the task's level succeeds;
// moving to a project whose taxonomy does not contain the level is rejected.
func TestLevelsReassign(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "reassign_compatible_and_incompatible",
			Steps: []Step{
				{Args: []string{"config", "set", "taxonomy.levels", "milestone:story"}},
				{Args: []string{"project", "create", "alpha", "workflow=kanban"}},
				{Args: []string{"project", "create", "beta", "workflow=kanban"}},
				{Args: []string{"project", "modify", "beta", "taxonomy.levels=red:blue"}},
				{
					// Task with "milestone" lives in project alpha (inherits workspace default).
					Args: []string{"task", "create", "work", "level=milestone", "project=alpha"},
				},
				{
					// Reassign to default — still has milestone — must succeed.
					Args: []string{"task", "modify", "$4.short_id", "project=default"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["project_id"], "default")
					},
				},
				{
					// Reassign to beta — its taxonomy lacks "milestone" — must fail.
					Args:    []string{"task", "modify", "$4.short_id", "project=beta"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						if !strings.Contains(r.Stderr, "not in the taxonomy") {
							t.Fatalf("expected taxonomy violation when reassigning to incompatible project, got: %q", r.Stderr)
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
