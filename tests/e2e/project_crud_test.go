package e2e

import (
	"strings"
	"testing"
)

func TestProjectCreateAndList(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "create_list/" + dbMode + "/" + format
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, format)

			result := env.Run("project", "create", "backend", "workflow=kanban")
			if result.Err != nil {
				test.Fatalf("create: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}

			result = env.Run("project", "list")
			if result.Err != nil {
				test.Fatalf("list: %v\nstderr: %s", result.Err, result.Stderr)
			}
			assertContains(test, result.Stdout, "backend")
		})
	}
}

func TestProjectShowDescription(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "show_description/" + dbMode + "/" + format
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, format)

			const desc = "the project blurb"
			result := env.Run("project", "create", "backend",
				"workflow=kanban", `description="`+desc+`"`)
			if result.Err != nil {
				test.Fatalf("create: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}

			result = env.Run("project", "show", "backend")
			if result.Err != nil {
				test.Fatalf("show: %v\nstderr: %s", result.Err, result.Stderr)
			}
			if format == "json" {
				if !strings.Contains(result.Stdout, `"description"`) {
					test.Fatalf("json output missing description key: %s", result.Stdout)
				}
				if !strings.Contains(result.Stdout, desc) {
					test.Fatalf("json output missing description value %q: %s", desc, result.Stdout)
				}
			} else {
				if !strings.Contains(result.Stdout, "Description:") {
					test.Fatalf("text output missing Description label: %s", result.Stdout)
				}
				if !strings.Contains(result.Stdout, desc) {
					test.Fatalf("text output missing description body %q: %s", desc, result.Stdout)
				}
			}

			result = env.Run("project", "modify", "backend", "description=")
			if result.Err != nil {
				test.Fatalf("modify clear: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}
			result = env.Run("project", "show", "backend")
			if result.Err != nil {
				test.Fatalf("show after clear: %v\nstderr: %s", result.Err, result.Stderr)
			}
			if format == "json" {
				if !strings.Contains(result.Stdout, `"description": ""`) {
					test.Fatalf("json output should still emit empty description: %s", result.Stdout)
				}
			} else {
				if strings.Contains(result.Stdout, "Description:") {
					test.Fatalf("text output should omit Description block when empty: %s", result.Stdout)
				}
			}
		})
	}
}

func TestProjectModifyUrgencyDelta(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "modify_delta/" + dbMode + "/" + format
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, format)

			result := env.Run("project", "create", "backend",
				"workflow=kanban", "urgency.blocking-weight=5")
			if result.Err != nil {
				test.Fatalf("create: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}

			result = env.Run("project", "modify", "backend", "+urgency.blocking-weight=2")
			if result.Err != nil {
				test.Fatalf("modify: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}
		})
	}
}

func TestProjectDeleteRejectsDefault(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "delete_default/" + dbMode + "/" + format
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, format)

			result := env.Run("project", "delete", "default")
			if result.Err == nil {
				test.Fatalf("expected error deleting default, stdout: %s", result.Stdout)
			}
			combined := result.Stdout + result.Stderr
			assertContains(test, combined, "default")
		})
	}
}

func TestProjectDeleteRejectsWhenReferenced(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "delete_referenced/" + dbMode + "/" + format
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, format)

			result := env.Run("project", "create", "backend", "workflow=kanban")
			if result.Err != nil {
				test.Fatalf("create: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}

			result = env.Run("task", "create", "task in backend", "project=backend")
			if result.Err != nil {
				test.Fatalf("add task: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}

			result = env.Run("project", "delete", "backend")
			if result.Err == nil {
				test.Fatalf("expected error deleting referenced project, stdout: %s", result.Stdout)
			}
		})
	}
}

func TestProjectDeleteForceWithRefs(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "delete_force/" + dbMode + "/" + format
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, format)

			result := env.Run("project", "create", "backend", "workflow=kanban")
			if result.Err != nil {
				test.Fatalf("create: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}

			result = env.Run("task", "create", "task in backend", "project=backend")
			if result.Err != nil {
				test.Fatalf("add task: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}

			result = env.Run("project", "delete", "backend", "--force")
			if result.Err != nil {
				test.Fatalf("force delete: %v\nstderr: %s\nstdout: %s", result.Err, result.Stderr, result.Stdout)
			}
		})
	}
}
