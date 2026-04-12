package config

import (
	"strings"
	"testing"
)

func TestCreateProject(t *testing.T) {
	path := writeTestConfig(t, baseConfig)

	proj := ProjectConfig{Workflow: "kanban", DBPath: "/tmp/b.db"}
	if err := CreateProject(path, "backend", proj); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, ok := cfg.Projects["backend"]
	if !ok {
		t.Fatal("expected backend project in config")
	}
	if got.Workflow != "kanban" || got.DBPath != "/tmp/b.db" {
		t.Fatalf("unexpected project: %+v", got)
	}
	if _, ok := cfg.Projects["default"]; !ok {
		t.Fatal("expected default project preserved")
	}
}

func TestCreateProject_AlreadyExists(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	err := CreateProject(path, "default", ProjectConfig{Workflow: "kanban"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
}

func TestCreateProject_UnknownWorkflow(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	err := CreateProject(path, "backend", ProjectConfig{Workflow: "ghost"})
	if err == nil || !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestDeleteProject_NotFound(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	err := DeleteProject(path, "ghost", func(string) (int, error) { return 0, nil }, false)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestDeleteProject_RejectsDefault(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	err := DeleteProject(path, DefaultProjectID, func(string) (int, error) { return 0, nil }, false)
	if err == nil || !strings.Contains(err.Error(), "default") {
		t.Fatalf("expected default-guard error, got %v", err)
	}
}

func TestDeleteProject_ForceRemovesDefault(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	if err := DeleteProject(path, DefaultProjectID, func(string) (int, error) { return 0, nil }, true); err != nil {
		t.Fatalf("DeleteProject force: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := cfg.Projects[DefaultProjectID]; ok {
		t.Fatal("expected default removed")
	}
}

func TestDeleteProject_RejectsReferenced(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	if err := CreateProject(path, "backend", ProjectConfig{Workflow: "kanban"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := DeleteProject(path, "backend",
		func(name string) (int, error) { return 3, nil }, false)
	if err == nil || !strings.Contains(err.Error(), "3") {
		t.Fatalf("expected refs error with count, got %v", err)
	}
}

func TestDeleteProject_Force(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	if err := CreateProject(path, "backend", ProjectConfig{Workflow: "kanban"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := DeleteProject(path, "backend",
		func(string) (int, error) { return 3, nil }, true); err != nil {
		t.Fatalf("force delete: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := cfg.Projects["backend"]; ok {
		t.Fatal("expected backend removed")
	}
}
