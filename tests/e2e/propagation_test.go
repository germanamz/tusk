package e2e

import (
	"testing"
)

func TestPropagation_Disabled(t *testing.T) {
	// Propagation is disabled by default — completing all children should NOT
	// auto-complete the parent.
	scenarios := []Scenario{
		{
			Name: "propagation_disabled_by_default",
			Steps: []Step{
				// Step 0: Create parent
				{Args: []string{"add", "Parent task"}},
				// Step 1: Start parent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create child
				{Args: []string{"add", "Child task", "parent:$0.short_id"}},
				// Step 3: Start child
				{Args: []string{"start", "$2.short_id"}},
				// Step 4: Complete child
				{Args: []string{"done", "$2.short_id"}},
				// Step 5: Check parent — should still be active
				{
					Args: []string{"info", "$0.short_id"},
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stdout, "active")
						assertNotContains(t, r.Stdout, "completed")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "active" {
							t.Fatalf("expected parent status 'active', got %v", m["status"])
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
