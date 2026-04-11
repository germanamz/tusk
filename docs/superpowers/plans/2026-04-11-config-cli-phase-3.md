# Config CLI — Phase 3: `config set` Command and E2E Tests

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `config set` mutation command and add E2E tests covering all config CLI commands.

**Architecture:** `config set` loads the on-disk file via `LoadFile`, applies the change through a temporary Viper instance for dot-path resolution, validates, and writes back via `WriteConfig`. E2E tests exercise all config commands through the compiled binary.

**Tech Stack:** Go, Cobra, Viper, `pelletier/go-toml/v2`, existing test harness

**Prerequisites:** Phase 1 and Phase 2 must be completed.

**Design spec:** `docs/superpowers/specs/2026-04-11-config-cli-design.md`

---

## Inherits From

**Phase 1** added:
- `config/write.go` with `ConfigFilePath()`, `LoadFile()`, `WriteConfig()`, `IsValidKey()`
- TOML struct tags on all config types

**Phase 2** added:
- `internal/tui/config.go` with `buildConfigCmd()` and handlers for `show`, `get`, `path`, `init`, `validate`, `edit`
- `loadOpts []config.Option` field on `App` struct
- `buildConfigViper()` helper for loading effective config into a Viper instance
- `New()` signature updated with `loadOpts` parameter
- Config command group registered in `app.go`

---

### Task 1: Implement `config set` command

**Files:**
- Modify: `internal/tui/config.go`

- [ ] **Step 1: Add `set` subcommand to `buildConfigCmd`**

In `internal/tui/config.go`, add this command to the `configCmd.AddCommand(...)` call in `buildConfigCmd`:

```go
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Set a config value and write to file",
			Args:  cobra.ExactArgs(2),
			RunE:  a.runConfigSet,
		},
```

- [ ] **Step 2: Implement `runConfigSet`**

Add to `internal/tui/config.go`. This needs the `strings` package added to imports:

```go
import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/config"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)
```

Add the handler:

```go
func (a *App) runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]

	if !config.IsValidKey(key) {
		return fmt.Errorf("unknown config key: %q", key)
	}

	// Resolve config file path.
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}

	// Reject if no config file exists.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("no config file found; run \"tusk config init\" to create one")
	}

	// Load the file contents (no defaults, no env).
	fileCfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}

	// Marshal to TOML, load into Viper for dot-path Set().
	data, err := toml.Marshal(fileCfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("reading config into viper: %w", err)
	}

	// Determine if this is a slice field and parse accordingly.
	var parsedValue any
	if config.IsSliceKey(key) {
		parsedValue = strings.Split(value, ",")
	} else {
		parsedValue = value
	}

	v.Set(key, parsedValue)

	// Unmarshal back to Config.
	var newCfg config.Config
	if err := v.Unmarshal(&newCfg); err != nil {
		return fmt.Errorf("applying config change: %w", err)
	}

	// Validate before writing.
	if err := newCfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	return config.WriteConfig(&newCfg, path)
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`

Expected: FAIL — `config.IsSliceKey` does not exist yet.

- [ ] **Step 4: Implement `IsSliceKey` in `config/write.go`**

Add to `config/write.go`:

```go
// IsSliceKey checks whether a dot-path key corresponds to a slice field in the Config struct.
func IsSliceKey(key string) bool {
	if key == "" {
		return false
	}
	parts := strings.Split(key, ".")
	return isSliceKeyPath(reflect.TypeOf(Config{}), parts)
}

func isSliceKeyPath(t reflect.Type, parts []string) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if len(parts) == 0 {
		return false
	}

	switch t.Kind() {
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("mapstructure")
			if tag == parts[0] {
				if len(parts) == 1 {
					ft := f.Type
					for ft.Kind() == reflect.Ptr {
						ft = ft.Elem()
					}
					return ft.Kind() == reflect.Slice
				}
				return isSliceKeyPath(f.Type, parts[1:])
			}
		}
		return false
	case reflect.Map:
		if len(parts) < 2 {
			return false
		}
		return isSliceKeyPath(t.Elem(), parts[1:])
	default:
		return false
	}
}
```

- [ ] **Step 5: Add test for `IsSliceKey` in `config/write_test.go`**

Append to `config/write_test.go`:

```go
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
```

- [ ] **Step 6: Verify compilation and run tests**

Run: `go build ./... && go test ./config/ -run TestIsSliceKey -v`

Expected: Compiles and all tests pass.

- [ ] **Step 7: Manual smoke test**

Run:
```bash
go run ./cmd/tusk config set tui.color false && go run ./cmd/tusk config get tui.color
```

Expected: Prints `false`.

Run:
```bash
go run ./cmd/tusk config set tui.color true
```

(Reset to original value.)

- [ ] **Step 8: Commit**

```bash
git add internal/tui/config.go config/write.go config/write_test.go
git commit -m "feat(tui): add config set command for config mutation"
```

---

### Task 2: E2E tests for config init, path, show

