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

	// MCP defaults
	if len(cfg.MCP.DisabledToolGroups) != 0 {
		t.Errorf("MCP.DisabledToolGroups = %v, want empty", cfg.MCP.DisabledToolGroups)
	}
	wantDisabledTools := []string{
		"tusk_config_set",
		"tusk_workflow_create",
		"tusk_workflow_modify",
		"tusk_workflow_delete",
		"tusk_project_create",
		"tusk_project_modify",
		"tusk_project_delete",
	}
	if len(cfg.MCP.DisabledTools) != len(wantDisabledTools) {
		t.Fatalf("MCP.DisabledTools = %v, want %v", cfg.MCP.DisabledTools, wantDisabledTools)
	}
	for i, want := range wantDisabledTools {
		if cfg.MCP.DisabledTools[i] != want {
			t.Errorf("MCP.DisabledTools[%d] = %q, want %q", i, cfg.MCP.DisabledTools[i], want)
		}
	}
	if len(cfg.MCP.DisabledResourceGroups) != 0 {
		t.Errorf("MCP.DisabledResourceGroups = %v, want empty", cfg.MCP.DisabledResourceGroups)
	}
	if len(cfg.MCP.DisabledResources) != 0 {
		t.Errorf("MCP.DisabledResources = %v, want empty", cfg.MCP.DisabledResources)
	}

	// Blocked fields defaults
	if got, want := cfg.MCP.BlockedFields["tusk_project_modify"], []string{"workflow"}; !equalStrings(got, want) {
		t.Errorf("MCP.BlockedFields[tusk_project_modify] = %v, want %v", got, want)
	}
	if got, want := cfg.MCP.BlockedFields["tusk_project_delete"], []string{"force"}; !equalStrings(got, want) {
		t.Errorf("MCP.BlockedFields[tusk_project_delete] = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
disabled_tool_groups = ["task_relations"]
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
	if len(cfg.MCP.DisabledToolGroups) != 1 || cfg.MCP.DisabledToolGroups[0] != "task_relations" {
		t.Errorf("MCP.DisabledToolGroups = %v, want [task_relations]", cfg.MCP.DisabledToolGroups)
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

func TestLoad_LegacyWorkflowSectionIsHardError(t *testing.T) {
	dir := t.TempDir()
	content := `
[workflows.kanban.statuses.pending]
roles = ["initial"]
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(WithSearchPath(dir))
	if err == nil {
		t.Fatal("expected error for legacy [workflows.*] section")
	}
	if !strings.Contains(err.Error(), "workflows") {
		t.Errorf("error should mention workflows, got: %v", err)
	}
}

func TestLoad_LegacyProjectSectionIsHardError(t *testing.T) {
	dir := t.TempDir()
	content := `
[projects.default]
workflow = "kanban"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(WithSearchPath(dir))
	if err == nil {
		t.Fatal("expected error for legacy [projects.*] section")
	}
	if !strings.Contains(err.Error(), "projects") {
		t.Errorf("error should mention projects, got: %v", err)
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

	// Auto-created file should round-trip through Load without errors and
	// yield the embedded defaults — projects and workflows live in the DB
	// now, so there is nothing left to assert on the Config struct itself
	// beyond the global sections.
	if cfg.Storage.Backend != "sqlite" {
		t.Errorf("Storage.Backend = %q, want sqlite", cfg.Storage.Backend)
	}
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

func TestLoad_SourcesFileForGlobal(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	want := filepath.Join(dir, "config.toml")
	if cfg.Sources.File != want {
		t.Fatalf("Sources.File = %q, want %q", cfg.Sources.File, want)
	}
}

func TestLoad_SourcesFileForExplicit(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(explicit, []byte("[storage]\npath=\"/tmp/x.db\"\n"), 0o644); err != nil {
		t.Fatalf("writing explicit file: %v", err)
	}

	cfg, err := Load(WithExplicitFile(explicit))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Sources.File != explicit {
		t.Fatalf("Sources.File = %q, want %q", cfg.Sources.File, explicit)
	}
	if cfg.Storage.Path != "/tmp/x.db" {
		t.Fatalf("Storage.Path = %q, want value from explicit file", cfg.Storage.Path)
	}
}

func TestLoad_ExplicitFileMissingIsHardError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.toml")
	_, err := Load(WithExplicitFile(missing))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "config file not found") {
		t.Fatalf("error %q should mention \"config file not found\"", err.Error())
	}
}

func TestLoad_WalkUpUsesLocalFile(t *testing.T) {
	startDir := t.TempDir()
	local := filepath.Join(startDir, "tusk.toml")
	if err := os.WriteFile(local, []byte("[tui]\ncolor = false\n"), 0o644); err != nil {
		t.Fatalf("writing local config: %v", err)
	}
	emptyGlobal := t.TempDir()

	cfg, err := Load(WithStartDir(startDir), WithSearchPath(emptyGlobal))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Sources.File != local {
		t.Fatalf("Sources.File = %q, want %q", cfg.Sources.File, local)
	}
	if cfg.TUI.Color != false {
		t.Fatalf("TUI.Color = %v, want false", cfg.TUI.Color)
	}
	if _, err := os.Stat(filepath.Join(emptyGlobal, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("global config.toml should not have been auto-created; stat err = %v", err)
	}
}

func TestLoad_WalkUpMissAutoCreatesGlobal(t *testing.T) {
	startDir := t.TempDir()
	globalDir := t.TempDir()

	cfg, err := Load(WithStartDir(startDir), WithSearchPath(globalDir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	want := filepath.Join(globalDir, "config.toml")
	if cfg.Sources.File != want {
		t.Fatalf("Sources.File = %q, want %q", cfg.Sources.File, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("global config.toml should exist after Load: %v", err)
	}
}

func TestLoad_TaxonomyPopulated(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
[taxonomy]
levels = [["milestone"], ["initiative"], ["story"], ["task", "spike"]]
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), content, 0o644); err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	cfg, err := Load(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := [][]string{{"milestone"}, {"initiative"}, {"story"}, {"task", "spike"}}
	if len(cfg.Taxonomy.Levels) != len(want) {
		t.Fatalf("Taxonomy.Levels = %v, want %v", cfg.Taxonomy.Levels, want)
	}
	for i := range want {
		if !equalStrings(cfg.Taxonomy.Levels[i], want[i]) {
			t.Errorf("Taxonomy.Levels[%d] = %v, want %v", i, cfg.Taxonomy.Levels[i], want[i])
		}
	}
}

func TestLoad_TaxonomyDuplicateLevelIsHardError(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
[taxonomy]
levels = [["story"], ["story"]]
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), content, 0o644); err != nil {
		t.Fatalf("writing config file: %v", err)
	}
	_, err := Load(WithSearchPath(dir))
	if err == nil {
		t.Fatal("expected error for duplicate level name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid taxonomy") || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should wrap taxonomy validation failure, got: %v", err)
	}
}

func TestLoad_TaxonomyEmptyPeerGroupIsHardError(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
[taxonomy]
levels = [["milestone"], [], ["task"]]
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), content, 0o644); err != nil {
		t.Fatalf("writing config file: %v", err)
	}
	_, err := Load(WithSearchPath(dir))
	if err == nil {
		t.Fatal("expected error for empty peer group, got nil")
	}
	if !strings.Contains(err.Error(), "invalid taxonomy") {
		t.Errorf("error should wrap taxonomy validation failure, got: %v", err)
	}
}

func TestLoad_TaxonomyAbsent(t *testing.T) {
	cfg, err := Load(WithSearchPath(t.TempDir()))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(cfg.Taxonomy.Levels) != 0 {
		t.Errorf("Taxonomy.Levels = %v, want empty (embedded default has section commented out)", cfg.Taxonomy.Levels)
	}
}

func TestLoad_ExplicitBeatsWalkUp(t *testing.T) {
	startDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(startDir, "tusk.toml"), []byte("[tui]\ncolor = false\n"), 0o644); err != nil {
		t.Fatalf("writing local config: %v", err)
	}
	explicit := filepath.Join(t.TempDir(), "custom.toml")
	if err := os.WriteFile(explicit, []byte("[tui]\ncolor = true\n"), 0o644); err != nil {
		t.Fatalf("writing explicit: %v", err)
	}

	cfg, err := Load(WithStartDir(startDir), WithExplicitFile(explicit))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Sources.File != explicit {
		t.Fatalf("Sources.File = %q, want %q", cfg.Sources.File, explicit)
	}
}
