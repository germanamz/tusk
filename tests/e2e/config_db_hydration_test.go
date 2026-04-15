package e2e

import (
	"strings"
	"testing"
)

func TestConfigDBHydration(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "config_set_rejects_db_keys",
			Steps: []Step{
				{
					Args:    []string{"config", "set", "projects.foo.workflow", "kanban"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						combined := r.Stderr + r.Stdout
						if !strings.Contains(combined, "tusk project modify") {
							t.Fatalf("expected error to point at `tusk project modify`, got stderr=%q stdout=%q", r.Stderr, r.Stdout)
						}
					},
				},
				{
					Args:    []string{"config", "set", "workflows.kanban.statuses.pending.roles", "initial"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						combined := r.Stderr + r.Stdout
						if !strings.Contains(combined, "tusk workflow modify") {
							t.Fatalf("expected error to point at `tusk workflow modify`, got stderr=%q stdout=%q", r.Stderr, r.Stdout)
						}
					},
				},
			},
		},
		{
			Name: "config_show_includes_db_project",
			Steps: []Step{
				{
					Args: []string{"project", "create", "scratch", "workflow=kanban"},
				},
				{
					Args: []string{"config", "show"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "[projects.scratch]")
						assertContains(t, output, `workflow = "kanban"`)
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						root, ok := parsed.(map[string]any)
						if !ok {
							t.Fatalf("expected object, got %T", parsed)
						}
						projects, ok := root["projects"].(map[string]any)
						if !ok {
							t.Fatalf("projects missing or wrong type: %#v", root["projects"])
						}
						scratch, ok := projects["scratch"].(map[string]any)
						if !ok {
							t.Fatalf("projects.scratch missing: %#v", projects)
						}
						if scratch["workflow"] != "kanban" {
							t.Fatalf("projects.scratch.workflow = %v, want kanban", scratch["workflow"])
						}
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
