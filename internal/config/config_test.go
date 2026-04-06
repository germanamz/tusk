package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Load with no config file and no env vars — should return defaults.
	// We set the config search path to a temp dir that has no config.toml.
	cfg, err := Load(WithSearchPath(t.TempDir()))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Storage defaults
	if cfg.Storage.Backend != "sqlite" {
		t.Errorf("Storage.Backend = %q, want %q", cfg.Storage.Backend, "sqlite")
	}
	if cfg.Storage.Path != "~/.local/share/tusk/tusk.db" {
		t.Errorf("Storage.Path = %q, want %q", cfg.Storage.Path, "~/.local/share/tusk/tusk.db")
	}

	// Urgency defaults
	if cfg.Urgency.PriorityWeight != 6.0 {
		t.Errorf("Urgency.PriorityWeight = %v, want 6.0", cfg.Urgency.PriorityWeight)
	}
	if cfg.Urgency.DueWeight != 12.0 {
		t.Errorf("Urgency.DueWeight = %v, want 12.0", cfg.Urgency.DueWeight)
	}
	if cfg.Urgency.AgeWeight != 2.0 {
		t.Errorf("Urgency.AgeWeight = %v, want 2.0", cfg.Urgency.AgeWeight)
	}
	if cfg.Urgency.BlockingWeight != 8.0 {
		t.Errorf("Urgency.BlockingWeight = %v, want 8.0", cfg.Urgency.BlockingWeight)
	}
	if cfg.Urgency.BlockedWeight != -5.0 {
		t.Errorf("Urgency.BlockedWeight = %v, want -5.0", cfg.Urgency.BlockedWeight)
	}

	// TUI defaults
	if cfg.TUI.DateFormat != "2006-01-02" {
		t.Errorf("TUI.DateFormat = %q, want %q", cfg.TUI.DateFormat, "2006-01-02")
	}
	if cfg.TUI.Color != true {
		t.Errorf("TUI.Color = %v, want true", cfg.TUI.Color)
	}
	if cfg.TUI.TreeIndent != 2 {
		t.Errorf("TUI.TreeIndent = %d, want 2", cfg.TUI.TreeIndent)
	}
	if cfg.TUI.DefaultSort != "urgency" {
		t.Errorf("TUI.DefaultSort = %q, want %q", cfg.TUI.DefaultSort, "urgency")
	}

	// MCP defaults (empty slices)
	if len(cfg.MCP.DisabledToolGroups) != 0 {
		t.Errorf("MCP.DisabledToolGroups = %v, want empty", cfg.MCP.DisabledToolGroups)
	}
	if len(cfg.MCP.DisabledTools) != 0 {
		t.Errorf("MCP.DisabledTools = %v, want empty", cfg.MCP.DisabledTools)
	}
	if len(cfg.MCP.DisabledResourceGroups) != 0 {
		t.Errorf("MCP.DisabledResourceGroups = %v, want empty", cfg.MCP.DisabledResourceGroups)
	}
	if len(cfg.MCP.DisabledResources) != 0 {
		t.Errorf("MCP.DisabledResources = %v, want empty", cfg.MCP.DisabledResources)
	}
}

