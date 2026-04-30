package e2e

import (
	"testing"
)

func TestTaskQueue(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "available_basic_filtering",
			Steps: []Step{
				// Step 0: Add task A (priority 3)
				{Args: []string{"task", "create", "Task A", "priority=3"}},
				// Step 1: Add task B (priority 1)
				{Args: []string{"task", "create", "Task B", "priority=1"}},
				// Step 2: Add task C (priority 2)
				{Args: []string{"task", "create", "Task C", "priority=2"}},
				// Step 3: Claim task A by p1
				{Args: []string{"task", "claim", "$0.short_id", "--player", "p1"}},
				// Step 4: List available for p2 — should see only B and C (unclaimed)
				{
					Args: []string{"task", "available", "--player", "p2"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						items := jsonArray(test, parsed)
						if len(items) != 2 {
							test.Fatalf("expected 2 available tasks, got %d", len(items))
						}
					},
				},
			},
		},
		{
			Name: "available_blocked_excluded",
			Steps: []Step{
				// Step 0: Add blocker task
				{Args: []string{"task", "create", "Blocker"}},
				// Step 1: Add blocked task
				{Args: []string{"task", "create", "Blocked"}},
				// Step 2: Link blocker blocks blocked
				{Args: []string{"task", "link", "$0.short_id", "blocks", "$1.short_id"}},
				// Step 3: Available should only show the blocker (blocked is excluded)
				{
					Args: []string{"task", "available", "--player", "p1"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						items := jsonArray(test, parsed)
						if len(items) != 1 {
							test.Fatalf("expected 1 available task, got %d", len(items))
						}
						mapped := items[0].(map[string]any)
						assertEqual(test, mapped["title"], "Blocker")
					},
				},
				// Step 4: Start the blocker
				{Args: []string{"task", "start", "$0.short_id"}},
				// Step 5: Complete the blocker
				{Args: []string{"task", "done", "$0.short_id"}},
				// Step 6: Available should now show only Blocked (Blocker is completed)
				{
					Args: []string{"task", "available", "--player", "p1"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						items := jsonArray(test, parsed)
						if len(items) != 1 {
							test.Fatalf("expected 1 available task, got %d", len(items))
						}
						mapped := items[0].(map[string]any)
						assertEqual(test, mapped["title"], "Blocked")
					},
				},
			},
		},
		{
			Name: "available_with_filter",
			Steps: []Step{
				// Step 0: Add API task with tag
				{Args: []string{"task", "create", "API task", "+api"}},
				// Step 1: Add UI task with tag
				{Args: []string{"task", "create", "UI task", "+ui"}},
				// Step 2: Available filtered by +api should show only the API task
				{
					Args: []string{"task", "available", "+api", "--player", "p1"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						items := jsonArray(test, parsed)
						if len(items) != 1 {
							test.Fatalf("expected 1 available task, got %d", len(items))
						}
						mapped := items[0].(map[string]any)
						assertEqual(test, mapped["title"], "API task")
					},
				},
			},
		},
		{
			Name: "pop_claims_highest_urgency",
			Steps: []Step{
				// Step 0: Add low priority task
				{Args: []string{"task", "create", "Low task", "priority=1"}},
				// Step 1: Add high priority task
				{Args: []string{"task", "create", "High task", "priority=3"}},
				// Step 2: Pop should return the highest urgency task
				{
					Args: []string{"task", "pop", "--player", "p1"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						assertEqual(test, mapped["title"], "High task")
						assertEqual(test, mapped["status"], "active")
						assertEqual(test, mapped["claimed_by"], "p1")
					},
				},
			},
		},
		{
			Name: "pop_sequential_different_tasks",
			Steps: []Step{
				// Step 0: Add first task
				{Args: []string{"task", "create", "First"}},
				// Step 1: Add second task
				{Args: []string{"task", "create", "Second"}},
				// Step 2: Pop gets a task
				{Args: []string{"task", "pop", "--player", "p1"}},
				// Step 3: Pop gets the other task
				{Args: []string{"task", "pop", "--player", "p1"}},
				// Step 4: Pop with no tasks left should error
				{
					Args:    []string{"task", "pop", "--player", "p1"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertContains(test, result.Stderr, "No available tasks")
					},
				},
			},
		},
		{
			Name: "pop_with_filter",
			Steps: []Step{
				// Step 0: Add backend job with tag
				{Args: []string{"task", "create", "Backend job", "+backend"}},
				// Step 1: Add frontend job with tag
				{Args: []string{"task", "create", "Frontend job", "+frontend"}},
				// Step 2: Pop with +backend filter should return backend job
				{
					Args: []string{"task", "pop", "+backend", "--player", "p1"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						assertEqual(test, mapped["title"], "Backend job")
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}
