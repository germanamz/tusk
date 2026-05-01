package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_Defaults(test *testing.T) {
	// Load with no config file and no env vars — should return defaults.
	// We set the config search path to a temp dir that has no config.toml.
	cfg, err := Load(WithSearchPath(test.TempDir()))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	// Storage defaults
	if cfg.Storage.Backend != "sqlite" {
		test.Errorf("Storage.Backend = %q, want %q", cfg.Storage.Backend, "sqlite")
	}
	if cfg.Storage.Path != "~/.local/share/tusk/tusk.db" {
		test.Errorf("Storage.Path = %q, want %q", cfg.Storage.Path, "~/.local/share/tusk/tusk.db")
	}

	// Urgency defaults
	if cfg.Urgency.PriorityWeight != 6.0 {
		test.Errorf("Urgency.PriorityWeight = %v, want 6.0", cfg.Urgency.PriorityWeight)
	}
	if cfg.Urgency.DueWeight != 12.0 {
		test.Errorf("Urgency.DueWeight = %v, want 12.0", cfg.Urgency.DueWeight)
	}
	if cfg.Urgency.AgeWeight != 2.0 {
		test.Errorf("Urgency.AgeWeight = %v, want 2.0", cfg.Urgency.AgeWeight)
	}
	if cfg.Urgency.BlockingWeight != 8.0 {
		test.Errorf("Urgency.BlockingWeight = %v, want 8.0", cfg.Urgency.BlockingWeight)
	}
	if cfg.Urgency.BlockedWeight != -5.0 {
		test.Errorf("Urgency.BlockedWeight = %v, want -5.0", cfg.Urgency.BlockedWeight)
	}

	// TUI defaults
	if cfg.TUI.DateFormat != "2006-01-02" {
		test.Errorf("TUI.DateFormat = %q, want %q", cfg.TUI.DateFormat, "2006-01-02")
	}
	if cfg.TUI.Color != true {
		test.Errorf("TUI.Color = %v, want true", cfg.TUI.Color)
	}
	if cfg.TUI.TreeIndent != 2 {
		test.Errorf("TUI.TreeIndent = %d, want 2", cfg.TUI.TreeIndent)
	}
	if cfg.TUI.DefaultSort != "urgency" {
		test.Errorf("TUI.DefaultSort = %q, want %q", cfg.TUI.DefaultSort, "urgency")
	}

	// MCP defaults
	if len(cfg.MCP.DisabledToolGroups) != 0 {
		test.Errorf("MCP.DisabledToolGroups = %v, want empty", cfg.MCP.DisabledToolGroups)
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
		test.Fatalf("MCP.DisabledTools = %v, want %v", cfg.MCP.DisabledTools, wantDisabledTools)
	}
	for index, want := range wantDisabledTools {
		if cfg.MCP.DisabledTools[index] != want {
			test.Errorf("MCP.DisabledTools[%d] = %q, want %q", index, cfg.MCP.DisabledTools[index], want)
		}
	}
	if len(cfg.MCP.DisabledResourceGroups) != 0 {
		test.Errorf("MCP.DisabledResourceGroups = %v, want empty", cfg.MCP.DisabledResourceGroups)
	}
	if len(cfg.MCP.DisabledResources) != 0 {
		test.Errorf("MCP.DisabledResources = %v, want empty", cfg.MCP.DisabledResources)
	}

	// Blocked fields defaults
	if got, want := cfg.MCP.BlockedFields["tusk_project_modify"], []string{"workflow"}; !equalStrings(got, want) {
		test.Errorf("MCP.BlockedFields[tusk_project_modify] = %v, want %v", got, want)
	}
	if got, want := cfg.MCP.BlockedFields["tusk_project_delete"], []string{"force"}; !equalStrings(got, want) {
		test.Errorf("MCP.BlockedFields[tusk_project_delete] = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func TestLoad_File(test *testing.T) {
	// Write a TOML config file to a temp directory.
	dir := test.TempDir()
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
		test.Fatalf("writing config file: %v", err)
	}

	cfg, err := Load(WithSearchPath(dir))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	// Overridden values
	if cfg.Storage.Backend != "postgres" {
		test.Errorf("Storage.Backend = %q, want %q", cfg.Storage.Backend, "postgres")
	}
	if cfg.Storage.Path != "/custom/path/tusk.db" {
		test.Errorf("Storage.Path = %q, want %q", cfg.Storage.Path, "/custom/path/tusk.db")
	}
	if cfg.Urgency.PriorityWeight != 10.0 {
		test.Errorf("Urgency.PriorityWeight = %v, want 10.0", cfg.Urgency.PriorityWeight)
	}
	if cfg.TUI.Color != false {
		test.Errorf("TUI.Color = %v, want false", cfg.TUI.Color)
	}
	if cfg.TUI.TreeIndent != 4 {
		test.Errorf("TUI.TreeIndent = %d, want 4", cfg.TUI.TreeIndent)
	}

	// MCP disabled lists
	if len(cfg.MCP.DisabledTools) != 1 || cfg.MCP.DisabledTools[0] != "tusk_task_delete" {
		test.Errorf("MCP.DisabledTools = %v, want [tusk_task_delete]", cfg.MCP.DisabledTools)
	}
	if len(cfg.MCP.DisabledToolGroups) != 1 || cfg.MCP.DisabledToolGroups[0] != "task_relations" {
		test.Errorf("MCP.DisabledToolGroups = %v, want [task_relations]", cfg.MCP.DisabledToolGroups)
	}

	// Non-overridden values keep defaults
	if cfg.Urgency.DueWeight != 12.0 {
		test.Errorf("Urgency.DueWeight = %v, want 12.0 (default)", cfg.Urgency.DueWeight)
	}
	if cfg.TUI.DateFormat != "2006-01-02" {
		test.Errorf("TUI.DateFormat = %q, want %q (default)", cfg.TUI.DateFormat, "2006-01-02")
	}
}

func TestLoad_EnvOverride(test *testing.T) {
	// Write a config file with one value, then override it with an env var.
	dir := test.TempDir()
	content := []byte(`
[storage]
path = "/from-file/tusk.db"
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), content, 0o644); err != nil {
		test.Fatalf("writing config file: %v", err)
	}

	// Env var should override file value.
	test.Setenv("TUSK_STORAGE_PATH", "/from-env/tusk.db")
	test.Setenv("TUSK_TUI_COLOR", "false")

	cfg, err := Load(WithSearchPath(dir))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	if cfg.Storage.Path != "/from-env/tusk.db" {
		test.Errorf("Storage.Path = %q, want %q (env override)", cfg.Storage.Path, "/from-env/tusk.db")
	}
	if cfg.TUI.Color != false {
		test.Errorf("TUI.Color = %v, want false (env override)", cfg.TUI.Color)
	}
}

func TestLoad_MalformedFile(test *testing.T) {
	dir := test.TempDir()
	content := []byte(`this is not valid toml [[[`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), content, 0o644); err != nil {
		test.Fatalf("writing config file: %v", err)
	}

	_, err := Load(WithSearchPath(dir))
	if err == nil {
		test.Fatal("Load() should return error for malformed TOML")
	}
}

func TestExpandPath(test *testing.T) {
	home, err := os.UserHomeDir()

	if err != nil {
		test.Skip("cannot determine home dir")
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

	for _, testCase := range tests {
		got := ExpandPath(testCase.input)
		if got != testCase.want {
			test.Errorf("ExpandPath(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
}

func TestLoad_LegacyWorkflowSectionIsHardError(test *testing.T) {
	dir := test.TempDir()
	content := `
[workflows.kanban.statuses.pending]
roles = ["initial"]
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		test.Fatal(err)
	}
	_, err := Load(WithSearchPath(dir))
	if err == nil {
		test.Fatal("expected error for legacy [workflows.*] section")
	}
	if !strings.Contains(err.Error(), "workflows") {
		test.Errorf("error should mention workflows, got: %v", err)
	}
}

func TestLoad_LegacyProjectSectionIsHardError(test *testing.T) {
	dir := test.TempDir()
	content := `
[projects.default]
workflow = "kanban"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		test.Fatal(err)
	}
	_, err := Load(WithSearchPath(dir))
	if err == nil {
		test.Fatal("expected error for legacy [projects.*] section")
	}
	if !strings.Contains(err.Error(), "projects") {
		test.Errorf("error should mention projects, got: %v", err)
	}
}

func TestLoad_AutoCreateConfigFile(test *testing.T) {
	dir := test.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// Verify file does NOT exist before Load
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		test.Fatal("config file should not exist before Load")
	}

	cfg, statErr := Load(WithSearchPath(dir))

	if statErr != nil {
		test.Fatalf("Load() error: %v", statErr)
	}

	// Verify file WAS created
	info, err := os.Stat(configPath)

	if err != nil {
		test.Fatalf("config file should exist after Load: %v", err)
	}

	if info.Size() == 0 {
		test.Fatal("config file should not be empty")
	}

	// Auto-created file should round-trip through Load without errors and
	// yield the embedded defaults — projects and workflows live in the DB
	// now, so there is nothing left to assert on the Config struct itself
	// beyond the global sections.
	if cfg.Storage.Backend != "sqlite" {
		test.Errorf("Storage.Backend = %q, want sqlite", cfg.Storage.Backend)
	}
}

func TestLoad_AutoCreateDoesNotOverwrite(test *testing.T) {
	dir := test.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	custom := `[tui]
color = false
`
	if err := os.WriteFile(configPath, []byte(custom), 0o644); err != nil {
		test.Fatal(err)
	}

	cfg, err := Load(WithSearchPath(dir))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	if cfg.TUI.Color != false {
		test.Error("expected color=false from custom config")
	}

	// File should NOT have been overwritten
	content, readErr := os.ReadFile(configPath)

	if readErr != nil {
		test.Fatal(readErr)
	}

	if string(content) != custom {
		test.Error("config file was overwritten")
	}
}

func TestLoad_SourcesFileForGlobal(test *testing.T) {
	dir := test.TempDir()
	cfg, err := Load(WithSearchPath(dir))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	want := filepath.Join(dir, "config.toml")
	if cfg.Sources.File != want {
		test.Fatalf("Sources.File = %q, want %q", cfg.Sources.File, want)
	}
}

func TestLoad_SourcesFileForExplicit(test *testing.T) {
	dir := test.TempDir()
	explicit := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(explicit, []byte("[storage]\npath=\"/tmp/x.db\"\n"), 0o644); err != nil {
		test.Fatalf("writing explicit file: %v", err)
	}

	cfg, err := Load(WithExplicitFile(explicit))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	if cfg.Sources.File != explicit {
		test.Fatalf("Sources.File = %q, want %q", cfg.Sources.File, explicit)
	}
	if cfg.Storage.Path != "/tmp/x.db" {
		test.Fatalf("Storage.Path = %q, want value from explicit file", cfg.Storage.Path)
	}
}

func TestLoad_ExplicitFileMissingIsHardError(test *testing.T) {
	missing := filepath.Join(test.TempDir(), "nope.toml")
	_, err := Load(WithExplicitFile(missing))
	if err == nil {
		test.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "config file not found") {
		test.Fatalf("error %q should mention \"config file not found\"", err.Error())
	}
}

func TestLoad_WalkUpUsesLocalFile(test *testing.T) {
	startDir := test.TempDir()
	local := filepath.Join(startDir, "tusk.toml")
	if err := os.WriteFile(local, []byte("[tui]\ncolor = false\n"), 0o644); err != nil {
		test.Fatalf("writing local config: %v", err)
	}
	emptyGlobal := test.TempDir()

	cfg, err := Load(WithStartDir(startDir), WithSearchPath(emptyGlobal))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	if cfg.Sources.File != local {
		test.Fatalf("Sources.File = %q, want %q", cfg.Sources.File, local)
	}
	if cfg.TUI.Color != false {
		test.Fatalf("TUI.Color = %v, want false", cfg.TUI.Color)
	}
	if _, statErr := os.Stat(filepath.Join(emptyGlobal, "config.toml")); !os.IsNotExist(statErr) {
		test.Fatalf("global config.toml should not have been auto-created; stat err = %v", statErr)
	}
}

func TestLoad_WalkUpMissAutoCreatesGlobal(test *testing.T) {
	startDir := test.TempDir()
	globalDir := test.TempDir()

	cfg, err := Load(WithStartDir(startDir), WithSearchPath(globalDir))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	want := filepath.Join(globalDir, "config.toml")
	if cfg.Sources.File != want {
		test.Fatalf("Sources.File = %q, want %q", cfg.Sources.File, want)
	}
	if _, statErr := os.Stat(want); statErr != nil {
		test.Fatalf("global config.toml should exist after Load: %v", statErr)
	}
}

func TestLoad_TaxonomyPopulated(test *testing.T) {
	dir := test.TempDir()
	content := []byte(`
[taxonomy]
levels = [["milestone"], ["initiative"], ["story"], ["task", "spike"]]
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), content, 0o644); err != nil {
		test.Fatalf("writing config file: %v", err)
	}

	cfg, err := Load(WithSearchPath(dir))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	want := [][]string{{"milestone"}, {"initiative"}, {"story"}, {"task", "spike"}}
	if len(cfg.Taxonomy.Levels) != len(want) {
		test.Fatalf("Taxonomy.Levels = %v, want %v", cfg.Taxonomy.Levels, want)
	}
	for index := range want {
		if !equalStrings(cfg.Taxonomy.Levels[index], want[index]) {
			test.Errorf("Taxonomy.Levels[%d] = %v, want %v", index, cfg.Taxonomy.Levels[index], want[index])
		}
	}
}

func TestLoad_TaxonomyDuplicateLevelIsHardError(test *testing.T) {
	dir := test.TempDir()
	content := []byte(`
[taxonomy]
levels = [["story"], ["story"]]
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), content, 0o644); err != nil {
		test.Fatalf("writing config file: %v", err)
	}
	_, err := Load(WithSearchPath(dir))
	if err == nil {
		test.Fatal("expected error for duplicate level name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid taxonomy") || !strings.Contains(err.Error(), "duplicate") {
		test.Errorf("error should wrap taxonomy validation failure, got: %v", err)
	}
}

func TestLoad_TaxonomyEmptyPeerGroupIsHardError(test *testing.T) {
	dir := test.TempDir()
	content := []byte(`
[taxonomy]
levels = [["milestone"], [], ["task"]]
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), content, 0o644); err != nil {
		test.Fatalf("writing config file: %v", err)
	}
	_, err := Load(WithSearchPath(dir))
	if err == nil {
		test.Fatal("expected error for empty peer group, got nil")
	}
	if !strings.Contains(err.Error(), "invalid taxonomy") {
		test.Errorf("error should wrap taxonomy validation failure, got: %v", err)
	}
}

func TestLoad_TaxonomyAbsent(test *testing.T) {
	cfg, err := Load(WithSearchPath(test.TempDir()))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Taxonomy.Levels) != 0 {
		test.Errorf("Taxonomy.Levels = %v, want empty (embedded default has section commented out)", cfg.Taxonomy.Levels)
	}
}

func TestLoad_ExplicitBeatsWalkUp(test *testing.T) {
	startDir := test.TempDir()
	if err := os.WriteFile(filepath.Join(startDir, "tusk.toml"), []byte("[tui]\ncolor = false\n"), 0o644); err != nil {
		test.Fatalf("writing local config: %v", err)
	}
	explicit := filepath.Join(test.TempDir(), "custom.toml")
	if err := os.WriteFile(explicit, []byte("[tui]\ncolor = true\n"), 0o644); err != nil {
		test.Fatalf("writing explicit: %v", err)
	}

	cfg, err := Load(WithStartDir(startDir), WithExplicitFile(explicit))

	if err != nil {
		test.Fatalf("Load() error: %v", err)
	}

	if cfg.Sources.File != explicit {
		test.Fatalf("Sources.File = %q, want %q", cfg.Sources.File, explicit)
	}
}