func TestLoad_File(t *testing.T) {
	// Write a TOML config file to a temp directory.
	dir := t.TempDir()
	content := []byte(`
[storage]
backend = "postgres"
path = "/custom/path/tusk.db"

[urgency]
priority_weight = 10.0

[tui]
color = false
tree_indent = 4

[mcp]
disabled_tools = ["tusk_task_delete"]
disabled_tool_groups = ["relation"]
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), content, 0o644); err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	cfg, err := Load(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Overridden values
	if cfg.Storage.Backend != "postgres" {
		t.Errorf("Storage.Backend = %q, want %q", cfg.Storage.Backend, "postgres")
	}
	if cfg.Storage.Path != "/custom/path/tusk.db" {
		t.Errorf("Storage.Path = %q, want %q", cfg.Storage.Path, "/custom/path/tusk.db")
	}
	if cfg.Urgency.PriorityWeight != 10.0 {
		t.Errorf("Urgency.PriorityWeight = %v, want 10.0", cfg.Urgency.PriorityWeight)
	}
	if cfg.TUI.Color != false {
		t.Errorf("TUI.Color = %v, want false", cfg.TUI.Color)
	}
	if cfg.TUI.TreeIndent != 4 {
		t.Errorf("TUI.TreeIndent = %d, want 4", cfg.TUI.TreeIndent)
	}

	// MCP disabled lists
	if len(cfg.MCP.DisabledTools) != 1 || cfg.MCP.DisabledTools[0] != "tusk_task_delete" {
		t.Errorf("MCP.DisabledTools = %v, want [tusk_task_delete]", cfg.MCP.DisabledTools)
	}
	if len(cfg.MCP.DisabledToolGroups) != 1 || cfg.MCP.DisabledToolGroups[0] != "relation" {
		t.Errorf("MCP.DisabledToolGroups = %v, want [relation]", cfg.MCP.DisabledToolGroups)
	}

	// Non-overridden values keep defaults
	if cfg.Urgency.DueWeight != 12.0 {
		t.Errorf("Urgency.DueWeight = %v, want 12.0 (default)", cfg.Urgency.DueWeight)
	}
	if cfg.TUI.DateFormat != "2006-01-02" {
		t.Errorf("TUI.DateFormat = %q, want %q (default)", cfg.TUI.DateFormat, "2006-01-02")
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	// Write a config file with one value, then override it with an env var.
	dir := t.TempDir()
	content := []byte(`
[storage]
path = "/from-file/tusk.db"
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), content, 0o644); err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	// Env var should override file value.
	t.Setenv("TUSK_STORAGE_PATH", "/from-env/tusk.db")
	t.Setenv("TUSK_TUI_COLOR", "false")

	cfg, err := Load(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Storage.Path != "/from-env/tusk.db" {
		t.Errorf("Storage.Path = %q, want %q (env override)", cfg.Storage.Path, "/from-env/tusk.db")
	}
	if cfg.TUI.Color != false {
		t.Errorf("TUI.Color = %v, want false (env override)", cfg.TUI.Color)
	}
}

func TestLoad_MalformedFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`this is not valid toml [[[`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), content, 0o644); err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	_, err := Load(WithSearchPath(dir))
	if err == nil {
		t.Fatal("Load() should return error for malformed TOML")
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~/foo/bar", filepath.Join(home, "foo/bar")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~", home},
	}

	for _, tt := range tests {
		got := ExpandPath(tt.input)
		if got != tt.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLoad_WorkflowConfig(t *testing.T) {
	dir := t.TempDir()
	content := `
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]

[[workflows.kanban.transitions]]
from = "pending"
to = "active"

[[workflows.kanban.transitions]]
from = "active"
to = "completed"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	wf, ok := cfg.Workflows["kanban"]
	if !ok {
		t.Fatal("expected workflows.kanban to exist")
	}
	if len(wf.Statuses) != 4 {
		t.Errorf("expected 4 statuses, got %d", len(wf.Statuses))
	}
	if len(wf.Transitions) != 2 {
		t.Errorf("expected 2 transitions, got %d", len(wf.Transitions))
	}
	if wf.Transitions[0].From != "pending" || wf.Transitions[0].To != "active" {
		t.Errorf("unexpected first transition: %+v", wf.Transitions[0])
	}
}

func TestLoad_ProjectConfig(t *testing.T) {
	dir := t.TempDir()
	content := `
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
transitions = []

[projects.default]
workflow = "kanban"

[projects.backend]
workflow = "kanban"

[projects.backend.settings.auto_complete_parent]
trigger_status = "completed"
target_status = "completed"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(cfg.Projects))
	}

	def, ok := cfg.Projects["default"]
	if !ok {
		t.Fatal("expected projects.default to exist")
	}
	if def.Workflow != "kanban" {
		t.Errorf("expected workflow 'kanban', got %q", def.Workflow)
	}

	backend, ok := cfg.Projects["backend"]
	if !ok {
		t.Fatal("expected projects.backend to exist")
	}
	if backend.Settings.AutoCompleteParent == nil {
		t.Fatal("expected auto_complete_parent settings")
	}
	if backend.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Errorf("expected trigger_status 'completed', got %q", backend.Settings.AutoCompleteParent.TriggerStatus)
	}
}

