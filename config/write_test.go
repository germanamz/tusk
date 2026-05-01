package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFilePath_ExplicitFile(test *testing.T) {
	dir := test.TempDir()
	explicit := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(explicit, []byte("# custom\n"), 0o644); err != nil {
		test.Fatalf("writing explicit file: %v", err)
	}

	got, err := ConfigFilePath(WithExplicitFile(explicit))

	if err != nil {
		test.Fatalf("ConfigFilePath() error: %v", err)
	}

	if got != explicit {
		test.Fatalf("got %q, want %q", got, explicit)
	}
}

func TestConfigFilePath_ExplicitFileMissingErrors(test *testing.T) {
	missing := filepath.Join(test.TempDir(), "nope.toml")
	_, err := ConfigFilePath(WithExplicitFile(missing))

	if err == nil {
		test.Fatal("want error for missing explicit file")
	}
}

func TestConfigFilePath_WithSearchPath(test *testing.T) {
	dir := test.TempDir()
	got, err := ConfigFilePath(WithSearchPath(dir))

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join(dir, "config.toml")
	if got != want {
		test.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigFilePath_WithEnvVar(test *testing.T) {
	dir := test.TempDir()
	test.Setenv("TUSK_CONFIG_DIR", dir)
	got, err := ConfigFilePath()

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join(dir, "config.toml")
	if got != want {
		test.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigFilePath_Default(test *testing.T) {
	// Clear env to force default path.
	test.Setenv("TUSK_CONFIG_DIR", "")
	got, err := ConfigFilePath()

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "tusk", "config.toml")
	if got != want {
		test.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteConfig_RoundTrip(test *testing.T) {
	dir := test.TempDir()
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
		test.Fatalf("WriteConfig: %v", err)
	}

	loaded, err := LoadFile(path)

	if err != nil {
		test.Fatalf("LoadFile: %v", err)
	}

	// Verify key fields round-trip correctly.
	if loaded.Storage.Backend != "sqlite" {
		test.Errorf("storage.backend: got %q, want %q", loaded.Storage.Backend, "sqlite")
	}
	if loaded.Storage.Path != "/tmp/test.db" {
		test.Errorf("storage.path: got %q, want %q", loaded.Storage.Path, "/tmp/test.db")
	}
	if loaded.Urgency.PriorityWeight != 6.0 {
		test.Errorf("urgency.priority_weight: got %v, want %v", loaded.Urgency.PriorityWeight, 6.0)
	}
	if loaded.TUI.Color != true {
		test.Errorf("tui.color: got %v, want true", loaded.TUI.Color)
	}
}

func TestLoadFile_NotFound(test *testing.T) {
	_, err := LoadFile("/nonexistent/path/config.toml")
	if err == nil {
		test.Fatal("expected error for nonexistent file")
	}
}

func TestLoadFile_MalformedTOML(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte("[invalid\n"), 0o644); err != nil {
		test.Fatal(err)
	}
	_, err := LoadFile(path)
	if err == nil {
		test.Fatal("expected error for malformed TOML")
	}
}

func TestWriteConfig_AtomicWrite(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{
		Storage: StorageConfig{Backend: "sqlite", Path: "/tmp/a.db"},
	}

	// Write initial config.
	if err := WriteConfig(cfg, path); err != nil {
		test.Fatalf("first write: %v", err)
	}

	// Overwrite with new value.
	cfg.Storage.Path = "/tmp/b.db"
	if err := WriteConfig(cfg, path); err != nil {
		test.Fatalf("second write: %v", err)
	}

	loaded, err := LoadFile(path)

	if err != nil {
		test.Fatalf("LoadFile: %v", err)
	}

	if loaded.Storage.Path != "/tmp/b.db" {
		test.Errorf("got %q, want %q", loaded.Storage.Path, "/tmp/b.db")
	}
}

func TestIsSliceKey(test *testing.T) {
	cases := []struct {
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

	for _, testCase := range cases {
		test.Run(testCase.key, func(test *testing.T) {
			got := IsSliceKey(testCase.key)
			if got != testCase.want {
				test.Errorf("IsSliceKey(%q) = %v, want %v", testCase.key, got, testCase.want)
			}
		})
	}
}

func TestIsValidKey(test *testing.T) {
	cases := []struct {
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

	for _, testCase := range cases {
		test.Run(testCase.key, func(test *testing.T) {
			got := IsValidKey(testCase.key)
			if got != testCase.want {
				test.Errorf("IsValidKey(%q) = %v, want %v", testCase.key, got, testCase.want)
			}
		})
	}
}

func TestConfigFilePath_WalkUpHitReturnsLocal(test *testing.T) {
	startDir := test.TempDir()
	local := filepath.Join(startDir, "tusk.toml")
	if err := os.WriteFile(local, []byte("# local\n"), 0o644); err != nil {
		test.Fatalf("writing local: %v", err)
	}
	globalDir := test.TempDir()
	if err := os.WriteFile(filepath.Join(globalDir, "config.toml"), []byte("# global\n"), 0o644); err != nil {
		test.Fatalf("writing global: %v", err)
	}

	got, err := ConfigFilePath(WithStartDir(startDir), WithSearchPath(globalDir))

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != local {
		test.Fatalf("got %q, want %q", got, local)
	}
}

func TestConfigFilePath_WalkUpMissFallsThroughToGlobal(test *testing.T) {
	startDir := test.TempDir()
	globalDir := test.TempDir()
	wantPath := filepath.Join(globalDir, "config.toml")
	if err := os.WriteFile(wantPath, []byte("# global\n"), 0o644); err != nil {
		test.Fatalf("writing global: %v", err)
	}

	got, err := ConfigFilePath(WithStartDir(startDir), WithSearchPath(globalDir))

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != wantPath {
		test.Fatalf("got %q, want %q", got, wantPath)
	}
}

func TestConfigFilePath_WalkUpMissNoGlobalFileStillReturnsPath(test *testing.T) {
	startDir := test.TempDir()
	globalDir := test.TempDir()

	got, err := ConfigFilePath(WithStartDir(startDir), WithSearchPath(globalDir))

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join(globalDir, "config.toml")
	if got != want {
		test.Fatalf("got %q, want %q", got, want)
	}
}
