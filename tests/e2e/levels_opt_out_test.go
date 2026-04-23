package e2e

import (
	"strings"
	"testing"
)

// TestLevelsOptOut exercises per-project taxonomy.disable=true: tasks in an
// opted-out project may omit a level; tasks in a non-opted project under the
// same workspace default must carry one.
func TestLevelsOptOut(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "per_project_opt_out",
			Steps: []Step{
				{
					Args: []string{"config", "set", "taxonomy.levels", "milestone:story"},
				},
				{
					Args: []string{"project", "create", "legacy", "workflow=kanban"},
				},
				{
					Args: []string{"project", "modify", "legacy", "taxonomy.disable=true"},
				},
				{
					// Opted-out project accepts a level-less task.
					Args: []string{"task", "create", "legacy bug", "project=legacy"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if _, present := m["level"]; present {
							if m["level"] != nil && m["level"] != "" {
								t.Fatalf("expected no level on opted-out task, got: %v", m["level"])
							}
						}
					},
				},
				{
					// Default project still enforces the workspace taxonomy.
					Args:    []string{"task", "create", "needs level"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						if !strings.Contains(r.Stderr, "requires a level") {
							t.Fatalf("expected friendly taxonomy error, got: %q", r.Stderr)
						}
					},
				},
				{
					// project show confirms the opt-out placeholder.
					Args: []string{"project", "show", "legacy"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						if !strings.Contains(output, "disabled; project opted out") {
							t.Fatalf("expected opt-out placeholder, got:\n%s", output)
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
