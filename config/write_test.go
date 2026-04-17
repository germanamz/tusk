package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFilePath_ExplicitFile(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(explicit, []byte("# custom\n"), 0o644); err != nil {
		t.Fatalf("writing explicit file: %v", err)
	}

	got, err := ConfigFilePath(WithExplicitFile(explicit))
	if err != nil {
		t.Fatalf("ConfigFilePath() error: %v", err)
	}
	if got != explicit {
		t.Fatalf("got %q, want %q", got, explicit)
	}
}

func TestConfigFilePath_ExplicitFileMissingErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.toml")
	_, err := ConfigFilePath(WithExplicitFile(missing))
	if err == nil {
		t.Fatal("want error for missing explicit file")
	}
}

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
		{"mcp.blocked_fields.tusk_project_modify", true},
		{"mcp.blocked_fields.tusk_task_modify", true},
		{"mcp.blocked_fields", false},
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
		{"mcp.blocked_fields.tusk_project_modify", true},
		{"mcp.blocked_fields", false},
		// Invalid keys
		{"nonexistent", false},
		{"storage.nonexistent", false},
		{"tui.nonexistent", false},
		{"", false},
		{"storage", false}, // not a leaf
		// Projects and workflows are DB-managed; they must NOT be valid
		// TOML keys anymore.
		{"workflows.kanban.statuses", false},
		{"projects.default.workflow", false},
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

func TestConfigFilePath_WalkUpHitReturnsLocal(t *testing.T) {
	startDir := t.TempDir()
	local := filepath.Join(startDir, "tusk.toml")
	if err := os.WriteFile(local, []byte("# local\n"), 0o644); err != nil {
		t.Fatalf("writing local: %v", err)
	}
	globalDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(globalDir, "config.toml"), []byte("# global\n"), 0o644); err != nil {
		t.Fatalf("writing global: %v", err)
	}

	got, err := ConfigFilePath(WithStartDir(startDir), WithSearchPath(globalDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != local {
		t.Fatalf("got %q, want %q", got, local)
	}
}

func TestConfigFilePath_WalkUpMissFallsThroughToGlobal(t *testing.T) {
	startDir := t.TempDir()
	globalDir := t.TempDir()
	wantPath := filepath.Join(globalDir, "config.toml")
	if err := os.WriteFile(wantPath, []byte("# global\n"), 0o644); err != nil {
		t.Fatalf("writing global: %v", err)
	}

	got, err := ConfigFilePath(WithStartDir(startDir), WithSearchPath(globalDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != wantPath {
		t.Fatalf("got %q, want %q", got, wantPath)
	}
}

func TestConfigFilePath_WalkUpMissNoGlobalFileStillReturnsPath(t *testing.T) {
	startDir := t.TempDir()
	globalDir := t.TempDir()

	got, err := ConfigFilePath(WithStartDir(startDir), WithSearchPath(globalDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(globalDir, "config.toml")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
