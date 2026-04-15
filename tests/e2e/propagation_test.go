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

func TestPropagation_Disabled(t *testing.T) {
	// Propagation is disabled by default — completing all children should NOT
	// auto-complete the parent.
	scenarios := []Scenario{
		{
			Name: "propagation_disabled_by_default",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent task"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"task", "create", "Child task", "parent=$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
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

// runPropagationScenarios runs scenarios after executing the given setup
// commands on a fresh env. Setup commands are not indexed into env.results,
// so scenario step refs ($0, $1, ...) still match the pre-phase-2 layout.
// JSON-only since assertions use AssertJSON.
func runPropagationScenarios(t *testing.T, scenarios []Scenario, setup [][]string) {
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
				for _, cmd := range setup {
					r := env.Run(cmd...)
					if r.Err != nil {
						t.Fatalf("setup %v: %v\nstderr: %s", cmd, r.Err, r.Stderr)
					}
				}
				// Discard setup results so scenario $N refs index into the
				// actual scenario steps, not the setup preamble.
				env.results = nil

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
				{Args: []string{"task", "create", "Parent task"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"task", "create", "Child one", "parent=$0.short_id"}},
				{Args: []string{"task", "create", "Child two", "parent=$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
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
					Args: []string{"task", "get", "$0.short_id"},
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
				{Args: []string{"task", "create", "Parent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"task", "create", "Child 1", "parent=$0.short_id"}},
				{Args: []string{"task", "create", "Child 2", "parent=$0.short_id"}},
				{Args: []string{"delete", "$3.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
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
				{Args: []string{"task", "create", "Parent pending"}},
				{Args: []string{"task", "create", "Child", "parent=$0.short_id"}},
				{Args: []string{"start", "$1.short_id"}},
				{Args: []string{"done", "$1.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
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

	runPropagationScenarios(t, scenarios, autoCompleteSetup)
}

func TestPropagation_Recursive(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_complete_recursive",
			Steps: []Step{
				{Args: []string{"task", "create", "Grandparent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"task", "create", "Parent", "parent=$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"task", "create", "Child", "parent=$2.short_id"}},
				{Args: []string{"start", "$4.short_id"}},
				{Args: []string{"done", "$4.short_id"}},
				{
					Args: []string{"task", "get", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed', got %v", m["status"])
						}
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
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

	runPropagationScenarios(t, scenarios, autoCompleteSetup)
}

func TestPropagation_AutoRevert(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_revert_child_reopened",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"task", "create", "Child", "parent=$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed', got %v", m["status"])
						}
					},
				},
				{Args: []string{"task", "modify", "$2.short_id", "status=pending"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
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
				{Args: []string{"task", "create", "Grandparent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"task", "create", "Parent", "parent=$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"task", "create", "Child", "parent=$2.short_id"}},
				{Args: []string{"start", "$4.short_id"}},
				{Args: []string{"done", "$4.short_id"}},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected grandparent 'completed', got %v", m["status"])
						}
					},
				},
				{Args: []string{"task", "modify", "$4.short_id", "status=pending"}},
				{
					Args: []string{"task", "get", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "pending" {
							t.Fatalf("expected parent 'pending' after revert, got %v", m["status"])
						}
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
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

	runPropagationScenarios(t, scenarios, bothPropagationSetup)
}
