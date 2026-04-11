# Config CLI — Phase 2: CLI Commands (show, get, path, init, validate)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the read-only config CLI commands (`show`, `get`, `path`, `init`, `validate`) and wire the config command group into the App.

**Architecture:** New `buildConfigCmd()` on `App` following existing subcommand patterns (`buildWorkflowCmd`, `buildTagCmd`). The `App` struct gains `loadOpts []config.Option` to pass config path resolution into commands. Read-only commands use `config.Load()` for effective config and `config.LoadFile()` / `config.ConfigFilePath()` from Phase 1 for file-specific operations.

**Tech Stack:** Go, Cobra, Viper, `pelletier/go-toml/v2`, existing `config` package

**Prerequisites:** Phase 1 must be completed. Phase 1 introduced `config.ConfigFilePath()`, `config.LoadFile()`, `config.WriteConfig()`, `config.IsValidKey()`, and TOML struct tags on all config types.

**Design spec:** `docs/superpowers/specs/2026-04-11-config-cli-design.md`

---

## Inherits From

**Phase 1** added:
- `config/write.go` with `ConfigFilePath(opts ...Option) (string, error)`, `LoadFile(path string) (*Config, error)`, `WriteConfig(cfg *Config, path string) error`, `IsValidKey(key string) bool`
- `toml` struct tags on all config types in `config/config.go`
- `pelletier/go-toml/v2` as a direct dependency

---

### Task 1: Wire `loadOpts` into App struct and constructor

**Files:**
- Modify: `internal/tui/app.go:23-38` (App struct)
- Modify: `internal/tui/app.go:54` (New function signature)
- Modify: `cmd/tusk/main.go:86-90` (App construction call)

- [ ] **Step 1: Add `loadOpts` field to App struct**

In `internal/tui/app.go`, add `loadOpts` to the struct (after line 37, before the closing brace):

```go
type App struct {
	taskSvc     *service.TaskService
	tagSvc      *service.TagService
	relationSvc *service.RelationService
	projectSvc  *service.ProjectService
	workflowSvc *service.WorkflowService
	playerSvc   *service.PlayerService
	playerID    string // from --player flag
	resolver    *filter.Resolver
	root        *cobra.Command
	format      string
	noColor     bool
	version     VersionInfo
	tuiCfg      config.TUIConfig
	mcpCfg      config.MCPConfig
	loadOpts    []config.Option
}
```

- [ ] **Step 2: Update `New()` signature and body to accept and store `loadOpts`**

Change the `New` function signature in `internal/tui/app.go` (line 54):

```go
func New(taskSvc *service.TaskService, tagSvc *service.TagService, relationSvc *service.RelationService, projectSvc *service.ProjectService, workflowSvc *service.WorkflowService, playerSvc *service.PlayerService, vi VersionInfo, tuiCfg config.TUIConfig, mcpCfg config.MCPConfig, loadOpts []config.Option) *App {
```

Add `loadOpts` to the struct initialization (after `mcpCfg: mcpCfg,`):

```go
	a := &App{
		taskSvc:     taskSvc,
		tagSvc:      tagSvc,
		relationSvc: relationSvc,
		projectSvc:  projectSvc,
		workflowSvc: workflowSvc,
		playerSvc:   playerSvc,
		version:     vi,
		tuiCfg:      tuiCfg,
		mcpCfg:      mcpCfg,
		loadOpts:    loadOpts,
	}
```

- [ ] **Step 3: Update the `tui.New()` call in `cmd/tusk/main.go`**

In `cmd/tusk/main.go`, the call to `tui.New()` (around line 86) currently ends with `cfg.TUI, cfg.MCP)`. Add `nil` for loadOpts since no custom options are used in the default run path:

```go
	app := tui.New(taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, playerSvc, tui.VersionInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}, cfg.TUI, cfg.MCP, nil)
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./...`

Expected: Compiles successfully.

- [ ] **Step 5: Run full test suite to check for regressions**

Run: `make test`

Expected: All tests pass. The extra `nil` parameter doesn't change behavior.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go cmd/tusk/main.go
git commit -m "feat(tui): add loadOpts to App for config path resolution"
```

---

### Task 2: Implement `buildConfigCmd` with `show`, `path`, and `init`

**Files:**
- Create: `internal/tui/config.go`

- [ ] **Step 1: Create `internal/tui/config.go` with command group and three subcommands**

```go
package tui

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/config"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

// buildConfigCmd creates the `tusk config` command group.
func (a *App) buildConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	configCmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Display current effective configuration",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigShow,
		},
		&cobra.Command{
			Use:   "path",
			Short: "Print resolved config file path",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigPath,
		},
		&cobra.Command{
			Use:   "init",
			Short: "Create config file with defaults if none exists",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigInit,
		},
	)

	return configCmd
}

func (a *App) runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	_, err = cmd.OutOrStdout().Write(data)
	return err
}

func (a *App) runConfigPath(cmd *cobra.Command, args []string) error {
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
	return err
}

