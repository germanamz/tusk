package config

import (
	"os"
	"path/filepath"
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
