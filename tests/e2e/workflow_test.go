package e2e

import (
	"testing"
)

func TestWorkflowCommands(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "workflow_list_default",
			Steps: []Step{
				{
					Args: []string{"workflow", "list"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "kanban")
						assertContains(test, output, "pending")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) < 1 {
							test.Fatal("expected at least 1 workflow")
						}
						found := false
						for _, item := range arr {
							mapped := item.(map[string]any)
							if mapped["name"] == "kanban" {
								found = true
								statuses := mapped["statuses"].([]any)
								if len(statuses) != 4 {
									test.Fatalf("expected 4 statuses, got %d", len(statuses))
								}
								// Verify statuses are objects with name and roles fields
								for _, status := range statuses {
									sm := status.(map[string]any)
									if _, ok := sm["name"]; !ok {
										test.Fatal("status missing 'name' field")
									}
									if _, ok := sm["roles"]; !ok {
										test.Fatal("status missing 'roles' field")
									}
								}
								transitions := mapped["transitions"].([]any)
								if len(transitions) != 6 {
									test.Fatalf("expected 6 transitions, got %d", len(transitions))
								}
								projects := mapped["projects"].([]any)
								if len(projects) < 1 {
									test.Fatalf("expected at least 1 project, got %d", len(projects))
								}
								break
							}
						}
						if !found {
							test.Fatal("expected kanban workflow in list")
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
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "kanban")
						assertContains(test, output, "pending")
						assertContains(test, output, "active")
						assertContains(test, output, "completed")
						assertContains(test, output, "deleted")
						assertContains(test, output, "->")
						assertContains(test, output, "default")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						if mapped["name"] != "kanban" {
							test.Fatalf("expected name 'kanban', got %v", mapped["name"])
						}
						statuses := mapped["statuses"].([]any)
						if len(statuses) != 4 {
							test.Fatalf("expected 4 statuses, got %v", statuses)
						}
						// Verify statuses have name+roles shape
						for _, status := range statuses {
							sm := status.(map[string]any)
							if _, ok := sm["name"]; !ok {
								test.Fatal("status missing 'name' field")
							}
						}
						transitions := mapped["transitions"].([]any)
						if len(transitions) != 6 {
							test.Fatalf("expected 6 transitions, got %v", transitions)
						}
						projects := mapped["projects"].([]any)
						if len(projects) < 1 {
							test.Fatalf("expected at least 1 project, got %v", projects)
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
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						combined := result.Stdout + result.Stderr
						assertContains(test, combined, "not found")
					},
				},
			},
		},
	}

	runScenarios(test, binPath, scenarios)
}

