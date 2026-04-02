// tests/e2e/task_lifecycle_test.go
package e2e

import "testing"

func TestTaskLifecycle(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_single_task",
			Steps: []Step{
				{
					Args: []string{"add", "Buy milk", "priority:3"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m, ok := parsed.(map[string]any)
						if !ok {
							t.Fatalf("expected JSON object, got %T", parsed)
						}
						assertEqual(t, m["title"], "Buy milk")
						assertEqual(t, m["status"], "pending")
						assertEqual(t, m["priority"], float64(3))
						if m["short_id"] == nil || m["short_id"] == "" {
							t.Fatal("expected short_id to be set")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created task")
					},
				},
			},
		},
		{
			Name: "create_then_start",
			Steps: []Step{
				{
					Args: []string{"add", "Reference test"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created task")
					},
				},
				{
					Args: []string{"start", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "active")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Started task")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
