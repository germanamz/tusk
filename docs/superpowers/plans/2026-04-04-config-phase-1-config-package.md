# Configuration System — Phase 1: Config Package & Loading

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the `internal/config` package with Viper-based config loading, env var support, and hardcoded defaults. No consumers yet — just the package and its tests.

**Architecture:** A single `Load()` function uses a non-global Viper instance to read `~/.config/tusk/config.toml`, overlay `TUSK_*` environment variables, and unmarshal into typed Go structs. If the config file doesn't exist, defaults are used silently.

**Tech Stack:** Go, [github.com/spf13/viper](https://github.com/spf13/viper), TOML

**Design Spec:** `docs/superpowers/specs/2026-04-04-configuration-system-design.md`

---

### Task 1: Add Viper dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add Viper**

```bash
cd /Users/germanamz/projects/tusk && go get github.com/spf13/viper
```

- [ ] **Step 2: Verify it appears in go.mod**

```bash
grep "spf13/viper" go.mod
```

Expected: a line like `github.com/spf13/viper v1.x.x`

- [ ] **Step 3: Tidy**

```bash
go mod tidy
```

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "feat(config): add viper dependency"
```

---

### Task 2: Define config structs and Load function

**Files:**
- Create: `internal/config/config.go`

This file defines all config structs and the `Load()` function. Read the design spec section "Config Struct" for the full type definitions.

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go` with a test that calls `config.Load()` and checks that defaults are returned when no config file or env vars exist:

```go
package config

import (
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
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test -v ./internal/config/ -run TestLoad_Defaults
```

Expected: compilation error — `config` package doesn't exist yet.

- [ ] **Step 3: Write the implementation**

Create `internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config is the top-level Tusk configuration.
type Config struct {
	Storage StorageConfig `mapstructure:"storage"`
	Urgency UrgencyConfig `mapstructure:"urgency"`
	TUI     TUIConfig     `mapstructure:"tui"`
	MCP     MCPConfig     `mapstructure:"mcp"`
}

// StorageConfig configures the database backend.
type StorageConfig struct {
	Backend  string         `mapstructure:"backend"`
	Path     string         `mapstructure:"path"`
	Postgres PostgresConfig `mapstructure:"postgres"`
}

// PostgresConfig holds PostgreSQL connection settings (future use).
type PostgresConfig struct {
	DSN string `mapstructure:"dsn"`
}

// UrgencyConfig holds weights for the urgency scoring algorithm.
type UrgencyConfig struct {
	PriorityWeight float64 `mapstructure:"priority_weight"`
	DueWeight      float64 `mapstructure:"due_weight"`
	AgeWeight      float64 `mapstructure:"age_weight"`
	BlockingWeight float64 `mapstructure:"blocking_weight"`
	BlockedWeight  float64 `mapstructure:"blocked_weight"`
}

// MCPConfig controls which tools and resources the MCP server exposes.
type MCPConfig struct {
	DisabledToolGroups     []string `mapstructure:"disabled_tool_groups"`
	DisabledTools          []string `mapstructure:"disabled_tools"`
	DisabledResourceGroups []string `mapstructure:"disabled_resource_groups"`
	DisabledResources      []string `mapstructure:"disabled_resources"`
}

// TUIConfig controls CLI output formatting.
type TUIConfig struct {
	DateFormat  string `mapstructure:"date_format"`
	Color       bool   `mapstructure:"color"`
	TreeIndent  int    `mapstructure:"tree_indent"`
	DefaultSort string `mapstructure:"default_sort"`
}

// Option configures the Load function for testing.
type Option func(v *viper.Viper)

// WithSearchPath overrides the config file search path.
// Used in tests to point at a temp directory.
func WithSearchPath(path string) Option {
	return func(v *viper.Viper) {
		v.AddConfigPath(path)
	}
}

// Load reads configuration from file, environment, and defaults.
//
// Precedence (highest to lowest):
//  1. TUSK_* environment variables
//  2. Config file (~/.config/tusk/config.toml)
//  3. Hardcoded defaults
//
// If no config file is found, defaults are used without error.
func Load(opts ...Option) (*Config, error) {
	v := viper.New()

	// Hardcoded defaults
	v.SetDefault("storage.backend", "sqlite")
	v.SetDefault("storage.path", "~/.local/share/tusk/tusk.db")
	v.SetDefault("storage.postgres.dsn", "")

	v.SetDefault("urgency.priority_weight", 6.0)
	v.SetDefault("urgency.due_weight", 12.0)
	v.SetDefault("urgency.age_weight", 2.0)
	v.SetDefault("urgency.blocking_weight", 8.0)
	v.SetDefault("urgency.blocked_weight", -5.0)

	v.SetDefault("tui.date_format", "2006-01-02")
	v.SetDefault("tui.color", true)
	v.SetDefault("tui.tree_indent", 2)
	v.SetDefault("tui.default_sort", "urgency")

	// MCP defaults — empty slices are the zero value, no SetDefault needed.

	// Config file
	v.SetConfigName("config")
	v.SetConfigType("toml")

	// Default search path: ~/.config/tusk/
	hasCustomPath := false
	for _, opt := range opts {
		// Peek: if any option sets a path, skip the default path.
		// We apply them below; this flag just prevents adding the default.
		hasCustomPath = true
		_ = opt
	}
	if !hasCustomPath {
		home, err := os.UserHomeDir()
		if err == nil {
			v.AddConfigPath(filepath.Join(home, ".config", "tusk"))
		}
	}

	// Apply options (e.g., WithSearchPath for tests)
	for _, opt := range opts {
		opt(v)
	}

	// Environment variables: TUSK_STORAGE_PATH, TUSK_TUI_COLOR, etc.
	v.SetEnvPrefix("TUSK")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read config file (ignore "not found" — config is optional)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &cfg, nil
}

// ExpandPath replaces a leading ~ with the user's home directory.
// Returns the path unchanged if it doesn't start with ~.
func ExpandPath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}
```

**Key design notes for the implementer:**
- `viper.New()` creates a non-global instance — this avoids polluting global state.
- `WithSearchPath` is a functional option used by tests to override where Viper looks for config files. Production code calls `Load()` with no options.
- The `hasCustomPath` logic ensures that if any `WithSearchPath` option is passed, we don't also add `~/.config/tusk/` — this prevents tests from accidentally reading the developer's real config file.
- `ExpandPath` is a standalone utility — it's called by `main.go` when resolving the DB path, not inside `Load()`. The config stores the raw `~` path so consumers can see what was configured.

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test -v ./internal/config/ -run TestLoad_Defaults
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add config package with Load function and defaults"
```

---

### Task 3: Test config file loading and env override

**Files:**
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write test for config file loading**

Add to `internal/config/config_test.go`:

```go
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
```

- [ ] **Step 2: Run it**

```bash
go test -v ./internal/config/ -run TestLoad_File
```

Expected: PASS

- [ ] **Step 3: Write test for env var override**

Add to `internal/config/config_test.go`:

```go
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
```

- [ ] **Step 4: Run it**

```bash
go test -v ./internal/config/ -run TestLoad_EnvOverride
```

Expected: PASS

- [ ] **Step 5: Write test for malformed file**

Add to `internal/config/config_test.go`:

```go
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
```

- [ ] **Step 6: Run it**

```bash
go test -v ./internal/config/ -run TestLoad_MalformedFile
```

Expected: PASS

- [ ] **Step 7: Write test for tilde expansion utility**

Add to `internal/config/config_test.go`:

```go
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
```

- [ ] **Step 8: Run all config tests**

```bash
go test -v ./internal/config/
```

Expected: all PASS

- [ ] **Step 9: Commit**

```bash
git add internal/config/config_test.go
git commit -m "test(config): add file loading, env override, malformed, and tilde expansion tests"
```

---

### Task 4: Update `config/default.toml` to match schema

**Files:**
- Modify: `config/default.toml`

The existing `config/default.toml` has fields that no longer match the design (`transport`, `port`, `retry_max`, `retry_base_ms` under `[mcp]`). Update it to match the config struct exactly. This file is documentation — it shows users the full config with all defaults.

- [ ] **Step 1: Rewrite default.toml**

Replace the contents of `config/default.toml` with:

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

# Disable individual MCP tools by name, e.g.:
# disabled_tools = ["tusk_task_delete", "tusk_task_tree"]
disabled_tools = []

# Disable MCP resource groups: ["task", "project", "workflow"]
disabled_resource_groups = []

# Disable individual MCP resources by URI template, e.g.:
# disabled_resources = ["tusk://projects/{name}/workflow"]
disabled_resources = []
```

- [ ] **Step 2: Verify the file is valid TOML**

```bash
go run github.com/pelletier/go-toml/v2/cmd/tomll@latest config/default.toml
```

If `tomll` isn't available, alternatively create a quick Go test or just rely on the fact that Viper will parse it. A simpler check:

```bash
cd /Users/germanamz/projects/tusk && go test -v ./internal/config/ -run TestLoad_File
```

(This already validates TOML parsing works.)

- [ ] **Step 3: Commit**

```bash
git add config/default.toml
git commit -m "docs: update default.toml to match config system schema"
```
