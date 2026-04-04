# Config-based Projects — Phase 1: Config Foundation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add project and workflow config types to the configuration system, validate cross-references, and auto-create the config file on first run.

**Architecture:** This phase is purely additive — it extends `internal/config/config.go` with new types and validation, updates `config/default.toml` with new sections, and adds auto-creation logic. No existing code breaks. Everything compiles and tests pass after this phase.

**Tech Stack:** Go, Viper (github.com/spf13/viper), TOML

**Prerequisite:** Declarative Workflows initiative must ship first (workflow DB tables dropped, workflows config-driven). This phase adds the config types that both workflows and projects will use.

**Design spec:** `docs/superpowers/specs/2026-04-04-config-based-projects-design.md`

---

### Task 1: Add workflow and project config types to Config struct

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

This task adds the Go struct types for `[workflows.<name>]` and `[projects.<id>]` TOML sections. It does NOT add validation yet (Task 2) or auto-creation (Task 3).

- [ ] **Step 1: Write failing test for workflow config parsing**

Create or extend `internal/config/config_test.go` with a test that loads a TOML string containing a `[workflows.kanban]` section and asserts the parsed struct has the right values.

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

	cfg, err := config.Load(config.WithSearchPath(dir))
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
	if wf.Statuses[0] != "pending" {
		t.Errorf("expected first status 'pending', got %q", wf.Statuses[0])
	}
	if len(wf.Transitions) != 2 {
		t.Errorf("expected 2 transitions, got %d", len(wf.Transitions))
	}
	if wf.Transitions[0].From != "pending" || wf.Transitions[0].To != "active" {
		t.Errorf("unexpected first transition: %+v", wf.Transitions[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/config -run TestLoad_WorkflowConfig`
Expected: Compilation error — `cfg.Workflows` does not exist on `Config` struct.

- [ ] **Step 3: Add workflow config types to config.go**

In `internal/config/config.go`, add the following types and update the `Config` struct:

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
```

Add the `Workflows` field to the `Config` struct:

```go
type Config struct {
	Storage   StorageConfig              `mapstructure:"storage"`
	Urgency   UrgencyConfig              `mapstructure:"urgency"`
	TUI       TUIConfig                  `mapstructure:"tui"`
	MCP       MCPConfig                  `mapstructure:"mcp"`
	Workflows map[string]WorkflowConfig  `mapstructure:"workflows"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/config -run TestLoad_WorkflowConfig`
Expected: PASS

- [ ] **Step 5: Write failing test for project config parsing**

Add to `internal/config/config_test.go`:

```go
func TestLoad_ProjectConfig(t *testing.T) {
	dir := t.TempDir()
	content := `
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

	cfg, err := config.Load(config.WithSearchPath(dir))
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
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test -v ./internal/config -run TestLoad_ProjectConfig`
Expected: Compilation error — `cfg.Projects` does not exist.

- [ ] **Step 7: Add project config types to config.go**

In `internal/config/config.go`, add:

```go
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

Add the `Projects` field to the `Config` struct:

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

- [ ] **Step 8: Run test to verify it passes**

Run: `go test -v ./internal/config -run TestLoad_ProjectConfig`
Expected: PASS

- [ ] **Step 9: Write test for builtin defaults when no config sections present**

```go
func TestLoad_BuiltinDefaults(t *testing.T) {
	dir := t.TempDir()
	// Empty config file — no [projects] or [workflows] sections
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(config.WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Builtin "kanban" workflow should exist
	wf, ok := cfg.Workflows["kanban"]
	if !ok {
		t.Fatal("expected builtin kanban workflow")
	}
	if len(wf.Statuses) != 4 {
		t.Errorf("expected 4 kanban statuses, got %d", len(wf.Statuses))
	}

	// Builtin "default" project should exist
	proj, ok := cfg.Projects["default"]
	if !ok {
		t.Fatal("expected builtin default project")
	}
	if proj.Workflow != "kanban" {
		t.Errorf("expected default project workflow 'kanban', got %q", proj.Workflow)
	}
}
```

- [ ] **Step 10: Run test to verify it fails**

Run: `go test -v ./internal/config -run TestLoad_BuiltinDefaults`
Expected: FAIL — no builtin injection logic yet.

- [ ] **Step 11: Add builtin defaults injection in Load()**

In `internal/config/config.go`, after the `v.Unmarshal(&cfg)` call in `Load()`, add builtin injection:

```go
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

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

	return &cfg, nil
```

- [ ] **Step 12: Run test to verify it passes**

Run: `go test -v ./internal/config -run TestLoad_BuiltinDefaults`
Expected: PASS

- [ ] **Step 13: Run all config tests**

Run: `go test -v ./internal/config`
Expected: All PASS

- [ ] **Step 14: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add workflow and project config types with builtin defaults"
```

---

### Task 2: Add config validation

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

Validate that every project references a workflow that exists in config. This catches typos and misconfigurations at startup.

- [ ] **Step 1: Write failing test for valid cross-reference**

```go
func TestLoad_ValidationProjectReferencesValidWorkflow(t *testing.T) {
	dir := t.TempDir()
	content := `
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
transitions = []

[projects.default]
workflow = "kanban"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := config.Load(config.WithSearchPath(dir))
	if err != nil {
		t.Fatalf("expected no error for valid config, got: %v", err)
	}
}
```

- [ ] **Step 2: Write failing test for invalid cross-reference**

```go
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

	_, err := config.Load(config.WithSearchPath(dir))
	if err == nil {
		t.Fatal("expected error for project referencing unknown workflow")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("expected error to mention 'nonexistent', got: %v", err)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test -v ./internal/config -run TestLoad_Validation`
Expected: `TestLoad_ValidationProjectReferencesUnknownWorkflow` FAILS because no validation exists.

- [ ] **Step 4: Add validateConfig function**

In `internal/config/config.go`, add a `validate` method and call it from `Load()`:

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

In `Load()`, call `cfg.validate()` after builtin injection but before returning:

```go
	// ... builtin defaults injection ...

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -v ./internal/config -run TestLoad_Validation`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): validate project workflow references on load"
```

---

### Task 3: Auto-create config file when not found

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

When no config file exists at `~/.config/tusk/config.toml`, create it with default contents so users have a starting point to edit.

- [ ] **Step 1: Write failing test for auto-creation**

```go
func TestLoad_AutoCreateConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// Verify file does NOT exist before Load
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("config file should not exist before Load")
	}

	cfg, err := config.Load(config.WithSearchPath(dir))
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

	// Verify the config loaded correctly (builtins should be present)
	if _, ok := cfg.Projects["default"]; !ok {
		t.Fatal("expected builtin default project")
	}
	if _, ok := cfg.Workflows["kanban"]; !ok {
		t.Fatal("expected builtin kanban workflow")
	}

	// Verify file contains expected content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading created config: %v", err)
	}
	if !strings.Contains(string(content), "[storage]") {
		t.Error("expected config file to contain [storage] section")
	}
	if !strings.Contains(string(content), "[projects.default]") {
		t.Error("expected config file to contain [projects.default] section")
	}
	if !strings.Contains(string(content), "[workflows.kanban]") {
		t.Error("expected config file to contain [workflows.kanban] section")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/config -run TestLoad_AutoCreateConfigFile`
Expected: FAIL — config file is not created.

- [ ] **Step 3: Add default config content and auto-creation logic**

In `internal/config/config.go`, add an embedded default config string and the auto-creation function:

```go
// defaultConfigContent is written to disk when no config file exists.
// It serves as a starting point for users to customize.
const defaultConfigContent = `# Tusk Configuration
#
# All values shown below are the defaults — you only need to
# include settings you want to override.
#
# Every key can also be set via environment variable with the
# TUSK_ prefix. Nesting uses underscores:
#   storage.path       → TUSK_STORAGE_PATH
#   tui.color          → TUSK_TUI_COLOR
#   urgency.due_weight → TUSK_URGENCY_DUE_WEIGHT

[storage]
backend = "sqlite"                         # "sqlite" (only supported backend currently)
path    = "~/.local/share/tusk/tusk.db"    # SQLite database file path

[storage.postgres]
dsn = ""  # PostgreSQL connection string (future use)

[urgency]
priority_weight = 6.0    # Weight for task priority in urgency score
due_weight      = 12.0   # Weight for due date proximity
age_weight      = 2.0    # Weight for task age
blocking_weight = 8.0    # Weight for tasks that block others
blocked_weight  = -5.0   # Weight for tasks that are blocked

[tui]
date_format  = "2006-01-02"  # Go time format for date display
color        = true           # Enable colored output (respects NO_COLOR)
tree_indent  = 2              # Spaces per indent level in tree view
default_sort = "urgency"      # Default sort field for task lists

[mcp]
# Disable MCP tool groups: ["task", "relation", "project"]
disabled_tool_groups = []
# Disable individual MCP tools by name
disabled_tools = []
# Disable MCP resource groups: ["task", "project", "workflow"]
disabled_resource_groups = []
# Disable individual MCP resources by URI template
disabled_resources = []

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
`

// ensureConfigFile creates the config file with default content if it doesn't exist.
// Returns the path to the config directory (for Viper's search path).
func ensureConfigFile(searchPath string) error {
	configPath := filepath.Join(searchPath, "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		return nil // file exists
	}
	// Create directory if needed
	if err := os.MkdirAll(searchPath, 0o755); err != nil {
		return fmt.Errorf("creating config directory %s: %w", searchPath, err)
	}
	if err := os.WriteFile(configPath, []byte(defaultConfigContent), 0o644); err != nil {
		return fmt.Errorf("writing default config: %w", err)
	}
	return nil
}
```

In `Load()`, call `ensureConfigFile` before `v.ReadInConfig()`. Update the section that resolves the search path:

```go
	// Use custom search path if provided, otherwise default to ~/.config/tusk/.
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
		// Auto-create config file with defaults if it doesn't exist
		if err := ensureConfigFile(searchPath); err != nil {
			return nil, err
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/config -run TestLoad_AutoCreateConfigFile`
Expected: PASS

- [ ] **Step 5: Write test verifying auto-creation does NOT overwrite existing config**

```go
func TestLoad_AutoCreateDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// Write a custom config
	custom := `[tui]
color = false
`
	if err := os.WriteFile(configPath, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(config.WithSearchPath(dir))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Custom value should be preserved
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

- [ ] **Step 6: Run test to verify it passes**

Run: `go test -v ./internal/config -run TestLoad_AutoCreateDoesNotOverwrite`
Expected: PASS (the `os.Stat` check in `ensureConfigFile` prevents overwrite).

- [ ] **Step 7: Run all config tests**

Run: `go test -v ./internal/config`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): auto-create config file with defaults on first run"
```

---

### Task 4: Update default.toml template

**Files:**
- Modify: `config/default.toml`

Update the reference template to include the new `[workflows]` and `[projects]` sections. This file is a reference for users — it's not loaded at runtime (the auto-created file uses `defaultConfigContent` from config.go).

- [ ] **Step 1: Update config/default.toml**

Replace the entire contents of `config/default.toml` with the following (same content as `defaultConfigContent` in config.go — they must stay in sync):

```toml
# Tusk Configuration
#
# Place this file at ~/.config/tusk/config.toml
# All values shown below are the defaults — you only need to
# include settings you want to override.
#
# Every key can also be set via environment variable with the
# TUSK_ prefix. Nesting uses underscores:
#   storage.path       → TUSK_STORAGE_PATH
#   tui.color          → TUSK_TUI_COLOR
#   urgency.due_weight → TUSK_URGENCY_DUE_WEIGHT

[storage]
backend = "sqlite"                         # "sqlite" (only supported backend currently)
path    = "~/.local/share/tusk/tusk.db"    # SQLite database file path

[storage.postgres]
dsn = ""  # PostgreSQL connection string (future use)

[urgency]
priority_weight = 6.0    # Weight for task priority in urgency score
due_weight      = 12.0   # Weight for due date proximity
age_weight      = 2.0    # Weight for task age
blocking_weight = 8.0    # Weight for tasks that block others
blocked_weight  = -5.0   # Weight for tasks that are blocked

[tui]
date_format  = "2006-01-02"  # Go time format for date display
color        = true           # Enable colored output (respects NO_COLOR)
tree_indent  = 2              # Spaces per indent level in tree view
default_sort = "urgency"      # Default sort field for task lists

[mcp]
# Disable MCP tool groups: ["task", "relation", "project"]
disabled_tool_groups = []
# Disable individual MCP tools by name
disabled_tools = []
# Disable MCP resource groups: ["task", "project", "workflow"]
disabled_resource_groups = []
# Disable individual MCP resources by URI template
disabled_resources = []

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

- [ ] **Step 2: Run all tests to verify nothing broke**

Run: `make test`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add config/default.toml
git commit -m "docs: update default.toml with workflows and projects sections"
```
