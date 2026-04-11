# Config CLI — Phase 1: Config Package Write Infrastructure

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add TOML struct tags to all config types, implement `LoadFile()`, `WriteConfig()`, and `ConfigFilePath()` in the config package, and promote `pelletier/go-toml/v2` to a direct dependency.

**Architecture:** The config package gains a write path alongside the existing Viper-based read path. `LoadFile` parses a single TOML file via go-toml (no Viper, no env, no defaults). `WriteConfig` marshals a `Config` struct to TOML and writes atomically. `ConfigFilePath` extracts the path resolution logic from `Load()` into a reusable function.

**Tech Stack:** Go, `pelletier/go-toml/v2`, existing `config` package

**Prerequisites:** None — builds on the base codebase.

**Design spec:** `docs/superpowers/specs/2026-04-11-config-cli-design.md`

---

### Task 1: Add TOML struct tags to all config types

**Files:**
- Modify: `config/config.go:18-121`

Every config struct needs a `toml` tag matching its `mapstructure` tag. This enables `pelletier/go-toml/v2` to marshal/unmarshal the Config struct directly.

- [ ] **Step 1: Add `toml` tags to all struct fields**

Add `toml:"..."` tags to every field in every config struct. The tag values match the existing `mapstructure` values exactly. Modify these structs in `config/config.go`:

```go
type WorkflowTransitionConfig struct {
	From string `mapstructure:"from" toml:"from"`
	To   string `mapstructure:"to"   toml:"to"`
}

type WorkflowConfig struct {
	Statuses          []string                   `mapstructure:"statuses"           toml:"statuses"`
	Transitions       []WorkflowTransitionConfig `mapstructure:"transitions"        toml:"transitions"`
	HighlightStatuses []string                   `mapstructure:"highlight_statuses" toml:"highlight_statuses"`
	DimStatuses       []string                   `mapstructure:"dim_statuses"       toml:"dim_statuses"`
}

type AutoCompleteParentConfig struct {
	TriggerStatus string `mapstructure:"trigger_status" toml:"trigger_status"`
	TargetStatus  string `mapstructure:"target_status"  toml:"target_status"`
}

type AutoRevertParentConfig struct {
	TriggerStatus string `mapstructure:"trigger_status" toml:"trigger_status"`
	TargetStatus  string `mapstructure:"target_status"  toml:"target_status"`
}

type ProjectUrgencyConfig struct {
	PriorityWeight    *float64 `mapstructure:"priority_weight"    toml:"priority_weight,omitempty"`
	DueWeight         *float64 `mapstructure:"due_weight"         toml:"due_weight,omitempty"`
	AgeWeight         *float64 `mapstructure:"age_weight"         toml:"age_weight,omitempty"`
	ActiveWeight      *float64 `mapstructure:"active_weight"      toml:"active_weight,omitempty"`
	BlockingWeight    *float64 `mapstructure:"blocking_weight"    toml:"blocking_weight,omitempty"`
	BlockedWeight     *float64 `mapstructure:"blocked_weight"     toml:"blocked_weight,omitempty"`
	TagsWeight        *float64 `mapstructure:"tags_weight"        toml:"tags_weight,omitempty"`
	ProjectWeight     *float64 `mapstructure:"project_weight"     toml:"project_weight,omitempty"`
	AnnotationsWeight *float64 `mapstructure:"annotations_weight" toml:"annotations_weight,omitempty"`
	WaitingWeight     *float64 `mapstructure:"waiting_weight"     toml:"waiting_weight,omitempty"`
}

type ProjectSettingsConfig struct {
	AutoCompleteParent *AutoCompleteParentConfig `mapstructure:"auto_complete_parent" toml:"auto_complete_parent,omitempty"`
	AutoRevertParent   *AutoRevertParentConfig   `mapstructure:"auto_revert_parent"   toml:"auto_revert_parent,omitempty"`
	Urgency            *ProjectUrgencyConfig     `mapstructure:"urgency"              toml:"urgency,omitempty"`
}

type ProjectConfig struct {
	Workflow string                `mapstructure:"workflow" toml:"workflow"`
	Settings ProjectSettingsConfig `mapstructure:"settings" toml:"settings"`
}

type Config struct {
	Storage   StorageConfig             `mapstructure:"storage"   toml:"storage"`
	Urgency   UrgencyConfig             `mapstructure:"urgency"   toml:"urgency"`
	TUI       TUIConfig                 `mapstructure:"tui"       toml:"tui"`
	MCP       MCPConfig                 `mapstructure:"mcp"       toml:"mcp"`
	Workflows map[string]WorkflowConfig `mapstructure:"workflows" toml:"workflows"`
	Projects  map[string]ProjectConfig  `mapstructure:"projects"  toml:"projects"`
}

type StorageConfig struct {
	Backend  string         `mapstructure:"backend"  toml:"backend"`
	Path     string         `mapstructure:"path"     toml:"path"`
	Postgres PostgresConfig `mapstructure:"postgres" toml:"postgres"`
}

type PostgresConfig struct {
	DSN string `mapstructure:"dsn" toml:"dsn"`
}

type UrgencyConfig struct {
	PriorityWeight    float64 `mapstructure:"priority_weight"    toml:"priority_weight"`
	DueWeight         float64 `mapstructure:"due_weight"         toml:"due_weight"`
	AgeWeight         float64 `mapstructure:"age_weight"         toml:"age_weight"`
	ActiveWeight      float64 `mapstructure:"active_weight"      toml:"active_weight"`
	BlockingWeight    float64 `mapstructure:"blocking_weight"    toml:"blocking_weight"`
	BlockedWeight     float64 `mapstructure:"blocked_weight"     toml:"blocked_weight"`
	TagsWeight        float64 `mapstructure:"tags_weight"        toml:"tags_weight"`
	ProjectWeight     float64 `mapstructure:"project_weight"     toml:"project_weight"`
	AnnotationsWeight float64 `mapstructure:"annotations_weight" toml:"annotations_weight"`
	WaitingWeight     float64 `mapstructure:"waiting_weight"     toml:"waiting_weight"`
}

type MCPConfig struct {
	DisabledToolGroups     []string `mapstructure:"disabled_tool_groups"     toml:"disabled_tool_groups"`
	DisabledTools          []string `mapstructure:"disabled_tools"           toml:"disabled_tools"`
	DisabledResourceGroups []string `mapstructure:"disabled_resource_groups" toml:"disabled_resource_groups"`
	DisabledResources      []string `mapstructure:"disabled_resources"       toml:"disabled_resources"`
}

type TUIConfig struct {
	DateFormat  string `mapstructure:"date_format"  toml:"date_format"`
	Color       bool   `mapstructure:"color"        toml:"color"`
	TreeIndent  int    `mapstructure:"tree_indent"  toml:"tree_indent"`
	DefaultSort string `mapstructure:"default_sort" toml:"default_sort"`
}
```

