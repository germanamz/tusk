package e2e

import "testing"

func TestMovedCommandSuggestions(test *testing.T) {
	moved := []struct {
		old     string
		suggest string
	}{
		{"add", "task create"},
		{"info", "task get"},
		{"list", "task list"},
		{"modify", "task modify"},
		{"tree", "task tree"},
		{"start", "task start"},
		{"done", "task done"},
		{"delete", "task delete"},
		{"next", "task next"},
		{"annotate", "task annotate"},
		{"claim", "task claim"},
		{"release", "task release"},
		{"available", "task available"},
		{"pop", "task pop"},
		{"link", "task link"},
		{"unlink", "task unlink"},
	}

	var scenarios []Scenario
	for _, moved := range moved {
		suggest := moved.suggest // capture for closure
		scenarios = append(scenarios, Scenario{
			Name: "moved_" + moved.old,
			Steps: []Step{
				{
					Args:    []string{moved.old},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						assertStderrContains(test, result, suggest)
					},
				},
			},
		})
	}

	runScenarios(test, binPath, scenarios)
}
