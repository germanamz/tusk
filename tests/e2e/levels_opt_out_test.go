package e2e

import (
	"strings"
	"testing"
)

// TestLevelsOptOut exercises per-project taxonomy.disable=true: tasks in an
// opted-out project may omit a level; tasks in a non-opted project under the
// same workspace default must carry one.
func TestLevelsOptOut(test *testing.T) {
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
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if _, present := mapped["level"]; present {
							if mapped["level"] != nil && mapped["level"] != "" {
								test.Fatalf("expected no level on opted-out task, got: %v", mapped["level"])
							}
						}
					},
				},
				{
					// Default project still enforces the workspace taxonomy.
					Args:    []string{"task", "create", "needs level"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						if !strings.Contains(result.Stderr, "requires a level") {
							test.Fatalf("expected friendly taxonomy error, got: %q", result.Stderr)
						}
					},
				},
				{
					// project show confirms the opt-out placeholder.
					Args: []string{"project", "show", "legacy"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						if !strings.Contains(output, "disabled; project opted out") {
							test.Fatalf("expected opt-out placeholder, got:\n%s", output)
						}
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}