**Files:**
- Modify: `tests/e2e/config_test.go`

These tests follow the existing pattern in `config_test.go`: create a temp HOME, run the binary with `envWithHome()`, and check output.

- [ ] **Step 1: Add E2E test for `config init`**

Add to `tests/e2e/config_test.go`:

```go
func TestCLI_ConfigInit(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, ".config", "tusk", "config.toml")

	// First run: should create the file.
	cmd := exec.Command(binPath, "config", "init")
	cmd.Env = envWithHome(homeDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config init failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "Created") {
		t.Errorf("expected 'Created' in output, got: %s", out)
	}

	// Verify file exists.
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Second run: should report already exists.
	cmd = exec.Command(binPath, "config", "init")
	cmd.Env = envWithHome(homeDir)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config init (second run) failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "already exists") {
		t.Errorf("expected 'already exists' in output, got: %s", out)
	}
}
```

- [ ] **Step 2: Add E2E test for `config path`**

Add to `tests/e2e/config_test.go`:

```go
func TestCLI_ConfigPath(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	cmd := exec.Command(binPath, "config", "path")
	cmd.Env = envWithHome(homeDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config path failed: %v\noutput: %s", err, out)
	}
	expected := filepath.Join(homeDir, ".config", "tusk", "config.toml")
	if strings.TrimSpace(string(out)) != expected {
		t.Errorf("got %q, want %q", strings.TrimSpace(string(out)), expected)
	}
}
```

- [ ] **Step 3: Add E2E test for `config show`**

Add to `tests/e2e/config_test.go`:

```go
func TestCLI_ConfigShow(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	// Config init first to create the file (config show loads effective config so it will work either way,
	// but init ensures a consistent HOME).
	initCmd := exec.Command(binPath, "config", "init")
	initCmd.Env = envWithHome(homeDir)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("config init failed: %v\noutput: %s", err, out)
	}

	cmd := exec.Command(binPath, "config", "show")
	cmd.Env = envWithHome(homeDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config show failed: %v\noutput: %s", err, out)
	}

	output := string(out)
	// Should contain key TOML sections.
	if !strings.Contains(output, "[storage]") {
		t.Error("config show output missing [storage] section")
	}
	if !strings.Contains(output, "[urgency]") {
		t.Error("config show output missing [urgency] section")
	}
	if !strings.Contains(output, "[tui]") {
		t.Error("config show output missing [tui] section")
	}
}
```

- [ ] **Step 4: Build and run E2E tests**

Run:
```bash
make build && go test ./tests/e2e/ -run "TestCLI_Config(Init|Path|Show)" -v
```

