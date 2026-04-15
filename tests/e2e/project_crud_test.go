package e2e

import "testing"

func TestProjectCreateAndList(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "create_list/" + dbMode + "/" + format
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)

			r := env.Run("project", "create", "backend", "workflow=kanban")
			if r.Err != nil {
				t.Fatalf("create: %v\nstderr: %s\nstdout: %s", r.Err, r.Stderr, r.Stdout)
			}

			r = env.Run("project", "list")
			if r.Err != nil {
				t.Fatalf("list: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "backend")
		})
	}
}

func TestProjectModifyUrgencyDelta(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "modify_delta/" + dbMode + "/" + format
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)

			r := env.Run("project", "create", "backend",
				"workflow=kanban", "urgency.blocking-weight=5")
			if r.Err != nil {
				t.Fatalf("create: %v\nstderr: %s\nstdout: %s", r.Err, r.Stderr, r.Stdout)
			}

			r = env.Run("project", "modify", "backend", "+urgency.blocking-weight=2")
			if r.Err != nil {
				t.Fatalf("modify: %v\nstderr: %s\nstdout: %s", r.Err, r.Stderr, r.Stdout)
			}
		})
	}
}

func TestProjectDeleteRejectsDefault(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "delete_default/" + dbMode + "/" + format
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)

			r := env.Run("project", "delete", "default")
			if r.Err == nil {
				t.Fatalf("expected error deleting default, stdout: %s", r.Stdout)
			}
			combined := r.Stdout + r.Stderr
			assertContains(t, combined, "default")
		})
	}
}

func TestProjectDeleteRejectsWhenReferenced(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "delete_referenced/" + dbMode + "/" + format
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)

			r := env.Run("project", "create", "backend", "workflow=kanban")
			if r.Err != nil {
				t.Fatalf("create: %v\nstderr: %s\nstdout: %s", r.Err, r.Stderr, r.Stdout)
			}

			r = env.Run("task", "create", "task in backend", "project=backend")
			if r.Err != nil {
				t.Fatalf("add task: %v\nstderr: %s\nstdout: %s", r.Err, r.Stderr, r.Stdout)
			}

			r = env.Run("project", "delete", "backend")
			if r.Err == nil {
				t.Fatalf("expected error deleting referenced project, stdout: %s", r.Stdout)
			}
		})
	}
}

func TestProjectDeleteForceWithRefs(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "delete_force/" + dbMode + "/" + format
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)

			r := env.Run("project", "create", "backend", "workflow=kanban")
			if r.Err != nil {
				t.Fatalf("create: %v\nstderr: %s\nstdout: %s", r.Err, r.Stderr, r.Stdout)
			}

			r = env.Run("task", "create", "task in backend", "project=backend")
			if r.Err != nil {
				t.Fatalf("add task: %v\nstderr: %s\nstdout: %s", r.Err, r.Stderr, r.Stdout)
			}

			r = env.Run("project", "delete", "backend", "--force")
			if r.Err != nil {
				t.Fatalf("force delete: %v\nstderr: %s\nstdout: %s", r.Err, r.Stderr, r.Stdout)
			}
		})
	}
}
