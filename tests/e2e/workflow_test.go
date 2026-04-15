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

	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "custom_lifecycle/" + dbMode + "/" + format
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)

			// Create the scrum workflow via the CLI — post-phase-2, the
			// TOML schema no longer accepts [workflows.*], so custom
			// workflows must be seeded through the service layer.
			r := env.Run("workflow", "create", "scrum",
				"status=backlog(initial)",
				"status=in_progress(start,highlight)",
				"status=shipped(terminal,done,dim)",
				"status=wontfix(terminal,delete,dim)",
				"transition=backlog:in_progress,in_progress:shipped,in_progress:wontfix,backlog:wontfix",
			)
			if r.Err != nil {
				t.Fatalf("workflow create scrum: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// Rebind the default project to the scrum workflow.
			r = env.Run("project", "modify", "default", "workflow=scrum")
			if r.Err != nil {
				t.Fatalf("project modify default: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// 2: Add — should get "backlog" status (initial role)
			r = env.Run("task", "create", "Ship feature X")
			if r.Err != nil {
				t.Fatalf("add failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// 3: Start — should transition to "in_progress" (start role)
			r = env.Run("start", "$2.short_id")
			if r.Err != nil {
				t.Fatalf("start failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// 4: Info should show in_progress
			r = env.Run("task", "get", "$2.short_id")
			if r.Err != nil {
				t.Fatalf("info failed: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "in_progress")

			// 5: Done — should transition to "shipped" (done role)
			r = env.Run("done", "$2.short_id")
			if r.Err != nil {
				t.Fatalf("done failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// 6: Verify shipped
			r = env.Run("task", "get", "$2.short_id")
			if r.Err != nil {
				t.Fatalf("info after done: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "shipped")

			// 7: Add another task
			r = env.Run("task", "create", "Won't do this")
			if r.Err != nil {
				t.Fatalf("add 2 failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// 8: Delete — should transition to "wontfix" (delete role)
			r = env.Run("delete", "$7.short_id")
			if r.Err != nil {
				t.Fatalf("delete failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// 9: Verify wontfix
			r = env.Run("task", "get", "$7.short_id")
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

	for _, dbMode := range []string{"flag", "env"} {
		t.Run(dbMode, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, "json")

			// Seed a custom workflow through the CLI and rebind default.
			r := env.Run("workflow", "create", "custom",
				"status=pending(initial)",
				"status=in_progress(start,highlight)",
				"status=review(highlight)",
				"status=done(terminal,done,dim)",
				"status=archived(terminal,delete,dim)",
				"transition=pending:in_progress,in_progress:review,review:done",
			)
			if r.Err != nil {
				t.Fatalf("workflow create custom: %v\nstderr: %s", r.Err, r.Stderr)
			}
			r = env.Run("project", "modify", "default", "workflow=custom")
			if r.Err != nil {
				t.Fatalf("project modify default: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// Create a task — verifies config loads without error
			r = env.Run("task", "create", "Test task")
			if r.Err != nil {
				t.Fatalf("add failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// List tasks — verifies task is returned
			r = env.Run("task", "list")
			if r.Err != nil {
				t.Fatalf("list failed: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "Test task")
		})
	}
}

func TestWorkflowCRUD(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "crud/" + dbMode + "/" + format
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)

			r := env.Run("workflow", "create", "sprint",
				"status=todo(initial)",
				"status=doing(start,highlight)",
				"status=done(terminal,done,dim)",
				"status=removed(terminal,delete,dim)",
				"transition=todo:doing,doing:done,doing:removed",
			)
			if r.Err != nil {
				t.Fatalf("create: %v\nstderr: %s\nstdout: %s", r.Err, r.Stderr, r.Stdout)
			}
			assertContains(t, r.Stdout, "sprint")

			r = env.Run("workflow", "list")
			if r.Err != nil {
				t.Fatalf("list: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "sprint")
			assertContains(t, r.Stdout, "kanban")

			r = env.Run("workflow", "modify", "sprint",
				"+status=review",
				"+transition=doing:review,review:done",
			)
			if r.Err != nil {
				t.Fatalf("modify: %v\nstderr: %s\nstdout: %s", r.Err, r.Stderr, r.Stdout)
			}

			r = env.Run("workflow", "info", "sprint")
			if r.Err != nil {
				t.Fatalf("info: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "review")

			r = env.Run("workflow", "delete", "sprint")
			if r.Err != nil {
				t.Fatalf("delete: %v\nstderr: %s\nstdout: %s", r.Err, r.Stderr, r.Stdout)
			}

			r = env.Run("workflow", "list")
			if r.Err != nil {
				t.Fatalf("list after delete: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertNotContains(t, r.Stdout, "sprint")
		})
	}
}

func TestWorkflowDeleteInUse(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	for _, dbMode := range []string{"flag", "env"} {
		t.Run(dbMode, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, "text")

			r := env.Run("workflow", "delete", "kanban")
			if r.Err == nil {
				t.Fatal("expected error deleting in-use workflow")
			}
			combined := r.Stdout + r.Stderr
			assertContains(t, combined, "referenced")
		})
	}
}

func TestWorkflowCreateDuplicate(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	for _, dbMode := range []string{"flag", "env"} {
		t.Run(dbMode, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, "text")

			r := env.Run("workflow", "create", "kanban",
				"status=a(initial)", "status=b(start,highlight)",
				"status=c(terminal,done,dim)", "status=d(terminal,delete,dim)",
				"transition=a:b,b:c,b:d",
			)
			if r.Err == nil {
				t.Fatal("expected error creating duplicate workflow")
			}
			combined := r.Stdout + r.Stderr
			assertContains(t, combined, "already exists")
		})
	}
}