Expected: All 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/config_test.go
git commit -m "test(e2e): add tests for config init, path, and show commands"
```

---

### Task 3: E2E tests for config get, set, and validate

**Files:**
- Modify: `tests/e2e/config_test.go`

- [ ] **Step 1: Add E2E test for `config get`**

Add to `tests/e2e/config_test.go`:

```go
func TestCLI_ConfigGet(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	// Init config first.
	initCmd := exec.Command(binPath, "config", "init")
	initCmd.Env = envWithHome(homeDir)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("config init: %v\n%s", err, out)
	}

	t.Run("scalar_bool", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "get", "tui.color")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		if strings.TrimSpace(string(out)) != "true" {
			t.Errorf("got %q, want %q", strings.TrimSpace(string(out)), "true")
		}
	})

	t.Run("scalar_float", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "get", "urgency.due_weight")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		if strings.TrimSpace(string(out)) != "12" {
			t.Errorf("got %q, want %q", strings.TrimSpace(string(out)), "12")
		}
	})

	t.Run("complex_value", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "get", "workflows.kanban.statuses")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		// Should be JSON array.
		var arr []string
		if err := json.Unmarshal(out, &arr); err != nil {
			t.Fatalf("expected JSON array, got: %s", out)
		}
		if len(arr) != 4 {
			t.Errorf("expected 4 statuses, got %d", len(arr))
		}
	})

	t.Run("unknown_key", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "get", "nonexistent.key")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected error for unknown key")
		}
		if !strings.Contains(string(out), "unknown config key") {
			t.Errorf("expected 'unknown config key' error, got: %s", out)
		}
	})
}
```

- [ ] **Step 2: Add E2E test for `config set`**

Add to `tests/e2e/config_test.go`:

```go
func TestCLI_ConfigSet(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	env := envWithHome(homeDir)

	// Init config first.
	initCmd := exec.Command(binPath, "config", "init")
	initCmd.Env = env
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("config init: %v\n%s", err, out)
	}

	t.Run("set_scalar", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "set", "tui.color", "false")
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("config set: %v\n%s", err, out)
		}

		// Verify the change persisted.
		getCmd := exec.Command(binPath, "config", "get", "tui.color")
		getCmd.Env = env
		out, err := getCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		if strings.TrimSpace(string(out)) != "false" {
			t.Errorf("got %q, want %q", strings.TrimSpace(string(out)), "false")
		}
	})

	t.Run("set_list", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "set", "mcp.disabled_tools", "tusk_task_delete,tusk_task_tree")
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("config set: %v\n%s", err, out)
		}

		// Verify.
		getCmd := exec.Command(binPath, "config", "get", "mcp.disabled_tools")
		getCmd.Env = env
		out, err := getCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		var arr []string
		if err := json.Unmarshal(out, &arr); err != nil {
			t.Fatalf("expected JSON array, got: %s", out)
		}
		if len(arr) != 2 || arr[0] != "tusk_task_delete" || arr[1] != "tusk_task_tree" {
			t.Errorf("unexpected disabled_tools: %v", arr)
		}
	})

	t.Run("reject_unknown_key", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "set", "nonexistent.key", "value")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected error for unknown key")
		}
		if !strings.Contains(string(out), "unknown config key") {
			t.Errorf("expected 'unknown config key' error, got: %s", out)
		}
	})

	t.Run("reject_invalid_config", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "set", "projects.default.workflow", "nonexistent_workflow")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected error for invalid config")
		}
		if !strings.Contains(string(out), "unknown workflow") {
			t.Errorf("expected workflow validation error, got: %s", out)
		}
	})

	t.Run("works_with_auto_created_config", func(t *testing.T) {
		// Even with a fresh HOME (no pre-existing config), config set works
		// because Load() in main.go auto-creates the file via ensureConfigFile().
		freshHome := t.TempDir()
		cmd := exec.Command(binPath, "config", "set", "tui.color", "false")
		cmd.Env = envWithHome(freshHome)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("config set with fresh home: %v\n%s", err, out)
		}

		// Verify it persisted.
		getCmd := exec.Command(binPath, "config", "get", "tui.color")
		getCmd.Env = envWithHome(freshHome)
		out, err := getCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		if strings.TrimSpace(string(out)) != "false" {
			t.Errorf("got %q, want %q", strings.TrimSpace(string(out)), "false")
		}
	})
}
```

- [ ] **Step 3: Add E2E test for `config validate`**

Add to `tests/e2e/config_test.go`:

```go
func TestCLI_ConfigValidate(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	t.Run("valid_config", func(t *testing.T) {
		homeDir := t.TempDir()
		// Init config (writes valid defaults).
		initCmd := exec.Command(binPath, "config", "init")
		initCmd.Env = envWithHome(homeDir)
		if out, err := initCmd.CombinedOutput(); err != nil {
			t.Fatalf("config init: %v\n%s", err, out)
		}

		cmd := exec.Command(binPath, "config", "validate")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config validate: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "Config valid") {
			t.Errorf("expected 'Config valid', got: %s", out)
		}
	})

	t.Run("invalid_config", func(t *testing.T) {
		homeDir := t.TempDir()
		tuskConfigDir := filepath.Join(homeDir, ".config", "tusk")
		if err := os.MkdirAll(tuskConfigDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Write config with project referencing nonexistent workflow.
		badConfig := []byte("[projects.default]\nworkflow = \"nonexistent\"\n")
		if err := os.WriteFile(filepath.Join(tuskConfigDir, "config.toml"), badConfig, 0o644); err != nil {
			t.Fatal(err)
		}

		cmd := exec.Command(binPath, "config", "validate")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected error for invalid config")
		}
		if !strings.Contains(string(out), "unknown workflow") {
			t.Errorf("expected workflow validation error, got: %s", out)
		}
	})
}
```

- [ ] **Step 4: Ensure `json` and `encoding/json` imports in test file**

The test file needs `encoding/json` for the `config get` complex value test. Check the imports at the top of `tests/e2e/config_test.go` and add `encoding/json` if not already present. The existing file already imports `encoding/json`, `os`, `os/exec`, `path/filepath`, `strings`, and `testing`.

- [ ] **Step 5: Build and run all config E2E tests**

Run:
```bash
make build && go test ./tests/e2e/ -run "TestCLI_Config" -v
```

Expected: All config E2E tests PASS.

- [ ] **Step 6: Run full test suite for regressions**

Run: `make test`

Expected: All tests PASS.

- [ ] **Step 7: Commit**

```bash
git add tests/e2e/config_test.go
git commit -m "test(e2e): add tests for config get, set, and validate commands"
```

---

## Changes Introduced

**New files:** None

**Modified files:**
- `internal/tui/config.go` — `config set` subcommand added to `buildConfigCmd()`, `runConfigSet()` handler implemented
- `config/write.go` — `IsSliceKey()` and `isSliceKeyPath()` added
- `config/write_test.go` — `TestIsSliceKey` added
- `tests/e2e/config_test.go` — E2E tests for `config init`, `config path`, `config show`, `config get`, `config set`, `config validate`

**No bridge code introduced.** All config CLI commands are complete and tested.

**User-visible behavior preserved:** All existing commands unchanged. All prior tests pass. New `tusk config set` command available.