- [ ] **Step 2: Run existing tests to verify no regressions**

Run: `go test ./config/ -v`

Expected: All existing tests pass. Adding struct tags is additive and doesn't affect Viper-based loading.

- [ ] **Step 3: Commit**

```bash
git add config/config.go
git commit -m "feat(config): add toml struct tags to all config types"
```

---

### Task 2: Promote `pelletier/go-toml/v2` to direct dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add direct import and tidy**

Run:
```bash
cd /Users/germanamz/projects/tusk && go get github.com/pelletier/go-toml/v2@v2.2.4 && go mod tidy
```

Expected: `go.mod` moves `pelletier/go-toml/v2` from `// indirect` to the direct `require` block.

- [ ] **Step 2: Verify**

Run: `grep 'pelletier/go-toml' go.mod`

Expected: Line without `// indirect` comment.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: promote pelletier/go-toml/v2 to direct dependency"
```

---

### Task 3: Implement `ConfigFilePath()`

**Files:**
- Create: `config/write.go`
- Create: `config/write_test.go`

- [ ] **Step 1: Write tests for `ConfigFilePath`**

Create `config/write_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./config/ -run TestConfigFilePath -v`

Expected: FAIL — `ConfigFilePath` not defined.

- [ ] **Step 3: Implement `ConfigFilePath`**

Create `config/write.go`:

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigFilePath resolves the config file path using the same logic as Load():
// custom path option > TUSK_CONFIG_DIR env > ~/.config/tusk/config.toml.
// Returns the path regardless of whether the file exists.
func ConfigFilePath(opts ...Option) (string, error) {
	var lo loadOptions
	for _, opt := range opts {
		opt(&lo)
	}

	var searchPath string
	switch {
	case lo.searchPath != "":
		searchPath = lo.searchPath
	case os.Getenv("TUSK_CONFIG_DIR") != "":
		searchPath = os.Getenv("TUSK_CONFIG_DIR")
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		searchPath = filepath.Join(home, ".config", "tusk")
	}

	return filepath.Join(searchPath, "config.toml"), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./config/ -run TestConfigFilePath -v`

