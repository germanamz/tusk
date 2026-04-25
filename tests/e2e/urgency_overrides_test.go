package e2e

import (
	"testing"
)

func TestUrgencyOverrides(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "set_single_key",
			Steps: []Step{
				{Args: []string{"task", "create", "Task"}},                                      // 0
				{Args: []string{"task", "modify", "$0.short_id", "urgency.blocking-weight=20"}}, // 1
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						overrides, ok := m["urgency_overrides"].(map[string]any)
						if !ok {
							t.Fatalf("expected urgency_overrides object, got %v", m["urgency_overrides"])
						}
						v, ok := overrides["blocking_weight"].(float64)
						if !ok {
							t.Fatalf("expected blocking_weight number, got %v", overrides["blocking_weight"])
						}
						if v != 20 {
							t.Fatalf("expected blocking_weight=20, got %v", v)
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if v, ok := m["urgency_overrides"]; ok && v != nil {
							t.Fatalf("expected urgency_overrides absent/nil after clearing only key, got %v", v)
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if v, ok := m["urgency_overrides"]; ok && v != nil {
							t.Fatalf("expected urgency_overrides absent after clear=true, got %v", v)
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						overrides, ok := m["urgency_overrides"].(map[string]any)
						if !ok {
							t.Fatalf("expected urgency_overrides set after delta, got %v", m["urgency_overrides"])
						}
						v, ok := overrides["blocking_weight"].(float64)
						if !ok {
							t.Fatalf("expected blocking_weight number, got %v", overrides["blocking_weight"])
						}
						if v != 15 {
							t.Fatalf("expected self blocking_weight=15 (inherited 10 + delta 5), got %v", v)
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						overrides, ok := m["urgency_overrides"].(map[string]any)
						if !ok {
							t.Fatalf("expected urgency_overrides set, got %v", m["urgency_overrides"])
						}
						v, ok := overrides["blocking_weight"].(float64)
						if !ok {
							t.Fatalf("expected blocking_weight number, got %v", overrides["blocking_weight"])
						}
						if v != 25 {
							t.Fatalf("expected blocking_weight=25 (20 + 5 delta), got %v", v)
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						var grandchild map[string]any
						for _, item := range arr {
							m := item.(map[string]any)
							if m["title"] == "Grandchild" {
								grandchild = m
								break
							}
						}
						if grandchild == nil {
							t.Fatalf("grandchild not found in list output")
						}
						eff, ok := grandchild["effective_urgency_weights"].(map[string]any)
						if !ok {
							t.Fatalf("expected effective_urgency_weights on grandchild, got %v", grandchild["effective_urgency_weights"])
						}
						v, ok := eff["priority_weight"].(float64)
						if !ok {
							t.Fatalf("expected priority_weight number, got %v", eff["priority_weight"])
						}
						if v != 100 {
							t.Fatalf("expected inherited priority_weight=100, got %v", v)
						}
						if u, ok := grandchild["urgency_overrides"]; ok && u != nil {
							t.Fatalf("expected urgency_overrides absent on grandchild, got %v", u)
						}
						urg, ok := grandchild["urgency"].(float64)
						if !ok {
							t.Fatalf("expected urgency number, got %v", grandchild["urgency"])
						}
						// Priority 2 / max 4 * inherited weight 100 = 50.
						if urg < 40 {
							t.Fatalf("expected urgency to reflect inherited priority_weight=100 boost, got %v", urg)
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
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if v, ok := m["effective_urgency_weights"]; ok && v != nil {
							t.Fatalf("expected effective_urgency_weights absent on isolated task, got %v", v)
						}
						if v, ok := m["urgency_overrides"]; ok && v != nil {
							t.Fatalf("expected urgency_overrides absent on isolated task, got %v", v)
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
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Urgency Overrides:")
						assertContains(t, output, "Effective Urgency Weights:")
						assertContains(t, output, "blocking_weight")
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
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "urgency.blocking-weight")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