func TestLoad_BuiltinDefaults(t *testing.T) {
	dir := t.TempDir()
	// Empty config file — no [projects] or [workflows] sections
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	wf, ok := cfg.Workflows["kanban"]
	if !ok {
		t.Fatal("expected builtin kanban workflow")
	}
	if len(wf.Statuses) != 4 {
		t.Errorf("expected 4 kanban statuses, got %d", len(wf.Statuses))
	}

	proj, ok := cfg.Projects["default"]
	if !ok {
		t.Fatal("expected builtin default project")
	}
	if proj.Workflow != "kanban" {
		t.Errorf("expected default project workflow 'kanban', got %q", proj.Workflow)
	}
}

func TestLoad_ValidationProjectReferencesUnknownWorkflow(t *testing.T) {
	dir := t.TempDir()
	content := `
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
transitions = []

[projects.backend]
workflow = "nonexistent"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(WithSearchPath(dir))
	if err == nil {
		t.Fatal("expected error for project referencing unknown workflow")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("expected error to mention 'nonexistent', got: %v", err)
	}
}

func TestLoad_AutoCreateConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// Verify file does NOT exist before Load
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("config file should not exist before Load")
	}

	cfg, err := Load(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Verify file WAS created
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("config file should exist after Load: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("config file should not be empty")
	}

	// Builtins should be present (the auto-created file defines them)
	if _, ok := cfg.Projects["default"]; !ok {
		t.Fatal("expected default project")
	}
	if _, ok := cfg.Workflows["kanban"]; !ok {
		t.Fatal("expected kanban workflow")
	}
}

func TestLoad_WorkflowStatusDisplay(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.toml")

	t.Run("valid highlight and dim statuses", func(t *testing.T) {
		err := os.WriteFile(cfgFile, []byte(`
[workflows.test]
statuses = ["todo", "doing", "done", "archived"]
transitions = [{ from = "todo", to = "doing" }, { from = "doing", to = "done" }]
highlight_statuses = ["doing"]
dim_statuses = ["done", "archived"]

[projects.default]
workflow = "test"
`), 0o644)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(WithSearchPath(dir))
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		wf := cfg.Workflows["test"]
		if len(wf.HighlightStatuses) != 1 || wf.HighlightStatuses[0] != "doing" {
			t.Errorf("HighlightStatuses = %v, want [doing]", wf.HighlightStatuses)
		}
		if len(wf.DimStatuses) != 2 {
			t.Errorf("DimStatuses = %v, want [done archived]", wf.DimStatuses)
		}
	})

	t.Run("dim status not in statuses list", func(t *testing.T) {
		err := os.WriteFile(cfgFile, []byte(`
[workflows.test]
statuses = ["todo", "doing", "done"]
transitions = [{ from = "todo", to = "doing" }]
dim_statuses = ["archived"]

[projects.default]
workflow = "test"
`), 0o644)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Load(WithSearchPath(dir))
		if err == nil {
			t.Fatal("expected validation error for unknown dim status")
		}
	})

	t.Run("status in both highlight and dim", func(t *testing.T) {
		err := os.WriteFile(cfgFile, []byte(`
[workflows.test]
statuses = ["todo", "doing", "done"]
transitions = [{ from = "todo", to = "doing" }]
highlight_statuses = ["doing"]
dim_statuses = ["doing"]

[projects.default]
workflow = "test"
`), 0o644)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Load(WithSearchPath(dir))
		if err == nil {
			t.Fatal("expected validation error for status in both lists")
		}
	})
}

func TestLoad_AutoCreateDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	custom := `[tui]
color = false
`
	if err := os.WriteFile(configPath, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.TUI.Color != false {
		t.Error("expected color=false from custom config")
	}

	// File should NOT have been overwritten
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != custom {
		t.Error("config file was overwritten")
	}
}
