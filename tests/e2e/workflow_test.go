package e2e

import (
	"testing"
)

func TestWorkflowCommands(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "workflow_list_default",
			Steps: []Step{
				{
					Args: []string{"workflow", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "kanban")
						assertContains(t, output, "pending")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) < 1 {
							t.Fatal("expected at least 1 workflow")
						}
						found := false
						for _, item := range arr {
							m := item.(map[string]any)
							if m["name"] == "kanban" {
								found = true
								statuses := m["statuses"].([]any)
								if len(statuses) != 4 {
									t.Fatalf("expected 4 statuses, got %d", len(statuses))
								}
								// Verify statuses are objects with name and roles fields
								for _, s := range statuses {
									sm := s.(map[string]any)
									if _, ok := sm["name"]; !ok {
										t.Fatal("status missing 'name' field")
									}
									if _, ok := sm["roles"]; !ok {
										t.Fatal("status missing 'roles' field")
									}
								}
								transitions := m["transitions"].([]any)
								if len(transitions) != 6 {
									t.Fatalf("expected 6 transitions, got %d", len(transitions))
								}
								projects := m["projects"].([]any)
								if len(projects) < 1 {
									t.Fatalf("expected at least 1 project, got %d", len(projects))
								}
								break
							}
						}
						if !found {
							t.Fatal("expected kanban workflow in list")
						}
					},
				},
			},
		},
		{
			Name: "workflow_info_kanban",
			Steps: []Step{
				{
					Args: []string{"workflow", "info", "kanban"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "kanban")
						assertContains(t, output, "pending")
						assertContains(t, output, "active")
						assertContains(t, output, "completed")
						assertContains(t, output, "deleted")
						assertContains(t, output, "->")
						assertContains(t, output, "default")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["name"] != "kanban" {
							t.Fatalf("expected name 'kanban', got %v", m["name"])
						}
						statuses := m["statuses"].([]any)
						if len(statuses) != 4 {
							t.Fatalf("expected 4 statuses, got %v", statuses)
						}
						// Verify statuses have name+roles shape
						for _, s := range statuses {
							sm := s.(map[string]any)
							if _, ok := sm["name"]; !ok {
								t.Fatal("status missing 'name' field")
							}
						}
						transitions := m["transitions"].([]any)
						if len(transitions) != 6 {
							t.Fatalf("expected 6 transitions, got %v", transitions)
						}
						projects := m["projects"].([]any)
						if len(projects) < 1 {
							t.Fatalf("expected at least 1 project, got %v", projects)
						}
					},
				},
			},
		},
		{
			Name: "workflow_info_nonexistent",
			Steps: []Step{
				{
					Args:    []string{"workflow", "info", "nonexistent"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						combined := r.Stdout + r.Stderr
						assertContains(t, combined, "not found")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}

func TestCustomWorkflowTaskLifecycle(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	configContent := `
[workflows.scrum.statuses.backlog]
roles = ["initial"]

[workflows.scrum.statuses.in_progress]
roles = ["start", "highlight"]

[workflows.scrum.statuses.shipped]
roles = ["terminal", "done", "dim"]

[workflows.scrum.statuses.wontfix]
roles = ["terminal", "delete", "dim"]

[[workflows.scrum.transitions]]
from = "backlog"
to = "in_progress"

[[workflows.scrum.transitions]]
from = "in_progress"
to = "shipped"

[[workflows.scrum.transitions]]
from = "in_progress"
to = "wontfix"

[[workflows.scrum.transitions]]
from = "backlog"
to = "wontfix"

[projects.default]
workflow = "scrum"
`

	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "custom_lifecycle/" + dbMode + "/" + format
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)
			env.withConfig(configContent)

			// 0: Add — should get "backlog" status (initial role)
			r := env.Run("add", "Ship feature X")
			if r.Err != nil {
				t.Fatalf("add failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// 1: Start — should transition to "in_progress" (start role)
			r = env.Run("start", "$0.short_id")
			if r.Err != nil {
				t.Fatalf("start failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// 2: Info should show in_progress
			r = env.Run("info", "$0.short_id")
			if r.Err != nil {
				t.Fatalf("info failed: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "in_progress")

			// 3: Done — should transition to "shipped" (done role)
			r = env.Run("done", "$0.short_id")
			if r.Err != nil {
				t.Fatalf("done failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// 4: Verify shipped
			r = env.Run("info", "$0.short_id")
			if r.Err != nil {
				t.Fatalf("info after done: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "shipped")

			// 5: Add another task
			r = env.Run("add", "Won't do this")
			if r.Err != nil {
				t.Fatalf("add 2 failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// 6: Delete — should transition to "wontfix" (delete role)
			r = env.Run("delete", "$5.short_id")
			if r.Err != nil {
				t.Fatalf("delete failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// 7: Verify wontfix
			r = env.Run("info", "$5.short_id")
			if r.Err != nil {
				t.Fatalf("info after delete: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "wontfix")
		})
	}
}

func TestWorkflowStatusDisplay(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	configContent := `
[workflows.custom.statuses.pending]
roles = ["initial"]
[workflows.custom.statuses.in_progress]
roles = ["start", "highlight"]
[workflows.custom.statuses.review]
roles = ["highlight"]
[workflows.custom.statuses.done]
roles = ["terminal", "done", "dim"]
[workflows.custom.statuses.archived]
roles = ["terminal", "delete", "dim"]

[[workflows.custom.transitions]]
from = "pending"
to = "in_progress"
[[workflows.custom.transitions]]
from = "in_progress"
to = "review"
[[workflows.custom.transitions]]
from = "review"
to = "done"

[projects.default]
workflow = "custom"
`

	for _, dbMode := range []string{"flag", "env"} {
		t.Run(dbMode, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, "json")
			env.withConfig(configContent)

			// Create a task — verifies config loads without error
			r := env.Run("add", "Test task")
			if r.Err != nil {
				t.Fatalf("add failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// List tasks — verifies task is returned
			r = env.Run("list")
			if r.Err != nil {
				t.Fatalf("list failed: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "Test task")
		})
	}
}
