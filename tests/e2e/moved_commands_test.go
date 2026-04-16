package e2e

import "testing"

func TestMovedCommandSuggestions(t *testing.T) {
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
	for _, m := range moved {
		suggest := m.suggest // capture for closure
		scenarios = append(scenarios, Scenario{
			Name: "moved_" + m.old,
			Steps: []Step{
				{
					Args:    []string{m.old},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						assertStderrContains(t, r, suggest)
					},
				},
			},
		})
	}

	runScenarios(t, binPath, scenarios)
}
