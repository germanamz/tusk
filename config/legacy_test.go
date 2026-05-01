package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_RejectsLegacyProjectSections(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	content := `
[storage]
path = "/tmp/x.db"

[projects.foo]
workflow = "kanban"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		test.Fatal(err)
	}

	cfg, err := Load(WithExplicitFile(path))
	if err == nil {
		test.Fatal("expected error for legacy [projects.*] section")
	}
	msg := err.Error()
	if !strings.Contains(msg, "projects") {
		test.Errorf("error should mention projects, got: %v", err)
	}
	if !strings.Contains(msg, "tusk project") {
		test.Errorf("error should point at `tusk project`, got: %v", err)
	}
	if cfg != nil {
		test.Errorf("expected nil config on error, got %+v", cfg)
	}
}

func TestLoad_RejectsLegacyWorkflowSections(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	content := `
[storage]
path = "/tmp/x.db"

[workflows.custom.statuses.pending]
roles = ["initial"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		test.Fatal(err)
	}

	cfg, err := Load(WithExplicitFile(path))
	if err == nil {
		test.Fatal("expected error for legacy [workflows.*] section")
	}
	msg := err.Error()
	if !strings.Contains(msg, "workflows") {
		test.Errorf("error should mention workflows, got: %v", err)
	}
	if !strings.Contains(msg, "tusk workflow") {
		test.Errorf("error should point at `tusk workflow`, got: %v", err)
	}
	if cfg != nil {
		test.Errorf("expected nil config on error, got %+v", cfg)
	}
}

func TestLoad_AcceptsTrimmedConfig(test *testing.T) {
	dir := test.TempDir()
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
		test.Fatal(err)
	}

	cfg, err := Load(WithSearchPath(dir))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	if cfg == nil {
		test.Fatal("expected non-nil config")
	}
	if cfg.Storage.Path != "/tmp/x.db" {
		test.Errorf("Storage.Path = %q, want /tmp/x.db", cfg.Storage.Path)
	}
	if cfg.Urgency.PriorityWeight != 7.0 {
		test.Errorf("Urgency.PriorityWeight = %v, want 7.0", cfg.Urgency.PriorityWeight)
	}
}

func TestLoad_AcceptsEmbeddedDefaults(test *testing.T) {
	// Isolated global dir with no pre-existing config.toml — Load should
	// auto-create from embedded defaults without tripping the legacy guard.
	cfg, err := Load(WithSearchPath(test.TempDir()))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	if cfg == nil {
		test.Fatal("expected non-nil config")
	}
	if cfg.Storage.Backend != "sqlite" {
		test.Errorf("Storage.Backend = %q, want sqlite", cfg.Storage.Backend)
	}
}

func TestLoad_RejectsLegacyInWalkUpHit(test *testing.T) {
	projectDir := test.TempDir()
	local := filepath.Join(projectDir, "tusk.toml")
	content := `
[projects.bar]
workflow = "kanban"
`
	if err := os.WriteFile(local, []byte(content), 0o644); err != nil {
		test.Fatal(err)
	}

	cfg, err := Load(WithStartDir(projectDir), WithSearchPath(test.TempDir()))
	if err == nil {
		test.Fatal("expected error for legacy [projects.*] in walk-up hit")
	}
	if !strings.Contains(err.Error(), "projects") {
		test.Errorf("error should mention projects, got: %v", err)
	}
	if cfg != nil {
		test.Errorf("expected nil config on error, got %+v", cfg)
	}
}
