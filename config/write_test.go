package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFilePath_WithSearchPath(t *testing.T) {
	dir := t.TempDir()
	got, err := ConfigFilePath(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, "config.toml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigFilePath_WithEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TUSK_CONFIG_DIR", dir)
	got, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, "config.toml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigFilePath_Default(t *testing.T) {
	// Clear env to force default path.
	t.Setenv("TUSK_CONFIG_DIR", "")
	got, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "tusk", "config.toml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	original := &Config{
		Storage: StorageConfig{
			Backend: "sqlite",
			Path:    "/tmp/test.db",
		},
		Urgency: UrgencyConfig{
			PriorityWeight: 6.0,
			DueWeight:      12.0,
		},
		TUI: TUIConfig{
			DateFormat:  "2006-01-02",
			Color:       true,
			TreeIndent:  2,
			DefaultSort: "urgency",
		},
		Workflows: map[string]WorkflowConfig{
			"kanban": {
				Statuses: []string{"pending", "active", "completed"},
				Transitions: []WorkflowTransitionConfig{
					{From: "pending", To: "active"},
					{From: "active", To: "completed"},
				},
				HighlightStatuses: []string{"active"},
				DimStatuses:       []string{"completed"},
			},
		},
		Projects: map[string]ProjectConfig{
			"default": {
				Workflow: "kanban",
			},
		},
	}

	if err := WriteConfig(original, path); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	// Verify key fields round-trip correctly.
	if loaded.Storage.Backend != "sqlite" {
		t.Errorf("storage.backend: got %q, want %q", loaded.Storage.Backend, "sqlite")
	}
	if loaded.Storage.Path != "/tmp/test.db" {
		t.Errorf("storage.path: got %q, want %q", loaded.Storage.Path, "/tmp/test.db")
	}
	if loaded.Urgency.PriorityWeight != 6.0 {
		t.Errorf("urgency.priority_weight: got %v, want %v", loaded.Urgency.PriorityWeight, 6.0)
	}
	if loaded.TUI.Color != true {
		t.Errorf("tui.color: got %v, want true", loaded.TUI.Color)
	}
	if len(loaded.Workflows) != 1 {
		t.Fatalf("workflows: got %d, want 1", len(loaded.Workflows))
	}
	wf := loaded.Workflows["kanban"]
	if len(wf.Statuses) != 3 {
		t.Errorf("kanban statuses: got %d, want 3", len(wf.Statuses))
	}
	if len(wf.Transitions) != 2 {
		t.Errorf("kanban transitions: got %d, want 2", len(wf.Transitions))
	}
	if loaded.Projects["default"].Workflow != "kanban" {
		t.Errorf("default project workflow: got %q, want %q", loaded.Projects["default"].Workflow, "kanban")
	}
}

func TestLoadFile_NotFound(t *testing.T) {
	_, err := LoadFile("/nonexistent/path/config.toml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestLoadFile_MalformedTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte("[invalid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for malformed TOML")
	}
}

func TestWriteConfig_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{
		Storage: StorageConfig{Backend: "sqlite", Path: "/tmp/a.db"},
	}

	// Write initial config.
	if err := WriteConfig(cfg, path); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Overwrite with new value.
	cfg.Storage.Path = "/tmp/b.db"
	if err := WriteConfig(cfg, path); err != nil {
		t.Fatalf("second write: %v", err)
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Storage.Path != "/tmp/b.db" {
		t.Errorf("got %q, want %q", loaded.Storage.Path, "/tmp/b.db")
	}
}

func TestIsSliceKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"mcp.disabled_tools", true},
		{"mcp.disabled_tool_groups", true},
		{"mcp.disabled_resources", true},
		{"workflows.kanban.statuses", true},
		{"workflows.kanban.highlight_statuses", true},
		{"workflows.kanban.dim_statuses", true},
		{"tui.color", false},
		{"storage.path", false},
		{"urgency.due_weight", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := IsSliceKey(tt.key)
			if got != tt.want {
				t.Errorf("IsSliceKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestIsValidKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		// Scalar fields
		{"storage.backend", true},
		{"storage.path", true},
		{"tui.color", true},
		{"tui.date_format", true},
		{"urgency.due_weight", true},
		{"mcp.disabled_tools", true},
		// Map-keyed paths
		{"workflows.kanban.statuses", true},
		{"workflows.kanban.transitions", true},
		{"workflows.kanban.highlight_statuses", true},
		{"workflows.myworkflow.statuses", true},
		{"projects.default.workflow", true},
		{"projects.backend.settings.urgency.blocking_weight", true},
		{"projects.myproj.settings.auto_complete_parent.trigger_status", true},
		// Invalid keys
		{"nonexistent", false},
		{"storage.nonexistent", false},
		{"workflows.kanban.nonexistent", false},
		{"tui.nonexistent", false},
		{"", false},
		{"storage", false}, // not a leaf
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := IsValidKey(tt.key)
			if got != tt.want {
				t.Errorf("IsValidKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}
