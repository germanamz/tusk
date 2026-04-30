package e2e

import (
	"strings"
	"testing"
)

// TestLevelsLevelCheck exercises `tusk task level-check` end-to-end: filtering,
// JSON output, and exit-code semantics.
func TestLevelsLevelCheck(test *testing.T) {
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
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						if !strings.Contains(output, "missing") {
							test.Fatalf("expected 'missing' reason in text output, got:\n%s", output)
						}
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr, ok := parsed.([]any)
						if !ok {
							test.Fatalf("expected JSON array, got %T: %v", parsed, parsed)
						}
						if len(arr) != 1 {
							test.Fatalf("expected 1 violation, got %d", len(arr))
						}
						entry := arr[0].(map[string]any)
						if entry["reason"] != "missing" {
							test.Fatalf("reason: got %v, want missing", entry["reason"])
						}
						taxonomy, ok := entry["taxonomy"].(map[string]any)
						if !ok {
							test.Fatalf("expected taxonomy object, got: %v", entry)
						}
						if _, ok := taxonomy["ranks"]; !ok {
							test.Fatalf("expected taxonomy.ranks, got: %v", taxonomy)
						}
						if entry["source"] != "workspace_default" {
							test.Fatalf("source: got %v, want workspace_default", entry["source"])
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
	runScenarios(test, binPath, scenarios)
}
