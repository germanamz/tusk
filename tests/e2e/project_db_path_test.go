package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// perProjectDBConfig seeds the default project and kanban workflow.
// backend and frontend projects are created at test time via the CLI so
// their `db_path` values can point at per-scenario temp directories.
const perProjectDBConfig = `
[workflows.kanban.statuses.pending]
roles = ["initial"]
[workflows.kanban.statuses.active]
roles = ["start", "highlight"]
[workflows.kanban.statuses.completed]
roles = ["terminal", "done", "dim"]
[workflows.kanban.statuses.deleted]
roles = ["terminal", "delete", "dim"]
[[workflows.kanban.transitions]]
from = "pending"
to = "active"
[[workflows.kanban.transitions]]
from = "active"
to = "completed"
[[workflows.kanban.transitions]]
from = "active"
to = "deleted"

[projects.default]
workflow = "kanban"
`

func TestPerProjectDatabase_MergedListAcrossStores(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "merged/" + dbMode + "/" + format
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)
			env.withConfig(perProjectDBConfig)

			storeDir := t.TempDir()
			backendPath := filepath.Join(storeDir, "backend.db")
			frontendPath := filepath.Join(storeDir, "frontend.db")

			if r := env.Run("project", "create", "backend", "workflow=kanban", "db-path="+backendPath); r.Err != nil {
				t.Fatalf("create backend: %v\nstderr: %s", r.Err, r.Stderr)
			}
			if r := env.Run("project", "create", "frontend", "workflow=kanban", "db-path="+frontendPath); r.Err != nil {
				t.Fatalf("create frontend: %v\nstderr: %s", r.Err, r.Stderr)
			}
			if r := env.Run("add", "backend task", "project=backend", "priority=4"); r.Err != nil {
				t.Fatalf("add backend task: %v\nstderr: %s", r.Err, r.Stderr)
			}
			if r := env.Run("add", "frontend task", "project=frontend", "priority=2"); r.Err != nil {
				t.Fatalf("add frontend task: %v\nstderr: %s", r.Err, r.Stderr)
			}

			r := env.Run("list")
			if r.Err != nil {
				t.Fatalf("list: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "backend task")
			assertContains(t, r.Stdout, "frontend task")
		})
	}
}

func TestPerProjectDatabase_CrossStoreRelationRejected(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "reject_relation/" + dbMode + "/" + format
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)
			env.withConfig(perProjectDBConfig)

			storeDir := t.TempDir()
			backendPath := filepath.Join(storeDir, "backend.db")
			frontendPath := filepath.Join(storeDir, "frontend.db")

			if r := env.Run("project", "create", "backend", "workflow=kanban", "db-path="+backendPath); r.Err != nil {
				t.Fatalf("create backend: %v\nstderr: %s", r.Err, r.Stderr)
			}
			if r := env.Run("project", "create", "frontend", "workflow=kanban", "db-path="+frontendPath); r.Err != nil {
				t.Fatalf("create frontend: %v\nstderr: %s", r.Err, r.Stderr)
			}

			srcResult := env.Run("add", "backend task", "project=backend")
			if srcResult.Err != nil {
				t.Fatalf("add backend: %v\nstderr: %s", srcResult.Err, srcResult.Stderr)
			}
			dstResult := env.Run("add", "frontend task", "project=frontend")
			if dstResult.Err != nil {
				t.Fatalf("add frontend: %v\nstderr: %s", dstResult.Err, dstResult.Stderr)
			}

			r := env.Run("link", "$2.short_id", "blocks", "$3.short_id")
			if r.Err == nil {
				t.Fatalf("expected cross-store link to fail, stdout:\n%s", r.Stdout)
			}
			combined := strings.ToLower(r.Stdout + r.Stderr)
			if !strings.Contains(combined, "cross-store") && !strings.Contains(combined, "cross store") {
				t.Fatalf("expected cross-store error, got:\nstdout: %s\nstderr: %s", r.Stdout, r.Stderr)
			}
		})
	}
}

func TestPerProjectDatabase_CrossStoreProjectMoveRejected(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "reject_move/" + dbMode + "/" + format
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, format)
			env.withConfig(perProjectDBConfig)

			storeDir := t.TempDir()
			backendPath := filepath.Join(storeDir, "backend.db")

			if r := env.Run("project", "create", "backend", "workflow=kanban", "db-path="+backendPath); r.Err != nil {
				t.Fatalf("create backend: %v\nstderr: %s", r.Err, r.Stderr)
			}

			if r := env.Run("add", "task", "project=default"); r.Err != nil {
				t.Fatalf("add: %v\nstderr: %s", r.Err, r.Stderr)
			}

			r := env.Run("modify", "$1.short_id", "project=backend")
			if r.Err == nil {
				t.Fatalf("expected cross-store move to fail, stdout:\n%s", r.Stdout)
			}
			combined := strings.ToLower(r.Stdout + r.Stderr)
			if !strings.Contains(combined, "cross-store") && !strings.Contains(combined, "cross store") {
				t.Fatalf("expected cross-store error, got:\nstdout: %s\nstderr: %s", r.Stdout, r.Stderr)
			}
		})
	}
}
