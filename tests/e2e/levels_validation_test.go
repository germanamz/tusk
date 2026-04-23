package e2e

import (
	"strings"
	"testing"
)

// TestLevelsValidation exercises all four TaxonomyError reasons end-to-end:
// missing, unknown_level, root_requires_top_rank, parent_rank_not_lower.
func TestLevelsValidation(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "missing_level_rejected",
			Steps: []Step{
				{Args: []string{"config", "set", "taxonomy.levels", "milestone:story:(task,spike)"}},
				{
					Args:    []string{"task", "create", "no level"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						if !strings.Contains(r.Stderr, "requires a level") {
							t.Fatalf("expected 'requires a level', got: %q", r.Stderr)
						}
					},
				},
			},
		},
		{
			Name: "unknown_level_rejected",
			Steps: []Step{
				{Args: []string{"config", "set", "taxonomy.levels", "milestone:story"}},
				{
					Args:    []string{"task", "create", "bogus", "level=bogus"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						if !strings.Contains(r.Stderr, "not in the taxonomy") {
							t.Fatalf("expected 'not in the taxonomy', got: %q", r.Stderr)
						}
					},
				},
			},
		},
		{
			Name: "root_requires_top_rank",
			Steps: []Step{
				{Args: []string{"config", "set", "taxonomy.levels", "milestone:story"}},
				{
					Args:    []string{"task", "create", "orphan story", "level=story"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						if !strings.Contains(r.Stderr, "top-rank level") {
							t.Fatalf("expected 'top-rank level', got: %q", r.Stderr)
						}
					},
				},
			},
		},
		{
			Name: "parent_rank_not_lower",
			Steps: []Step{
				{Args: []string{"config", "set", "taxonomy.levels", "milestone:story"}},
				{Args: []string{"task", "create", "root", "level=milestone"}},
				{
					Args:    []string{"task", "create", "sibling milestone", "level=milestone", "parent=$1.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						if !strings.Contains(r.Stderr, "strictly lower") {
							t.Fatalf("expected 'strictly lower', got: %q", r.Stderr)
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
