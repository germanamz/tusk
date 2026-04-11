package e2e

import (
	"testing"
)

func TestTaskQueue(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "available_basic_filtering",
			Steps: []Step{
				// Step 0: Add task A (priority 3)
				{Args: []string{"add", "Task A", "priority=3"}},
				// Step 1: Add task B (priority 1)
				{Args: []string{"add", "Task B", "priority=1"}},
				// Step 2: Add task C (priority 2)
				{Args: []string{"add", "Task C", "priority=2"}},
				// Step 3: Claim task A by p1
				{Args: []string{"claim", "$0.short_id", "--player", "p1"}},
				// Step 4: List available for p2 — should see only B and C (unclaimed)
				{
					Args: []string{"available", "--player", "p2"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						items := jsonArray(t, parsed)
						if len(items) != 2 {
							t.Fatalf("expected 2 available tasks, got %d", len(items))
						}
					},
				},
			},
		},
		{
			Name: "available_blocked_excluded",
			Steps: []Step{
				// Step 0: Add blocker task
				{Args: []string{"add", "Blocker"}},
				// Step 1: Add blocked task
				{Args: []string{"add", "Blocked"}},
				// Step 2: Link blocker blocks blocked
				{Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"}},
				// Step 3: Available should only show the blocker (blocked is excluded)
				{
					Args: []string{"available", "--player", "p1"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						items := jsonArray(t, parsed)
						if len(items) != 1 {
							t.Fatalf("expected 1 available task, got %d", len(items))
						}
						m := items[0].(map[string]any)
						assertEqual(t, m["title"], "Blocker")
					},
				},
				// Step 4: Start the blocker
				{Args: []string{"start", "$0.short_id"}},
				// Step 5: Complete the blocker
				{Args: []string{"done", "$0.short_id"}},
				// Step 6: Available should now show only Blocked (Blocker is completed)
				{
					Args: []string{"available", "--player", "p1"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						items := jsonArray(t, parsed)
						if len(items) != 1 {
							t.Fatalf("expected 1 available task, got %d", len(items))
						}
						m := items[0].(map[string]any)
						assertEqual(t, m["title"], "Blocked")
					},
				},
			},
		},
		{
			Name: "available_with_filter",
			Steps: []Step{
				// Step 0: Add API task with tag
				{Args: []string{"add", "API task", "+api"}},
				// Step 1: Add UI task with tag
				{Args: []string{"add", "UI task", "+ui"}},
				// Step 2: Available filtered by +api should show only the API task
				{
					Args: []string{"available", "+api", "--player", "p1"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						items := jsonArray(t, parsed)
						if len(items) != 1 {
							t.Fatalf("expected 1 available task, got %d", len(items))
						}
						m := items[0].(map[string]any)
						assertEqual(t, m["title"], "API task")
					},
				},
			},
		},
		{
			Name: "pop_claims_highest_urgency",
			Steps: []Step{
				// Step 0: Add low priority task
				{Args: []string{"add", "Low task", "priority=1"}},
				// Step 1: Add high priority task
				{Args: []string{"add", "High task", "priority=3"}},
				// Step 2: Pop should return the highest urgency task
				{
					Args: []string{"pop", "--player", "p1"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "High task")
						assertEqual(t, m["status"], "active")
						assertEqual(t, m["claimed_by"], "p1")
					},
				},
			},
		},
		{
			Name: "pop_sequential_different_tasks",
			Steps: []Step{
				// Step 0: Add first task
				{Args: []string{"add", "First"}},
				// Step 1: Add second task
				{Args: []string{"add", "Second"}},
				// Step 2: Pop gets a task
				{Args: []string{"pop", "--player", "p1"}},
				// Step 3: Pop gets the other task
				{Args: []string{"pop", "--player", "p1"}},
				// Step 4: Pop with no tasks left should error
				{
					Args:    []string{"pop", "--player", "p1"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stderr, "No available tasks")
					},
				},
			},
		},
		{
			Name: "pop_with_filter",
			Steps: []Step{
				// Step 0: Add backend job with tag
				{Args: []string{"add", "Backend job", "+backend"}},
				// Step 1: Add frontend job with tag
				{Args: []string{"add", "Frontend job", "+frontend"}},
				// Step 2: Pop with +backend filter should return backend job
				{
					Args: []string{"pop", "+backend", "--player", "p1"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "Backend job")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
