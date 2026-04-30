package e2e

import (
	"testing"
)

func TestUrgencyOverrides(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "set_single_key",
			Steps: []Step{
				{Args: []string{"task", "create", "Task"}},                                      // 0
				{Args: []string{"task", "modify", "$0.short_id", "urgency.blocking-weight=20"}}, // 1
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						overrides, ok := mapped["urgency_overrides"].(map[string]any)
						if !ok {
							test.Fatalf("expected urgency_overrides object, got %v", mapped["urgency_overrides"])
						}
						v, ok := overrides["blocking_weight"].(float64)
						if !ok {
							test.Fatalf("expected blocking_weight number, got %v", overrides["blocking_weight"])
						}
						if v != 20 {
							test.Fatalf("expected blocking_weight=20, got %v", v)
						}
					},
				},
			},
		},
		{
			Name: "clear_single_key",
			Steps: []Step{
				{Args: []string{"task", "create", "Task"}},                                      // 0
				{Args: []string{"task", "modify", "$0.short_id", "urgency.blocking-weight=20"}}, // 1
				{Args: []string{"task", "modify", "$0.short_id", "urgency.blocking-weight="}},   // 2
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if v, ok := mapped["urgency_overrides"]; ok && v != nil {
							test.Fatalf("expected urgency_overrides absent/nil after clearing only key, got %v", v)
						}
					},
				},
			},
		},
		{
			Name: "clear_all",
			Steps: []Step{
				{Args: []string{"task", "create", "Task"}}, // 0
				{Args: []string{"task", "modify", "$0.short_id", "urgency.blocking-weight=20", "urgency.priority-weight=15"}}, // 1
				{Args: []string{"task", "modify", "$0.short_id", "urgency.clear=true"}},                                       // 2
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if v, ok := mapped["urgency_overrides"]; ok && v != nil {
							test.Fatalf("expected urgency_overrides absent after clear=true, got %v", v)
						}
					},
				},
			},
		},
		{
			Name: "delta_against_inherited",
			Steps: []Step{
				// Configure project-level blocking_weight=10 on default project.
				{Args: []string{"project", "modify", "default", "urgency.blocking-weight=10"}}, // 0
				{Args: []string{"task", "create", "Task"}},                                     // 1
				// Apply +5 delta with no self value — should resolve to inherited 10 + 5 = 15.
				{Args: []string{"task", "modify", "$1.short_id", "+urgency.blocking-weight=5"}}, // 2
				{
					Args: []string{"task", "get", "$1.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						overrides, ok := mapped["urgency_overrides"].(map[string]any)
						if !ok {
							test.Fatalf("expected urgency_overrides set after delta, got %v", mapped["urgency_overrides"])
						}
						v, ok := overrides["blocking_weight"].(float64)
						if !ok {
							test.Fatalf("expected blocking_weight number, got %v", overrides["blocking_weight"])
						}
						if v != 15 {
							test.Fatalf("expected self blocking_weight=15 (inherited 10 + delta 5), got %v", v)
						}
					},
				},
			},
		},
		{
			Name: "delta_against_self_value",
			Steps: []Step{
				{Args: []string{"task", "create", "Task"}},                                      // 0
				{Args: []string{"task", "modify", "$0.short_id", "urgency.blocking-weight=20"}}, // 1
				{Args: []string{"task", "modify", "$0.short_id", "+urgency.blocking-weight=5"}}, // 2
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						overrides, ok := mapped["urgency_overrides"].(map[string]any)
						if !ok {
							test.Fatalf("expected urgency_overrides set, got %v", mapped["urgency_overrides"])
						}
						v, ok := overrides["blocking_weight"].(float64)
						if !ok {
							test.Fatalf("expected blocking_weight number, got %v", overrides["blocking_weight"])
						}
						if v != 25 {
							test.Fatalf("expected blocking_weight=25 (20 + 5 delta), got %v", v)
						}
					},
				},
			},
		},
		{
			Name: "subtree_inheritance",
			Steps: []Step{
				{Args: []string{"task", "create", "Milestone", "priority=2"}},                        // 0
				{Args: []string{"task", "modify", "$0.short_id", "urgency.priority-weight=100"}},     // 1
				{Args: []string{"task", "create", "Child", "parent=$0.short_id", "priority=2"}},      // 2
				{Args: []string{"task", "create", "Grandchild", "parent=$2.short_id", "priority=2"}}, // 3
				{
					Args: []string{"task", "list"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						var grandchild map[string]any
						for _, item := range arr {
							mapped := item.(map[string]any)
							if mapped["title"] == "Grandchild" {
								grandchild = mapped
								break
							}
						}
						if grandchild == nil {
							test.Fatalf("grandchild not found in list output")
						}
						eff, ok := grandchild["effective_urgency_weights"].(map[string]any)
						if !ok {
							test.Fatalf("expected effective_urgency_weights on grandchild, got %v", grandchild["effective_urgency_weights"])
						}
						v, ok := eff["priority_weight"].(float64)
						if !ok {
							test.Fatalf("expected priority_weight number, got %v", eff["priority_weight"])
						}
						if v != 100 {
							test.Fatalf("expected inherited priority_weight=100, got %v", v)
						}
						if urgOverride, ok := grandchild["urgency_overrides"]; ok && urgOverride != nil {
							test.Fatalf("expected urgency_overrides absent on grandchild, got %v", urgOverride)
						}
						urg, ok := grandchild["urgency"].(float64)
						if !ok {
							test.Fatalf("expected urgency number, got %v", grandchild["urgency"])
						}
						// Priority 2 / max 4 * inherited weight 100 = 50.
						if urg < 40 {
							test.Fatalf("expected urgency to reflect inherited priority_weight=100 boost, got %v", urg)
						}
					},
				},
			},
		},
		{
			Name: "sibling_without_inheritance",
			Steps: []Step{
				{Args: []string{"task", "create", "Milestone"}},                                  // 0
				{Args: []string{"task", "modify", "$0.short_id", "urgency.priority-weight=100"}}, // 1
				// Independent task — different subtree, no chain overrides.
				{Args: []string{"task", "create", "Lonely"}}, // 2
				{
					Args: []string{"task", "get", "$2.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if v, ok := mapped["effective_urgency_weights"]; ok && v != nil {
							test.Fatalf("expected effective_urgency_weights absent on isolated task, got %v", v)
						}
						if v, ok := mapped["urgency_overrides"]; ok && v != nil {
							test.Fatalf("expected urgency_overrides absent on isolated task, got %v", v)
						}
					},
				},
			},
		},
		{
			Name: "text_mode_renders_sections",
			Steps: []Step{
				{Args: []string{"task", "create", "Task"}},                                      // 0
				{Args: []string{"task", "modify", "$0.short_id", "urgency.blocking-weight=20"}}, // 1
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Urgency Overrides:")
						assertContains(test, output, "Effective Urgency Weights:")
						assertContains(test, output, "blocking_weight")
					},
				},
			},
		},
		{
			Name: "task_create_rejects_urgency",
			Steps: []Step{
				{
					Args:    []string{"task", "create", "X", "urgency.blocking-weight=5"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "urgency.blocking-weight")
					},
				},
			},
		},
	}

	runScenarios(test, binPath, scenarios)
}