func (a *App) runConfigInit(cmd *cobra.Command, args []string) error {
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Config file already exists: %s\n", path)
		return err
	}

	// Create directory and write defaults.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Load embedded defaults and write them.
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading defaults: %w", err)
	}
	if err := config.WriteConfig(cfg, path); err != nil {
		return err
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", path)
	return err
}
```

Wait — `runConfigInit` needs `filepath` imported. Update the import block:

```go
import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/config"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)
```

- [ ] **Step 2: Register the config command in `app.go`**

In `internal/tui/app.go`, add this line after `a.root.AddCommand(a.buildPlayerCmd())` (line 85):

```go
	a.root.AddCommand(a.buildConfigCmd())
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`

Expected: Compiles successfully.

- [ ] **Step 4: Manual smoke test**

Run: `go run ./cmd/tusk config path`

Expected: Prints a path like `/Users/<you>/.config/tusk/config.toml`.

Run: `go run ./cmd/tusk config show`

Expected: Prints the full effective config as TOML.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/config.go internal/tui/app.go
git commit -m "feat(tui): add config show, path, and init commands"
```

---

### Task 3: Implement `config get`

**Files:**
- Modify: `internal/tui/config.go`

- [ ] **Step 1: Add `config get` subcommand to `buildConfigCmd`**

In `internal/tui/config.go`, add this command to the `configCmd.AddCommand(...)` call:

```go
		&cobra.Command{
			Use:   "get <key>",
			Short: "Get a specific config value by dot-path key",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runConfigGet,
		},
```

- [ ] **Step 2: Implement `runConfigGet`**

Add to `internal/tui/config.go`. This needs Viper to do the merged-config get with dot-path resolution. Import Viper and add the handler:

Update imports:

```go
import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/config"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)
```

Add the handler:

```go
func (a *App) runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	if !config.IsValidKey(key) {
		return fmt.Errorf("unknown config key: %q", key)
	}

	// Build a Viper instance with the same config as Load() to get dot-path resolution.
	v, err := a.buildConfigViper()
	if err != nil {
		return err
	}

	val := v.Get(key)
	if val == nil {
		return fmt.Errorf("unknown config key: %q", key)
	}

	// Determine output format.
	switch v := val.(type) {
	case string, bool, int, int64, float64:
		if a.format == "json" {
			enc := json.NewEncoder(cmd.OutOrStdout())
			return enc.Encode(val)
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), v)
		return err
	default:
		// Complex value — always JSON.
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(val)
	}
}

// buildConfigViper creates a Viper instance mirroring the Load() setup for dot-path access.
func (a *App) buildConfigViper() (*viper.Viper, error) {
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}

	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("reading config into viper: %w", err)
	}

	return v, nil
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`

Expected: Compiles successfully.

- [ ] **Step 4: Manual smoke test**

Run: `go run ./cmd/tusk config get tui.color`

Expected: Prints `true`.

Run: `go run ./cmd/tusk config get urgency.due_weight`

Expected: Prints `12`.

Run: `go run ./cmd/tusk config get workflows.kanban.statuses`

Expected: Prints JSON array `["pending", "active", "completed", "deleted"]`.

Run: `go run ./cmd/tusk config get nonexistent.key`

Expected: Error `unknown config key: "nonexistent.key"`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/config.go
git commit -m "feat(tui): add config get command with dot-path key lookup"
```

---

### Task 4: Implement `config validate` and `config edit`

**Files:**
- Modify: `internal/tui/config.go`

- [ ] **Step 1: Add `validate` and `edit` subcommands to `buildConfigCmd`**

Add these commands to the `configCmd.AddCommand(...)` call in `buildConfigCmd`:

```go
		&cobra.Command{
			Use:   "validate",
			Short: "Validate config file for errors",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigValidate,
		},
		&cobra.Command{
			Use:   "edit",
			Short: "Open config file in $EDITOR",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigEdit,
		},
```

- [ ] **Step 2: Implement `runConfigValidate`**

Add to `internal/tui/config.go`:

```go
func (a *App) runConfigValidate(cmd *cobra.Command, args []string) error {
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}

	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), "Config valid")
	return err
}
```

- [ ] **Step 3: Implement `runConfigEdit`**

Add the necessary import for `os/exec` to the import block:

```go
import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/germanamz/tusk/config"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)
```

Add the handler:

```go
func (a *App) runConfigEdit(cmd *cobra.Command, args []string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("$EDITOR is not set")
	}

	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}

	c := exec.Command(editor, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./...`

Expected: Compiles successfully.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/config.go
git commit -m "feat(tui): add config validate and config edit commands"
```

---

## Changes Introduced

**New files:**
- `internal/tui/config.go` — `buildConfigCmd()`, `runConfigShow()`, `runConfigPath()`, `runConfigInit()`, `runConfigGet()`, `runConfigValidate()`, `runConfigEdit()`, `buildConfigViper()`

**Modified files:**
- `internal/tui/app.go` — `loadOpts` field added to `App`, `New()` signature updated, `a.root.AddCommand(a.buildConfigCmd())` added
- `cmd/tusk/main.go` — `tui.New()` call updated with `nil` loadOpts parameter

**No bridge code introduced.** All commands are complete. `config set` is deferred to Phase 3.

**User-visible behavior preserved:** All existing commands unchanged. New `tusk config` command group added with `show`, `get`, `path`, `init`, `validate`, `edit` subcommands.
