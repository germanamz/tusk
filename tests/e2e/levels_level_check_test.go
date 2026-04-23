package e2e

import (
	"strings"
	"testing"
)

// TestLevelsLevelCheck exercises `tusk task level-check` end-to-end: filtering,
// JSON output, and exit-code semantics.
func TestLevelsLevelCheck(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "level_check_clean_workspace",
			Steps: []Step{
				{Args: []string{"task", "level-check"}},
			},
		},
		{
			Name: "level_check_finds_violations_and_exits_nonzero",
			Steps: []Step{
				{
					// Seed a task BEFORE the taxonomy exists so it ends up violating.
					Args: []string{"task", "create", "Legacy work"},
				},
				{
					Args: []string{"config", "set", "taxonomy.levels", "milestone:story"},
				},
				{
					Args:    []string{"task", "level-check"},
					WantErr: true,
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						if !strings.Contains(output, "missing") {
							t.Fatalf("expected 'missing' reason in text output, got:\n%s", output)
						}
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr, ok := parsed.([]any)
						if !ok {
							t.Fatalf("expected JSON array, got %T: %v", parsed, parsed)
						}
						if len(arr) != 1 {
							t.Fatalf("expected 1 violation, got %d", len(arr))
						}
						entry := arr[0].(map[string]any)
						if entry["reason"] != "missing" {
							t.Fatalf("reason: got %v, want missing", entry["reason"])
						}
						tax, ok := entry["taxonomy"].(map[string]any)
						if !ok {
							t.Fatalf("expected taxonomy object, got: %v", entry)
						}
						if _, ok := tax["ranks"]; !ok {
							t.Fatalf("expected taxonomy.ranks, got: %v", tax)
						}
						if entry["source"] != "workspace_default" {
							t.Fatalf("source: got %v, want workspace_default", entry["source"])
						}
					},
				},
			},
		},
		{
			Name: "level_check_filter_scopes_results",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Legacy"},
				},
				{
					Args: []string{"config", "set", "taxonomy.levels", "milestone:story"},
				},
				{
					// Filter that does NOT match the seeded pending task → no violations.
					Args: []string{"task", "level-check", "status=completed"},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
