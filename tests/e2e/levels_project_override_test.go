package e2e

import (
	"strings"
	"testing"
)

// TestLevelsProjectOverride sets a per-project taxonomy override and asserts
// that `tusk project show` reports the override with provenance, while MCP-
// facing responses emit `effective_taxonomy.source=project_override`.
func TestLevelsProjectOverride(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "project_override_surfaces_provenance",
			Steps: []Step{
				{
					Args: []string{"config", "set", "taxonomy.levels", "milestone:story"},
				},
				{
					Args: []string{"project", "create", "backend", "workflow=kanban"},
				},
				{
					Args: []string{"project", "modify", "backend", "taxonomy.levels=alpha:beta:gamma"},
				},
				{
					Args: []string{"project", "show", "backend"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						if !strings.Contains(output, "alpha:beta:gamma") {
							t.Fatalf("expected override taxonomy inline, got:\n%s", output)
						}
						if !strings.Contains(output, "project override") {
							t.Fatalf("expected 'project override' source, got:\n%s", output)
						}
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						eff, ok := m["effective_taxonomy"].(map[string]any)
						if !ok {
							t.Fatalf("expected effective_taxonomy object, got: %v", m)
						}
						assertEqual(t, eff["source"], "project_override")
					},
				},
				{
					// Default project should still report workspace provenance.
					Args: []string{"project", "show", "default"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						if !strings.Contains(output, "milestone:story") {
							t.Fatalf("expected workspace taxonomy inline, got:\n%s", output)
						}
						if !strings.Contains(output, "workspace default") {
							t.Fatalf("expected 'workspace default' source, got:\n%s", output)
						}
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						eff := m["effective_taxonomy"].(map[string]any)
						assertEqual(t, eff["source"], "workspace_default")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
