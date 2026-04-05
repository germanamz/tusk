# Config-based Projects — Revised Phase 1: Config Foundation

**Goal:** Add workflow and project config types to the configuration system, add validation, inject builtin defaults, and auto-create the config file on first run.

**Outcome:** After this phase, `config.Load()` parses `[workflows.*]` and `[projects.*]` TOML sections, validates cross-references, and provides builtin defaults when no config is present. **The code compiles and all existing tests pass.** Nothing else in the codebase changes.

**Design spec:** `docs/superpowers/specs/2026-04-04-config-based-projects-design.md`

---

## What you are building

The config system currently has 4 top-level sections: `storage`, `urgency`, `tui`, `mcp`. You are adding 2 more: `workflows` and `projects`. These are TOML maps:

```toml
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
transitions = [
  { from = "pending", to = "active" },
  { from = "active", to = "completed" },
]

[projects.default]
workflow = "kanban"

[projects.backend]
workflow = "kanban"
[projects.backend.settings.auto_complete_parent]
trigger_status = "completed"
target_status = "completed"
```

When no workflows or projects are configured, builtin defaults are injected (a `kanban` workflow and a `default` project). Every project must reference a workflow that exists — this is validated on load.

---

## Files to modify

| File | Action |
|------|--------|
| `internal/config/config.go` | Add types, add to Config struct, add defaults injection, add validation, add auto-creation |
| `internal/config/config_test.go` | Add tests for new functionality |
| `config/default.toml` | Add `[workflows]` and `[projects]` sections |

**No other files are touched in this phase.**

---

## Task 1: Add workflow and project config types

### Step 1: Add types to `internal/config/config.go`

Add these type definitions ABOVE the existing `Config` struct:

```go
// WorkflowTransitionConfig defines an allowed status transition.
type WorkflowTransitionConfig struct {
	From string `mapstructure:"from"`
	To   string `mapstructure:"to"`
}

// WorkflowConfig defines a named workflow with its statuses and transitions.
type WorkflowConfig struct {
	Statuses    []string                   `mapstructure:"statuses"`
	Transitions []WorkflowTransitionConfig `mapstructure:"transitions"`
}

// AutoCompleteParentConfig controls automatic parent completion.
type AutoCompleteParentConfig struct {
	TriggerStatus string `mapstructure:"trigger_status"`
	TargetStatus  string `mapstructure:"target_status"`
}

// AutoRevertParentConfig controls automatic parent revert.
type AutoRevertParentConfig struct {
	TriggerStatus string `mapstructure:"trigger_status"`
	TargetStatus  string `mapstructure:"target_status"`
}

// ProjectSettingsConfig holds per-project automation settings.
type ProjectSettingsConfig struct {
	AutoCompleteParent *AutoCompleteParentConfig `mapstructure:"auto_complete_parent"`
	AutoRevertParent   *AutoRevertParentConfig   `mapstructure:"auto_revert_parent"`
}

// ProjectConfig defines a named project with its workflow assignment and settings.
type ProjectConfig struct {
	Workflow string                `mapstructure:"workflow"`
	Settings ProjectSettingsConfig `mapstructure:"settings"`
}
```

### Step 2: Add fields to the Config struct

Find the `Config` struct (currently has Storage, Urgency, TUI, MCP). Add two new fields at the end:

```go
type Config struct {
	Storage   StorageConfig              `mapstructure:"storage"`
	Urgency   UrgencyConfig              `mapstructure:"urgency"`
	TUI       TUIConfig                  `mapstructure:"tui"`
	MCP       MCPConfig                  `mapstructure:"mcp"`
	Workflows map[string]WorkflowConfig  `mapstructure:"workflows"`
	Projects  map[string]ProjectConfig   `mapstructure:"projects"`
}
```

### Step 3: Add builtin defaults injection and validation in Load()

In `Load()`, AFTER the `v.Unmarshal(&cfg)` block and BEFORE the `return &cfg, nil`, add:

```go
	// Inject builtin workflow if no workflows configured
	if len(cfg.Workflows) == 0 {
		cfg.Workflows = map[string]WorkflowConfig{
			"kanban": {
				Statuses: []string{"pending", "active", "completed", "deleted"},
				Transitions: []WorkflowTransitionConfig{
					{From: "pending", To: "active"},
					{From: "pending", To: "deleted"},
					{From: "active", To: "completed"},
					{From: "active", To: "pending"},
					{From: "active", To: "deleted"},
					{From: "completed", To: "pending"},
				},
			},
		}
	}

	// Inject builtin project if no projects configured
	if len(cfg.Projects) == 0 {
		cfg.Projects = map[string]ProjectConfig{
			"default": {Workflow: "kanban"},
		}
	}

	// Validate cross-references
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
```

