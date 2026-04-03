package e2e

import (
	"encoding/json"
	"testing"
)

// autoCompleteSettings is the JSON to enable auto-complete on the default project.
const autoCompleteSettings = `{"auto_complete_parent":{"trigger_status":"completed","target_status":"completed"}}`

// bothPropagationSettings enables both auto-complete and auto-revert.
// Note: default workflow only allows completed -> pending (not completed -> active),
// so the revert target must be "pending".
const bothPropagationSettings = `{"auto_complete_parent":{"trigger_status":"completed","target_status":"completed"},"auto_revert_parent":{"trigger_status":"completed","target_status":"pending"}}`

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

// runPropagationScenarios runs scenarios with custom project settings.
// JSON-only since assertions use AssertJSON.
func runPropagationScenarios(t *testing.T, scenarios []Scenario, settings string) {
	t.Helper()
	combos := combinations(
		[]string{"flag", "env"},
		[]string{"json"},
	)
	for _, sc := range scenarios {
		for _, combo := range combos {
			dbMode := combo[0]
			name := sc.Name + "/" + dbMode + "/json"
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				env := newEnv(t, binPath, dbMode, "json")
				env.SetDefaultProjectSettings(settings)
				for i, step := range sc.Steps {
					r := env.Run(step.Args...)
					if step.WantErr && r.Err == nil {
						t.Fatalf("step %d: expected error, got none. stdout:\n%s", i, r.Stdout)
					}
					if !step.WantErr && r.Err != nil {
						t.Fatalf("step %d: unexpected error: %v\nstderr: %s\nstdout: %s", i, r.Err, r.Stderr, r.Stdout)
					}
					if step.AssertJSON != nil {
						var parsed any
						if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
							t.Fatalf("step %d: failed to parse JSON: %v\nraw:\n%s", i, err, r.Stdout)
						}
						step.AssertJSON(t, parsed)
					}
				}
			})
		}
	}
}

func TestPropagation_AutoComplete(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_complete_all_children_done",
			Steps: []Step{
				// Step 0: Create parent
				{Args: []string{"add", "Parent task"}},
				// Step 1: Start parent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create child 1
				{Args: []string{"add", "Child one", "parent:$0.short_id"}},
				// Step 3: Create child 2
				{Args: []string{"add", "Child two", "parent:$0.short_id"}},
				// Step 4: Start child 1
				{Args: []string{"start", "$2.short_id"}},
				// Step 5: Complete child 1
				{Args: []string{"done", "$2.short_id"}},
				// Step 6: Check parent — should still be active (child 2 not done)
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "active" {
							t.Fatalf("expected parent still 'active', got %v", m["status"])
						}
					},
				},
				// Step 7: Start child 2
				{Args: []string{"start", "$3.short_id"}},
				// Step 8: Complete child 2
				{Args: []string{"done", "$3.short_id"}},
				// Step 9: Check parent — should be auto-completed
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed' (auto-complete), got %v", m["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_complete_deleted_child_ignored",
			Steps: []Step{
				// Step 0: Create parent
				{Args: []string{"add", "Parent"}},
				// Step 1: Start parent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create child 1
				{Args: []string{"add", "Child 1", "parent:$0.short_id"}},
				// Step 3: Create child 2 (will be deleted)
				{Args: []string{"add", "Child 2", "parent:$0.short_id"}},
				// Step 4: Delete child 2
				{Args: []string{"delete", "$3.short_id"}},
				// Step 5: Start child 1
				{Args: []string{"start", "$2.short_id"}},
				// Step 6: Complete child 1
				{Args: []string{"done", "$2.short_id"}},
				// Step 7: Check parent — should be auto-completed (deleted child ignored)
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed' (deleted child ignored), got %v", m["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_complete_workflow_guard",
			Steps: []Step{
				// Step 0: Create parent (stays pending — not started)
				{Args: []string{"add", "Parent pending"}},
				// Step 1: Create child
				{Args: []string{"add", "Child", "parent:$0.short_id"}},
				// Step 2: Start child
				{Args: []string{"start", "$1.short_id"}},
				// Step 3: Complete child
				{Args: []string{"done", "$1.short_id"}},
				// Step 4: Check parent — should still be pending (pending->completed not allowed)
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "pending" {
							t.Fatalf("expected parent still 'pending' (workflow guard), got %v", m["status"])
						}
					},
				},
			},
		},
	}

	runPropagationScenarios(t, scenarios, autoCompleteSettings)
}

func TestPropagation_Recursive(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_complete_recursive",
			Steps: []Step{
				// Step 0: Create grandparent
				{Args: []string{"add", "Grandparent"}},
				// Step 1: Start grandparent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create parent
				{Args: []string{"add", "Parent", "parent:$0.short_id"}},
				// Step 3: Start parent
				{Args: []string{"start", "$2.short_id"}},
				// Step 4: Create child
				{Args: []string{"add", "Child", "parent:$2.short_id"}},
				// Step 5: Start child
				{Args: []string{"start", "$4.short_id"}},
				// Step 6: Complete child — should cascade up
				{Args: []string{"done", "$4.short_id"}},
				// Step 7: Check parent — should be auto-completed
				{
					Args: []string{"info", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed', got %v", m["status"])
						}
					},
				},
				// Step 8: Check grandparent — should be auto-completed
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected grandparent 'completed', got %v", m["status"])
						}
					},
				},
			},
		},
	}

	runPropagationScenarios(t, scenarios, autoCompleteSettings)
}

func TestPropagation_AutoRevert(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_revert_child_reopened",
			Steps: []Step{
				// Step 0: Create parent
				{Args: []string{"add", "Parent"}},
				// Step 1: Start parent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create child
				{Args: []string{"add", "Child", "parent:$0.short_id"}},
				// Step 3: Start child
				{Args: []string{"start", "$2.short_id"}},
				// Step 4: Complete child — parent auto-completes
				{Args: []string{"done", "$2.short_id"}},
				// Step 5: Verify parent is completed
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed', got %v", m["status"])
						}
					},
				},
				// Step 6: Re-open child (completed -> pending)
				{Args: []string{"modify", "$2.short_id", "status:pending"}},
				// Step 7: Check parent — should be reverted to pending
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "pending" {
							t.Fatalf("expected parent 'pending' after revert, got %v", m["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_revert_recursive",
			Steps: []Step{
				// Step 0: Create grandparent
				{Args: []string{"add", "Grandparent"}},
				// Step 1: Start grandparent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create parent
				{Args: []string{"add", "Parent", "parent:$0.short_id"}},
				// Step 3: Start parent
				{Args: []string{"start", "$2.short_id"}},
				// Step 4: Create child
				{Args: []string{"add", "Child", "parent:$2.short_id"}},
				// Step 5: Start child
				{Args: []string{"start", "$4.short_id"}},
				// Step 6: Complete child — cascades up
				{Args: []string{"done", "$4.short_id"}},
				// Step 7: Verify grandparent is completed
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected grandparent 'completed', got %v", m["status"])
						}
					},
				},
				// Step 8: Re-open child — cascading revert
				{Args: []string{"modify", "$4.short_id", "status:pending"}},
				// Step 9: Check parent — should be reverted to pending
				{
					Args: []string{"info", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "pending" {
							t.Fatalf("expected parent 'pending' after revert, got %v", m["status"])
						}
					},
				},
				// Step 10: Check grandparent — should be reverted to pending
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "pending" {
							t.Fatalf("expected grandparent 'pending' after revert, got %v", m["status"])
						}
					},
				},
			},
		},
	}

	runPropagationScenarios(t, scenarios, bothPropagationSettings)
}
