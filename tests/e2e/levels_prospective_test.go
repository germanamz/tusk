package e2e

import (
	"strings"
	"testing"
)

// TestLevelsProspective exercises prospective-validation semantics: a task
// created before the workspace taxonomy exists stays valid; once a taxonomy
// is added the task surfaces under level-check, but unrelated modifications
// (title only) on the same task still succeed without re-validating.
func TestLevelsProspective(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "existing_task_surfaces_after_taxonomy_added",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Legacy task"},
				},
				{
					Args: []string{"config", "set", "taxonomy.levels", "milestone:story"},
				},
				{
					Args:    []string{"task", "level-check"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						if !strings.Contains(result.Stdout, "$0.short_id") && !strings.Contains(result.Stdout, "missing") {
							// short_id expansion happens before assert; just require "missing" reason.
							if !strings.Contains(result.Stdout, "missing") {
								test.Fatalf("expected missing reason in output, got: %q", result.Stdout)
							}
						}
					},
				},
				{
					// Modifying an unrelated field (title) on the same task
					// must succeed without re-validating taxonomy.
					Args: []string{"task", "modify", "$0.short_id", "title=Legacy task renamed"},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}
