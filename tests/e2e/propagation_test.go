package e2e

import (
	"encoding/json"
	"testing"
)

// autoCompleteSetup applies auto-complete on the default project via the
// service layer (post-phase-2, workflows and projects are DB-only — TOML
// no longer carries these settings).
var autoCompleteSetup = [][]string{
	{"project", "modify", "default",
		"auto-complete.trigger=completed",
		"auto-complete.target=completed",
	},
}

// bothPropagationSetup enables both auto-complete and auto-revert on the default project.
var bothPropagationSetup = [][]string{
	{"project", "modify", "default",
		"auto-complete.trigger=completed",
		"auto-complete.target=completed",
		"auto-revert.trigger=completed",
		"auto-revert.target=pending",
	},
}

func TestPropagation_Disabled(test *testing.T) {
	// Propagation is disabled by default — completing all children should NOT
	// auto-complete the parent.
	scenarios := []Scenario{
		{
			Name: "propagation_disabled_by_default",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent task"}},
				{Args: []string{"task", "start", "$0.short_id"}},
				{Args: []string{"task", "create", "Child task", "parent=$0.short_id"}},
				{Args: []string{"task", "start", "$2.short_id"}},
				{Args: []string{"task", "done", "$2.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertContains(test, result.Stdout, "active")
						assertNotContains(test, result.Stdout, "completed")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "active" {
							test.Fatalf("expected parent status 'active', got %v", mapped["status"])
						}
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}

// runPropagationScenarios runs scenarios after executing the given setup
// commands on a fresh env. Setup commands are not indexed into env.results,
// so scenario step refs ($0, $1, ...) still match the pre-phase-2 layout.
// JSON-only since assertions use AssertJSON.
func runPropagationScenarios(test *testing.T, scenarios []Scenario, setup [][]string) {
	test.Helper()
	combos := combinations(
		[]string{"flag", "env"},
		[]string{"json"},
	)
	for _, scenario := range scenarios {
		for _, combo := range combos {
			dbMode := combo[0]
			name := scenario.Name + "/" + dbMode + "/json"
			test.Run(name, func(test *testing.T) {
				test.Parallel()
				env := newEnv(test, binPath, dbMode, "json")
				for _, cmd := range setup {
					result := env.Run(cmd...)
					if result.Err != nil {
						test.Fatalf("setup %v: %v\nstderr: %s", cmd, result.Err, result.Stderr)
					}
				}
				// Discard setup results so scenario $N refs index into the
				// actual scenario steps, not the setup preamble.
				env.results = nil

				for index, step := range scenario.Steps {
					result := env.Run(step.Args...)
					if step.WantErr && result.Err == nil {
						test.Fatalf("step %d: expected error, got none. stdout:\n%s", index, result.Stdout)
					}
					if !step.WantErr && result.Err != nil {
						test.Fatalf("step %d: unexpected error: %v\nstderr: %s\nstdout: %s", index, result.Err, result.Stderr, result.Stdout)
					}
					if step.AssertJSON != nil {
						var parsed any
						if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
							test.Fatalf("step %d: failed to parse JSON: %v\nraw:\n%s", index, err, result.Stdout)
						}
						step.AssertJSON(test, parsed)
					}
				}
			})
		}
	}
}

func TestPropagation_AutoComplete(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_complete_all_children_done",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent task"}},
				{Args: []string{"task", "start", "$0.short_id"}},
				{Args: []string{"task", "create", "Child one", "parent=$0.short_id"}},
				{Args: []string{"task", "create", "Child two", "parent=$0.short_id"}},
				{Args: []string{"task", "start", "$2.short_id"}},
				{Args: []string{"task", "done", "$2.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "active" {
							test.Fatalf("expected parent still 'active', got %v", mapped["status"])
						}
					},
				},
				{Args: []string{"task", "start", "$3.short_id"}},
				{Args: []string{"task", "done", "$3.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "completed" {
							test.Fatalf("expected parent 'completed' (auto-complete), got %v", mapped["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_complete_deleted_child_ignored",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},
				{Args: []string{"task", "start", "$0.short_id"}},
				{Args: []string{"task", "create", "Child 1", "parent=$0.short_id"}},
				{Args: []string{"task", "create", "Child 2", "parent=$0.short_id"}},
				{Args: []string{"task", "delete", "$3.short_id"}},
				{Args: []string{"task", "start", "$2.short_id"}},
				{Args: []string{"task", "done", "$2.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "completed" {
							test.Fatalf("expected parent 'completed', got %v", mapped["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_complete_workflow_guard",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent pending"}},
				{Args: []string{"task", "create", "Child", "parent=$0.short_id"}},
				{Args: []string{"task", "start", "$1.short_id"}},
				{Args: []string{"task", "done", "$1.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "pending" {
							test.Fatalf("expected parent still 'pending' (workflow guard), got %v", mapped["status"])
						}
					},
				},
			},
		},
	}

	runPropagationScenarios(test, scenarios, autoCompleteSetup)
}

func TestPropagation_Recursive(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_complete_recursive",
			Steps: []Step{
				{Args: []string{"task", "create", "Grandparent"}},
				{Args: []string{"task", "start", "$0.short_id"}},
				{Args: []string{"task", "create", "Parent", "parent=$0.short_id"}},
				{Args: []string{"task", "start", "$2.short_id"}},
				{Args: []string{"task", "create", "Child", "parent=$2.short_id"}},
				{Args: []string{"task", "start", "$4.short_id"}},
				{Args: []string{"task", "done", "$4.short_id"}},
				{
					Args: []string{"task", "get", "$2.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "completed" {
							test.Fatalf("expected parent 'completed', got %v", mapped["status"])
						}
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "completed" {
							test.Fatalf("expected grandparent 'completed', got %v", mapped["status"])
						}
					},
				},
			},
		},
	}

	runPropagationScenarios(test, scenarios, autoCompleteSetup)
}

func TestPropagation_AutoRevert(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_revert_child_reopened",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},
				{Args: []string{"task", "start", "$0.short_id"}},
				{Args: []string{"task", "create", "Child", "parent=$0.short_id"}},
				{Args: []string{"task", "start", "$2.short_id"}},
				{Args: []string{"task", "done", "$2.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "completed" {
							test.Fatalf("expected parent 'completed', got %v", mapped["status"])
						}
					},
				},
				{Args: []string{"task", "modify", "$2.short_id", "status=pending"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "pending" {
							test.Fatalf("expected parent 'pending' after revert, got %v", mapped["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_revert_recursive",
			Steps: []Step{
				{Args: []string{"task", "create", "Grandparent"}},
				{Args: []string{"task", "start", "$0.short_id"}},
				{Args: []string{"task", "create", "Parent", "parent=$0.short_id"}},
				{Args: []string{"task", "start", "$2.short_id"}},
				{Args: []string{"task", "create", "Child", "parent=$2.short_id"}},
				{Args: []string{"task", "start", "$4.short_id"}},
				{Args: []string{"task", "done", "$4.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "completed" {
							test.Fatalf("expected grandparent 'completed', got %v", mapped["status"])
						}
					},
				},
				{Args: []string{"task", "modify", "$4.short_id", "status=pending"}},
				{
					Args: []string{"task", "get", "$2.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "pending" {
							test.Fatalf("expected parent 'pending' after revert, got %v", mapped["status"])
						}
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["status"] != "pending" {
							test.Fatalf("expected grandparent 'pending' after revert, got %v", mapped["status"])
						}
					},
				},
			},
		},
	}

	runPropagationScenarios(test, scenarios, bothPropagationSetup)
}
