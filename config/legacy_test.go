package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_RejectsLegacyProjectSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	content := `
[storage]
path = "/tmp/x.db"

[projects.foo]
workflow = "kanban"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(WithExplicitFile(path))
	if err == nil {
		t.Fatal("expected error for legacy [projects.*] section")
	}
	msg := err.Error()
	if !strings.Contains(msg, "projects") {
		t.Errorf("error should mention projects, got: %v", err)
	}
	if !strings.Contains(msg, "tusk project") {
		t.Errorf("error should point at `tusk project`, got: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil config on error, got %+v", cfg)
	}
}

func TestLoad_RejectsLegacyWorkflowSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	content := `
[storage]
path = "/tmp/x.db"

[workflows.custom.statuses.pending]
roles = ["initial"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(WithExplicitFile(path))
	if err == nil {
		t.Fatal("expected error for legacy [workflows.*] section")
	}
	msg := err.Error()
	if !strings.Contains(msg, "workflows") {
		t.Errorf("error should mention workflows, got: %v", err)
	}
	if !strings.Contains(msg, "tusk workflow") {
		t.Errorf("error should point at `tusk workflow`, got: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil config on error, got %+v", cfg)
	}
}

func TestLoad_AcceptsTrimmedConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[storage]
backend = "sqlite"
path = "/tmp/x.db"

[urgency]
priority_weight = 7.0

[tui]
color = false

[mcp]
disabled_tools = ["tusk_task_delete"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Storage.Path != "/tmp/x.db" {
		t.Errorf("Storage.Path = %q, want /tmp/x.db", cfg.Storage.Path)
	}
	if cfg.Urgency.PriorityWeight != 7.0 {
		t.Errorf("Urgency.PriorityWeight = %v, want 7.0", cfg.Urgency.PriorityWeight)
	}
}

func TestLoad_AcceptsEmbeddedDefaults(t *testing.T) {
	// Isolated global dir with no pre-existing config.toml — Load should
	// auto-create from embedded defaults without tripping the legacy guard.
	cfg, err := Load(WithSearchPath(t.TempDir()))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Storage.Backend != "sqlite" {
		t.Errorf("Storage.Backend = %q, want sqlite", cfg.Storage.Backend)
	}
}

func TestLoad_RejectsLegacyInWalkUpHit(t *testing.T) {
	projectDir := t.TempDir()
	local := filepath.Join(projectDir, "tusk.toml")
	content := `
[projects.bar]
workflow = "kanban"
`
	if err := os.WriteFile(local, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(WithStartDir(projectDir), WithSearchPath(t.TempDir()))
	if err == nil {
		t.Fatal("expected error for legacy [projects.*] in walk-up hit")
	}
	if !strings.Contains(err.Error(), "projects") {
		t.Errorf("error should mention projects, got: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil config on error, got %+v", cfg)
	}
}
