package e2e

import (
	"testing"
)

func TestUrgencySorting(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "list_sorted_by_urgency",
			Steps: []Step{
				// Create a low-priority task
				{Args: []string{"task", "create", "Low prio task", "priority=1"}},
				// Create a high-priority task
				{Args: []string{"task", "create", "High prio task", "priority=4"}},
				// Create a medium-priority task
				{Args: []string{"task", "create", "Med prio task", "priority=2"}},
				// List — should be sorted: high, med, low
				{
					Args: []string{"task", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 3 {
							t.Fatalf("expected 3 tasks, got %d", len(arr))
						}
						first := arr[0].(map[string]any)
						last := arr[len(arr)-1].(map[string]any)
						assertEqual(t, first["title"], "High prio task")
						assertEqual(t, last["title"], "Low prio task")

						// Verify urgency field is present and non-zero for all
						for _, item := range arr {
							m := item.(map[string]any)
							urg, ok := m["urgency"].(float64)
							if !ok {
								t.Fatal("urgency field missing or not a number")
							}
							if urg <= 0 {
								t.Errorf("expected positive urgency for %s, got %.2f", m["title"], urg)
							}
						}

						// Verify descending order
						firstUrg := first["urgency"].(float64)
						lastUrg := last["urgency"].(float64)
						if firstUrg <= lastUrg {
							t.Errorf("expected descending urgency: first=%.2f, last=%.2f", firstUrg, lastUrg)
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						// Verify Urg column header exists
						assertContains(t, output, "Urg")
						// Verify high priority task appears in output
						assertContains(t, output, "High prio task")
					},
				},
			},
		},
		{
			Name: "active_task_ranks_higher",
			Steps: []Step{
				// Create two equal-priority tasks
				{Args: []string{"task", "create", "Pending task", "priority=2"}},
				{Args: []string{"task", "create", "Active task", "priority=2"}},
				// Start the second task
				{Args: []string{"task", "start", "$1.short_id"}},
				// List — active should rank higher
				{
					Args: []string{"task", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 2 {
							t.Fatalf("expected 2 tasks, got %d", len(arr))
						}
						first := arr[0].(map[string]any)
						assertEqual(t, first["title"], "Active task")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}

func TestTaskNext(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "next_returns_highest_urgency",
			Steps: []Step{
				{Args: []string{"task", "create", "Low prio", "priority=1"}},
				{Args: []string{"task", "create", "High prio", "priority=4"}},
				{
					Args: []string{"task", "next"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "High prio")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "High prio")
					},
				},
			},
		},
		{
			Name: "next_no_actionable_tasks",
			Steps: []Step{
				{Args: []string{"task", "create", "Task 1"}},
				{Args: []string{"task", "start", "$0.short_id"}},
				{Args: []string{"task", "done", "$0.short_id"}},
				{
					Args:    []string{"task", "next"},
					WantErr: true,
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