### Step 4: Add the validate method

Add this method anywhere in `config.go`:

```go
// validate checks cross-references between config sections.
func (c *Config) validate() error {
	for id, proj := range c.Projects {
		if _, ok := c.Workflows[proj.Workflow]; !ok {
			return fmt.Errorf("project %q references unknown workflow %q", id, proj.Workflow)
		}
	}
	return nil
}
```

### Step 5: Verify

Run: `go build ./internal/config/...`
Expected: compiles with no errors.

Run: `go test ./internal/config/...`
Expected: all existing tests pass (the new fields are simply empty/nil when not configured, and builtins are injected).

---

## Task 2: Add config auto-creation

When no config file exists, create one at the search path with default content so users have a starting point.

### Step 1: Add the default config content and ensureConfigFile

Add this ABOVE the `Load()` function in `config.go`:

```go
// defaultConfigContent is written to disk when no config file exists.
const defaultConfigContent = `# Tusk Configuration
# Place this file at ~/.config/tusk/config.toml
# All values shown are defaults — only include settings you want to override.

[storage]
backend = "sqlite"
path    = "~/.local/share/tusk/tusk.db"

[storage.postgres]
dsn = ""

[urgency]
priority_weight = 6.0
due_weight      = 12.0
age_weight      = 2.0
blocking_weight = 8.0
blocked_weight  = -5.0

[tui]
date_format  = "2006-01-02"
color        = true
tree_indent  = 2
default_sort = "urgency"

[mcp]
disabled_tool_groups = []
disabled_tools = []
disabled_resource_groups = []
disabled_resources = []

# Workflows define allowed status transitions.
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
transitions = [
  { from = "pending",   to = "active" },
  { from = "pending",   to = "deleted" },
  { from = "active",    to = "completed" },
  { from = "active",    to = "pending" },
  { from = "active",    to = "deleted" },
  { from = "completed", to = "pending" },
]

# Projects group tasks and assign workflows.
[projects.default]
workflow = "kanban"
`

// ensureConfigFile creates the config file with default content if it doesn't exist.
func ensureConfigFile(searchPath string) error {
	configPath := filepath.Join(searchPath, "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		return nil // file already exists
	}
	if err := os.MkdirAll(searchPath, 0o755); err != nil {
		return fmt.Errorf("creating config directory %s: %w", searchPath, err)
	}
	if err := os.WriteFile(configPath, []byte(defaultConfigContent), 0o644); err != nil {
		return fmt.Errorf("writing default config: %w", err)
	}
	return nil
}
```

### Step 2: Call ensureConfigFile from Load()

In `Load()`, find the block that sets up the search path. Currently it looks like:

```go
	if lo.searchPath != "" {
		v.AddConfigPath(lo.searchPath)
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			v.AddConfigPath(filepath.Join(home, ".config", "tusk"))
		}
	}
```

Replace it with:

```go
	var searchPath string
	if lo.searchPath != "" {
		searchPath = lo.searchPath
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			searchPath = filepath.Join(home, ".config", "tusk")
		}
	}

	if searchPath != "" {
		v.AddConfigPath(searchPath)
		if err := ensureConfigFile(searchPath); err != nil {
			return nil, err
		}
	}
```

### Step 3: Verify

Run: `go build ./internal/config/...`
Run: `go test ./internal/config/...`
Expected: compiles and all tests pass.

---

## Task 3: Add tests for new functionality

### Step 1: Add tests to `internal/config/config_test.go`

Add these test functions to the existing file. You need to add `"strings"` to the imports.

```go
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
```

### Step 2: Verify all tests pass

Run: `go test -v ./internal/config/...`
Expected: all tests (old and new) pass.

---

## Task 4: Update default.toml

### Step 1: Add workflow and project sections to `config/default.toml`

Add these sections at the end of the existing file:

```toml
# Workflows define allowed status transitions.
# The builtin "kanban" workflow is always available even without config.
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
transitions = [
  { from = "pending",   to = "active" },
  { from = "pending",   to = "deleted" },
  { from = "active",    to = "completed" },
  { from = "active",    to = "pending" },
  { from = "active",    to = "deleted" },
  { from = "completed", to = "pending" },
]

# Projects group tasks and assign workflows.
# The builtin "default" project uses the "kanban" workflow.
[projects.default]
workflow = "kanban"

# Example: custom project with auto-completion
# [projects.backend]
# workflow = "kanban"
# [projects.backend.settings.auto_complete_parent]
# trigger_status = "completed"
# target_status = "completed"
```

### Step 2: Final verification

Run: `make test`
Expected: all tests pass, no compilation errors.

---

## Commit

```
git add internal/config/config.go internal/config/config_test.go config/default.toml
git commit -m "feat(config): add workflow and project config types with validation and auto-creation"
```
