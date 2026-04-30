package e2e

import (
	"strings"
	"testing"
)

func TestConfigDBHydration(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "config_set_rejects_db_keys",
			Steps: []Step{
				{
					Args:    []string{"config", "set", "projects.foo.workflow", "kanban"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						combined := result.Stderr + result.Stdout
						if !strings.Contains(combined, "tusk project modify") {
							test.Fatalf("expected error to point at `tusk project modify`, got stderr=%q stdout=%q", result.Stderr, result.Stdout)
						}
					},
				},
				{
					Args:    []string{"config", "set", "workflows.kanban.statuses.pending.roles", "initial"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						combined := result.Stderr + result.Stdout
						if !strings.Contains(combined, "tusk workflow modify") {
							test.Fatalf("expected error to point at `tusk workflow modify`, got stderr=%q stdout=%q", result.Stderr, result.Stdout)
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
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "[projects.scratch]")
						assertContains(test, output, `workflow = "kanban"`)
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						root, ok := parsed.(map[string]any)
						if !ok {
							test.Fatalf("expected object, got %T", parsed)
						}
						projects, ok := root["projects"].(map[string]any)
						if !ok {
							test.Fatalf("projects missing or wrong type: %#v", root["projects"])
						}
						scratch, ok := projects["scratch"].(map[string]any)
						if !ok {
							test.Fatalf("projects.scratch missing: %#v", projects)
						}
						if scratch["workflow"] != "kanban" {
							test.Fatalf("projects.scratch.workflow = %v, want kanban", scratch["workflow"])
						}
					},
				},
			},
		},
	}

	runScenarios(test, binPath, scenarios)
}