Expected: All 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add config/write.go config/write_test.go
git commit -m "feat(config): add ConfigFilePath for resolving config file location"
```

---

### Task 4: Implement `LoadFile()` and `WriteConfig()`

**Files:**
- Modify: `config/write.go`
- Modify: `config/write_test.go`

- [ ] **Step 1: Write tests for `LoadFile` and `WriteConfig`**

Append to `config/write_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./config/ -run "TestWriteConfig|TestLoadFile" -v`

Expected: FAIL — `WriteConfig` and `LoadFile` not defined.

- [ ] **Step 3: Implement `LoadFile` and `WriteConfig`**

Add to `config/write.go` (update imports and add the two functions):

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// ConfigFilePath resolves the config file path using the same logic as Load():
// custom path option > TUSK_CONFIG_DIR env > ~/.config/tusk/config.toml.
// Returns the path regardless of whether the file exists.
func ConfigFilePath(opts ...Option) (string, error) {
	var lo loadOptions
	for _, opt := range opts {
		opt(&lo)
	}

	var searchPath string
	switch {
	case lo.searchPath != "":
		searchPath = lo.searchPath
	case os.Getenv("TUSK_CONFIG_DIR") != "":
		searchPath = os.Getenv("TUSK_CONFIG_DIR")
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		searchPath = filepath.Join(home, ".config", "tusk")
	}

	return filepath.Join(searchPath, "config.toml"), nil
}

// LoadFile parses a single TOML config file into a Config struct.
// Unlike Load(), this uses go-toml directly — no Viper, no env merging, no defaults.
// Used by config set (load-modify-write) and config validate (file-only validation).
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return &cfg, nil
}

// WriteConfig marshals a Config struct to TOML and writes it to path atomically.
// Writes to a temporary file first, then renames to avoid partial writes.
func WriteConfig(cfg *Config, path string) error {
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "tusk-config-*.toml")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./config/ -run "TestWriteConfig|TestLoadFile|TestConfigFilePath" -v`

Expected: All tests PASS.

- [ ] **Step 5: Run full config test suite for regressions**

Run: `go test ./config/ -v`

Expected: All tests PASS (existing Viper-based tests unaffected).

- [ ] **Step 6: Commit**

```bash
git add config/write.go config/write_test.go
git commit -m "feat(config): add LoadFile and WriteConfig for TOML read/write"
```

---

### Task 5: Implement valid key detection helper

**Files:**
- Modify: `config/write.go`
- Modify: `config/write_test.go`

This helper walks the Config struct's `mapstructure` tags to build a set of valid dot-paths. Used by `config set` and `config get` to reject unknown keys. Map-keyed paths (like `workflows.<name>.statuses`) need special handling — the helper accepts any map key in those positions.

- [ ] **Step 1: Write tests for `ValidKeys` and `IsValidKey`**

Append to `config/write_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./config/ -run TestIsValidKey -v`

Expected: FAIL — `IsValidKey` not defined.

- [ ] **Step 3: Implement `IsValidKey`**

Add to `config/write.go`:

```go
import (
	"reflect"
	"strings"
	// ... existing imports
)

// IsValidKey checks whether a dot-path key corresponds to a leaf field in the Config struct.
// For map-keyed sections (workflows, projects), any map key is accepted.
func IsValidKey(key string) bool {
	if key == "" {
		return false
	}
	parts := strings.Split(key, ".")
	return isValidKeyPath(reflect.TypeOf(Config{}), parts)
}

// isValidKeyPath recursively walks the struct type tree to validate a dot-path.
func isValidKeyPath(t reflect.Type, parts []string) bool {
	if len(parts) == 0 {
		return false
	}

	// Unwrap pointer types.
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Struct:
		// Find the field matching the mapstructure tag.
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("mapstructure")
			if tag == parts[0] {
				if len(parts) == 1 {
					// Valid only if this is a leaf (not a struct, or is a slice/basic type).
					ft := f.Type
					for ft.Kind() == reflect.Ptr {
						ft = ft.Elem()
					}
					return ft.Kind() != reflect.Struct
				}
				return isValidKeyPath(f.Type, parts[1:])
			}
		}
		return false

	case reflect.Map:
		// Map key can be anything (e.g., workflows.<anyname>).
		// Continue validating the value type with remaining parts.
		if len(parts) == 1 {
			return false // map itself is not a leaf
		}
		return isValidKeyPath(t.Elem(), parts[1:])

	default:
		// Leaf type — valid only if no more parts.
		return len(parts) == 0
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./config/ -run TestIsValidKey -v`

Expected: All tests PASS.

- [ ] **Step 5: Run full test suite**

Run: `go test ./config/ -v`

Expected: All tests PASS.

- [ ] **Step 6: Commit**

```bash
git add config/write.go config/write_test.go
git commit -m "feat(config): add IsValidKey for dot-path validation"
```

---

## Changes Introduced

**New files:**
- `config/write.go` — `ConfigFilePath()`, `LoadFile()`, `WriteConfig()`, `IsValidKey()`, `isValidKeyPath()`
- `config/write_test.go` — unit tests for all of the above

**Modified files:**
- `config/config.go` — `toml` struct tags added to all 13 config types (additive, no behavioral change)
- `go.mod` / `go.sum` — `pelletier/go-toml/v2` promoted from indirect to direct

**New dependency:**
- `pelletier/go-toml/v2` v2.2.4 (was already indirect via Viper)

**No bridge code introduced.** All functions are complete and usable. Phase 2 consumes them from the CLI layer.

**User-visible behavior preserved:** No existing commands changed. All existing tests pass. The config read path (`Load()`) is untouched.
