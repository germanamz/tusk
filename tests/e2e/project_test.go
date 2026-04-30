package e2e

import (
	"testing"
)

func TestProjectCommands(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "project_list_default",
			Steps: []Step{
				{
					Args: []string{"project", "list"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "default")
						assertContains(test, output, "kanban")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) < 1 {
							test.Fatal("expected at least 1 project (default)")
						}
						found := false
						for _, item := range arr {
							mapped := item.(map[string]any)
							if mapped["id"] == "default" {
								found = true
								if mapped["workflow"] != "kanban" {
									test.Fatalf("expected workflow 'kanban', got %v", mapped["workflow"])
								}
								break
							}
						}
						if !found {
							test.Fatal("expected default project in list")
						}
					},
				},
			},
		},
	}

	runScenarios(test, binPath, scenarios)
}