func TestCustomWorkflowTaskLifecycle(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "custom_lifecycle/" + dbMode + "/" + format
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, format)

			// Create the scrum workflow via the CLI — post-phase-2, the
			// TOML schema no longer accepts [workflows.*], so custom
			// workflows must be seeded through the service layer.
			result := env.Run("workflow", "create", "scrum",
				"status=backlog(initial)",
				"status=in_progress(start,highlight)",
				"status=shipped(terminal,done,dim)",
				"status=wontfix(terminal,delete,dim)",
				"transition=backlog:in_progress,in_progress:shipped,in_progress:wontfix,backlog:wontfix",
			)
			if result.Err != nil {
				test.Fatalf("workflow create scrum: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// Rebind the default project to the scrum workflow.
			result = env.Run("project", "modify", "default", "workflow=scrum")
			if result.Err != nil {
				test.Fatalf("project modify default: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// 2: Add — should get "backlog" status (initial role)
			result = env.Run("task", "create", "Ship feature X")
			if result.Err != nil {
				test.Fatalf("add failed: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// 3: Start — should transition to "in_progress" (start role)
			result = env.Run("task", "start", "$2.short_id")
			if result.Err != nil {
				test.Fatalf("start failed: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// 4: Info should show in_progress
			result = env.Run("task", "get", "$2.short_id")
			if result.Err != nil {
				test.Fatalf("info failed: %v\nstderr: %s", result.Err, result.Stderr)
			}
			assertContains(test, result.Stdout, "in_progress")

			// 5: Done — should transition to "shipped" (done role)
			result = env.Run("task", "done", "$2.short_id")
			if result.Err != nil {
				test.Fatalf("done failed: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// 6: Verify shipped
			result = env.Run("task", "get", "$2.short_id")
			if result.Err != nil {
				test.Fatalf("info after done: %v\nstderr: %s", result.Err, result.Stderr)
			}
			assertContains(test, result.Stdout, "shipped")

			// 7: Add another task
			result = env.Run("task", "create", "Won't do this")
			if result.Err != nil {
				test.Fatalf("add 2 failed: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// 8: Delete — should transition to "wontfix" (delete role)
			result = env.Run("task", "delete", "$7.short_id")
			if result.Err != nil {
				test.Fatalf("delete failed: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// 9: Verify wontfix
			result = env.Run("task", "get", "$7.short_id")
			if result.Err != nil {
				test.Fatalf("info after delete: %v\nstderr: %s", result.Err, result.Stderr)
			}
			assertContains(test, result.Stdout, "wontfix")
		})
	}
}

func TestWorkflowStatusDisplay(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	for _, dbMode := range []string{"flag", "env"} {
		test.Run(dbMode, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, "json")

			// Seed a custom workflow through the CLI and rebind default.
			result := env.Run("workflow", "create", "custom",
				"status=pending(initial)",
				"status=in_progress(start,highlight)",
				"status=review(highlight)",
				"status=done(terminal,done,dim)",
				"status=archived(terminal,delete,dim)",
				"transition=pending:in_progress,in_progress:review,review:done",
			)
			if result.Err != nil {
				test.Fatalf("workflow create custom: %v\nstderr: %s", result.Err, result.Stderr)
			}
			result = env.Run("project", "modify", "default", "workflow=custom")
			if result.Err != nil {
				test.Fatalf("project modify default: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// Create a task — verifies config loads without error
			result = env.Run("task", "create", "Test task")
			if result.Err != nil {
				test.Fatalf("add failed: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// List tasks — verifies task is returned
			result = env.Run("task", "list")
			if result.Err != nil {
				test.Fatalf("list failed: %v\nstderr: %s", result.Err, result.Stderr)
			}
			assertContains(test, result.Stdout, "Test task")
		})
	}
}

func TestWorkflowCRUD(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "crud/" + dbMode + "/" + format
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, format)

			result := env.Run("workflow", "create", "sprint",
				"status=todo(initial)",
				"status=doing(start,highlight)",
				"status=done(terminal,done,dim)",
				"status=removed(terminal,delete,dim)",
				"transition=todo:doing,doing:done,doing:removed",
			)
			if result.Err != nil {
				test.Fatalf("create: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}
			assertContains(test, result.Stdout, "sprint")

			result = env.Run("workflow", "list")
			if result.Err != nil {
				test.Fatalf("list: %v\nstderr: %s", result.Err, result.Stderr)
			}
			assertContains(test, result.Stdout, "sprint")
			assertContains(test, result.Stdout, "kanban")

			result = env.Run("workflow", "modify", "sprint",
				"+status=review",
				"+transition=doing:review,review:done",
			)
			if result.Err != nil {
				test.Fatalf("modify: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}

			result = env.Run("workflow", "info", "sprint")
			if result.Err != nil {
				test.Fatalf("info: %v\nstderr: %s", result.Err, result.Stderr)
			}
			assertContains(test, result.Stdout, "review")

			result = env.Run("workflow", "delete", "sprint")
			if result.Err != nil {
				test.Fatalf("delete: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}

			result = env.Run("workflow", "list")
			if result.Err != nil {
				test.Fatalf("list after delete: %v\nstderr: %s", result.Err, result.Stderr)
			}
			assertNotContains(test, result.Stdout, "sprint")
		})
	}
}

func TestWorkflowDeleteInUse(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	for _, dbMode := range []string{"flag", "env"} {
		test.Run(dbMode, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, "text")

			result := env.Run("workflow", "delete", "kanban")
			if result.Err == nil {
				test.Fatal("expected error deleting in-use workflow")
			}
			combined := result.Stdout + result.Stderr
			assertContains(test, combined, "referenced")
		})
	}
}

func TestWorkflowCreateDuplicate(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	for _, dbMode := range []string{"flag", "env"} {
		test.Run(dbMode, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, "text")

			result := env.Run("workflow", "create", "kanban",
				"status=a(initial)", "status=b(start,highlight)",
				"status=c(terminal,done,dim)", "status=d(terminal,delete,dim)",
				"transition=a:b,b:c,b:d",
			)
			if result.Err == nil {
				test.Fatal("expected error creating duplicate workflow")
			}
			combined := result.Stdout + result.Stderr
			assertContains(test, combined, "already exists")
		})
	}
}
