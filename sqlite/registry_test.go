package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/migrations"
)

func TestStoreRegistry_DefaultFallback(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "default.db")
	projects := map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
	}

	reg, err := NewStoreRegistry(defaultPath, dir, projects, migrations.FS)
	if err != nil {
		t.Fatalf("NewStoreRegistry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })

	store, err := reg.Get("default")
	if err != nil {
		t.Fatalf("Get default: %v", err)
	}
	if store == nil {
		t.Fatal("nil store")
	}
	store2, _ := reg.Get("default")
	if store != store2 {
		t.Fatal("expected cached store")
	}
}

func TestStoreRegistry_PerProjectAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "default.db")
	backendPath := filepath.Join(dir, "backend.db")
	projects := map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {Workflow: "kanban", DBPath: backendPath},
	}
	reg, err := NewStoreRegistry(defaultPath, dir, projects, migrations.FS)
	if err != nil {
		t.Fatalf("NewStoreRegistry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })

	def, _ := reg.Get("default")
	back, _ := reg.Get("backend")
	if def == back {
		t.Fatal("default and backend should be distinct stores")
	}
	if _, err := os.Stat(backendPath); err != nil {
		t.Fatalf("backend db file not created: %v", err)
	}
}

func TestStoreRegistry_RelativePathResolvedAgainstBase(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "default.db")
	projects := map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {Workflow: "kanban", DBPath: "backend.db"},
	}
	reg, err := NewStoreRegistry(defaultPath, dir, projects, migrations.FS)
	if err != nil {
		t.Fatalf("NewStoreRegistry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })
	if _, err := reg.Get("backend"); err != nil {
		t.Fatalf("Get backend: %v", err)
	}
	expected := filepath.Join(dir, "backend.db")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected db at %s: %v", expected, err)
	}
}

func TestStoreRegistry_UnknownProject(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewStoreRegistry(filepath.Join(dir, "d.db"), dir,
		map[string]config.ProjectConfig{"default": {Workflow: "kanban"}}, migrations.FS)
	t.Cleanup(func() { reg.Close() })
	if _, err := reg.Get("ghost"); err == nil {
		t.Fatal("expected error for unknown project")
	}
}

func TestStoreRegistry_ProjectIDs(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewStoreRegistry(filepath.Join(dir, "d.db"), dir,
		map[string]config.ProjectConfig{"default": {Workflow: "kanban"}, "backend": {Workflow: "kanban"}}, migrations.FS)
	t.Cleanup(func() { reg.Close() })
	ids := reg.ProjectIDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 project ids, got %v", ids)
	}
}

func TestStoreRegistry_Close(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewStoreRegistry(filepath.Join(dir, "d.db"), dir,
		map[string]config.ProjectConfig{"default": {Workflow: "kanban"}}, migrations.FS)
	if _, err := reg.Get("default"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := reg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
