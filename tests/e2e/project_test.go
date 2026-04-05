package e2e

import (
	"testing"
)

func TestProjectCommands(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "project_list_default",
			Steps: []Step{
				{
					Args: []string{"project", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "default")
						assertContains(t, output, "kanban")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) < 1 {
							t.Fatal("expected at least 1 project (default)")
						}
						found := false
						for _, item := range arr {
							m := item.(map[string]any)
							if m["id"] == "default" {
								found = true
								if m["workflow"] != "kanban" {
									t.Fatalf("expected workflow 'kanban', got %v", m["workflow"])
								}
								break
							}
						}
						if !found {
							t.Fatal("expected default project in list")
						}
					},
				},
			},
		},
		{
			Name: "project_create_removed",
			Steps: []Step{
				{
					Args: []string{"project", "create", "myproject"},
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						// "project create" no longer exists — Cobra prints usage
						combined := r.Stdout + r.Stderr
						assertContains(t, combined, "Usage")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
