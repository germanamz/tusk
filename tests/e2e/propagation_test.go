package e2e

import (
	"encoding/json"
	"testing"
)

// autoCompleteConfig is a config.toml that enables auto-complete on the default project.
const autoCompleteConfig = `
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
transitions = [
  { from = "pending",   to = "active" },
  { from = "pending",   to = "deleted" },
  { from = "active",    to = "completed" },
  { from = "active",    to = "pending" },
  { from = "active",    to = "deleted" },
  { from = "completed", to = "pending" },
]

[projects.default]
workflow = "kanban"

[projects.default.settings.auto_complete_parent]
trigger_status = "completed"
target_status = "completed"
`

// bothPropagationConfig enables both auto-complete and auto-revert on the default project.
const bothPropagationConfig = `
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
transitions = [
  { from = "pending",   to = "active" },
  { from = "pending",   to = "deleted" },
  { from = "active",    to = "completed" },
  { from = "active",    to = "pending" },
  { from = "active",    to = "deleted" },
  { from = "completed", to = "pending" },
]

[projects.default]
workflow = "kanban"

[projects.default.settings.auto_complete_parent]
trigger_status = "completed"
target_status = "completed"

[projects.default.settings.auto_revert_parent]
trigger_status = "completed"
target_status = "pending"
`

func TestPropagation_Disabled(t *testing.T) {
	// Propagation is disabled by default — completing all children should NOT
	// auto-complete the parent.
	scenarios := []Scenario{
		{
			Name: "propagation_disabled_by_default",
			Steps: []Step{
				{Args: []string{"add", "Parent task"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Child task", "parent:$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
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

// runPropagationScenarios runs scenarios with a custom config file.
// JSON-only since assertions use AssertJSON.
func runPropagationScenarios(t *testing.T, scenarios []Scenario, configContent string) {
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
				env.withConfig(configContent)

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
				{Args: []string{"add", "Parent task"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Child one", "parent:$0.short_id"}},
				{Args: []string{"add", "Child two", "parent:$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
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
				{Args: []string{"start", "$3.short_id"}},
				{Args: []string{"done", "$3.short_id"}},
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
				{Args: []string{"add", "Parent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Child 1", "parent:$0.short_id"}},
				{Args: []string{"add", "Child 2", "parent:$0.short_id"}},
				{Args: []string{"delete", "$3.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
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
			},
		},
		{
			Name: "auto_complete_workflow_guard",
			Steps: []Step{
				{Args: []string{"add", "Parent pending"}},
				{Args: []string{"add", "Child", "parent:$0.short_id"}},
				{Args: []string{"start", "$1.short_id"}},
				{Args: []string{"done", "$1.short_id"}},
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

	runPropagationScenarios(t, scenarios, autoCompleteConfig)
}

func TestPropagation_Recursive(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_complete_recursive",
			Steps: []Step{
				{Args: []string{"add", "Grandparent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Parent", "parent:$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"add", "Child", "parent:$2.short_id"}},
				{Args: []string{"start", "$4.short_id"}},
				{Args: []string{"done", "$4.short_id"}},
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

	runPropagationScenarios(t, scenarios, autoCompleteConfig)
}

func TestPropagation_AutoRevert(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_revert_child_reopened",
			Steps: []Step{
				{Args: []string{"add", "Parent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Child", "parent:$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
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
				{Args: []string{"modify", "$2.short_id", "status:pending"}},
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
				{Args: []string{"add", "Grandparent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Parent", "parent:$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"add", "Child", "parent:$2.short_id"}},
				{Args: []string{"start", "$4.short_id"}},
				{Args: []string{"done", "$4.short_id"}},
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
				{Args: []string{"modify", "$4.short_id", "status:pending"}},
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

	runPropagationScenarios(t, scenarios, bothPropagationConfig)
}
