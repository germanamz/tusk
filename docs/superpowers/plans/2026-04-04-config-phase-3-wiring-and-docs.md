# Configuration System — Phase 3: DI Wiring & Documentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the config system into `main.go` and the TUI app, update DB path resolution to use config, and write the user-facing configuration reference documentation.

**Architecture:** `main.go` calls `config.Load()` first, then passes sub-structs to MCP server and TUI app. The DB path resolution chain becomes: `--db` flag > `TUSK_DB` env > `config.Storage.Path` > hardcoded default. The TUI `App` struct gains a `config.TUIConfig` field for future use.

**Tech Stack:** Go, Cobra, Viper

**Design Spec:** `docs/superpowers/specs/2026-04-04-configuration-system-design.md`

**Depends on:** Phase 1 (config package), Phase 2 (MCP visibility filtering)

---

### Task 1: Wire config into main.go

**Files:**
- Modify: `cmd/tusk/main.go`

Currently `main.go` calls `resolveDBPath()` which checks `--db` flag, `TUSK_DB` env, and a hardcoded default (lines 90-118). We need to insert `config.Load()` before this and add the config value as a new fallback in the chain.

- [ ] **Step 1: Write the updated run function**

Update the `run()` function in `cmd/tusk/main.go` to load config first. The full function becomes:

```go
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	dbPath, err := resolveDBPath(cfg.Storage.Path)
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating data directory %s: %w", dir, err)
	}

	store, err := sqlite.New(dbPath, migrations.FS)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer store.Close()

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)

	tagRepo := sqlite.NewTagRepo(db)
	relationRepo := sqlite.NewRelationRepo(db)

	workflowSvc := service.NewWorkflowService(workflowRepo)
	taskSvc := service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)
	tagSvc := service.NewTagService(tagRepo)
	relationSvc := service.NewRelationService(relationRepo, taskRepo, store)

	projectSvc := service.NewProjectService(projectRepo)

	app := tui.New(taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, tui.VersionInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}, cfg.TUI, cfg.MCP)
	return app.Run(stripDBFlag(os.Args[1:]))
}
```

Add `"github.com/germanamz/tusk/internal/config"` to the import block.

- [ ] **Step 2: Update resolveDBPath to accept config path**

Replace the existing `resolveDBPath()` function with one that accepts the config-provided storage path as a fallback:

```go
// resolveDBPath returns the database path from: --db flag > TUSK_DB env > config value > default.
func resolveDBPath(configPath string) (string, error) {
	// 1. --db flag (highest priority)
	for i, arg := range os.Args {
		if arg == "--db" {
			if i+1 >= len(os.Args) {
				return "", fmt.Errorf("--db requires a value")
			}
			return os.Args[i+1], nil
		}
		if strings.HasPrefix(arg, "--db=") {
			val := arg[5:]
			if val == "" {
				return "", fmt.Errorf("--db requires a value")
			}
			return val, nil
		}
	}

	// 2. TUSK_DB environment variable
	if v := os.Getenv("TUSK_DB"); v != "" {
		return v, nil
	}

	// 3. Config file value (with tilde expansion)
	if configPath != "" {
		return config.ExpandPath(configPath), nil
	}

	// 4. Hardcoded default
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "share", "tusk", "tusk.db"), nil
}
```

Note: The config default for `storage.path` is `~/.local/share/tusk/tusk.db`, so step 3 and step 4 will produce the same result in practice. But this chain is correct — if a user changes `storage.path` in their config, it takes effect between the env var and the hardcoded default.

- [ ] **Step 3: Verify it compiles**

This step will fail until Task 2 updates the `tui.New` signature. For now, just verify the `main.go` changes are correct by checking syntax:

```bash
cd /Users/germanamz/projects/tusk && go vet ./cmd/tusk/
```

Expected: may fail due to `tui.New` signature mismatch — that's OK, we fix it in Task 2.

- [ ] **Step 4: Commit (will be part of a joint commit with Task 2)**

Hold this commit until Task 2 is done so the build stays green.

---

### Task 2: Wire config into TUI App

**Files:**
- Modify: `internal/tui/app.go` (lines 20-34 — App struct and New function)

The `App` struct and `New` function need to accept `TUIConfig` and `MCPConfig`. The TUIConfig is stored for future use by TUI commands. The MCPConfig is passed through to the MCP server constructor.

- [ ] **Step 1: Update the App struct**

In `internal/tui/app.go`, add `tuiCfg` and `mcpCfg` fields to the `App` struct:

```go
type App struct {
	taskSvc     *service.TaskService
	tagSvc      *service.TagService
	relationSvc *service.RelationService
	projectSvc  *service.ProjectService
	workflowSvc *service.WorkflowService
	resolver    *filter.Resolver
	root        *cobra.Command
	format      string
	version     VersionInfo
	tuiCfg      config.TUIConfig
	mcpCfg      config.MCPConfig
}
```

Add `"github.com/germanamz/tusk/internal/config"` to the import block (it should already be there from Phase 2).

- [ ] **Step 2: Update the New function signature**

Change the `New` function signature (line 34) to accept the two config sub-structs:

```go
func New(taskSvc *service.TaskService, tagSvc *service.TagService, relationSvc *service.RelationService, projectSvc *service.ProjectService, workflowSvc *service.WorkflowService, vi VersionInfo, tuiCfg config.TUIConfig, mcpCfg config.MCPConfig) *App {
	a := &App{
		taskSvc:     taskSvc,
		tagSvc:      tagSvc,
		relationSvc: relationSvc,
		projectSvc:  projectSvc,
		workflowSvc: workflowSvc,
		version:     vi,
		tuiCfg:      tuiCfg,
		mcpCfg:      mcpCfg,
	}
```

- [ ] **Step 3: Update the MCP serve command to pass config**

In the same file, the `mcp serve` command (around line 144-151) currently creates the MCP server. Update it to pass `a.mcpCfg`:

```go
mcpCmd.AddCommand(&cobra.Command{
    Use:   "serve",
    Short: "Start MCP server with stdio transport",
    RunE: func(cmd *cobra.Command, args []string) error {
        mcpServer := tuskmcp.New(taskSvc, tagSvc, relationSvc, projectSvc, a.workflowSvc, vi.Version, a.mcpCfg)
        return mcpServer.Serve()
    },
})
```

- [ ] **Step 4: Fix app_test.go and commands_test.go**

Check `internal/tui/app_test.go` and `internal/tui/commands_test.go` for calls to `tui.New(...)`. Each one needs the two new config parameters appended. Pass zero-value configs:

```go
// Before:
app := New(nil, nil, nil, nil, nil, VersionInfo{})

// After:
app := New(nil, nil, nil, nil, nil, VersionInfo{}, config.TUIConfig{}, config.MCPConfig{})
```

Search each test file for all occurrences of `New(` and update them.

Add the import `"github.com/germanamz/tusk/internal/config"` to the test files if not present.

- [ ] **Step 5: Build and run all tests**

```bash
go build ./cmd/tusk/ && go test ./internal/tui/ ./internal/mcp/ ./cmd/tusk/
```

Expected: all PASS, binary compiles.

- [ ] **Step 6: Run E2E tests**

```bash
go test ./tests/e2e/ -timeout 120s
```

Expected: all PASS. The E2E tests build the binary from source, so they'll pick up the config changes. Since no config file exists in the test environment, defaults are used — behavior is unchanged.

- [ ] **Step 7: Commit both tasks**

```bash
git add cmd/tusk/main.go internal/tui/app.go internal/tui/app_test.go internal/tui/commands_test.go
git commit -m "feat(config): wire config loading into main.go and TUI app"
```

---

### Task 3: E2E tests for config system

**Files:**
- Create: `tests/e2e/config_test.go`

- [ ] **Step 1: Write E2E test for config file with custom DB path**

Create `tests/e2e/config_test.go`:

```go
package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_WithConfigFile(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	// Create a temp directory for the config and a separate one for the DB.
	configDir := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "custom.db")

	// Write a config file that sets a custom DB path.
	configContent := []byte(`
[storage]
path = "` + dbPath + `"
`)
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), configContent, 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Run tusk with XDG_CONFIG_HOME pointing to our temp dir.
	// Tusk looks in ~/.config/tusk/, so we set HOME to a dir
	// where .config/tusk/ contains our config file.
	//
	// Actually, Viper looks at a hardcoded path. We need to set
	// the config search path. Since tusk uses ~/.config/tusk/,
	// we can set HOME to make ~ resolve to our temp dir.
	homeDir := t.TempDir()
	tuskConfigDir := filepath.Join(homeDir, ".config", "tusk")
	if err := os.MkdirAll(tuskConfigDir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tuskConfigDir, "config.toml"), configContent, 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Run tusk add (no --db flag, no TUSK_DB) with HOME overridden.
	cmd := newCmdWithHome(t, homeDir, "add", "Config test task")
	r := cmd.Run()
	if r.Err != nil {
		t.Fatalf("tusk add failed: %v\nstderr: %s", r.Err, r.Stderr)
	}

	// Verify the DB file was created at the custom path.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("expected DB at %s but it doesn't exist", dbPath)
	}
}

// cmdWithHome is a helper that runs tusk with a custom HOME and no --db/TUSK_DB.
type cmdWithHome struct {
	t       *testing.T
	home    string
	binPath string
	args    []string
}

func newCmdWithHome(t *testing.T, home string, args ...string) *cmdWithHome {
	return &cmdWithHome{t: t, home: home, binPath: binPath, args: args}
}

func (c *cmdWithHome) Run() Result {
	c.t.Helper()
	cmd := exec.Command(c.binPath, c.args...)

	// Build env without HOME, TUSK_DB, or XDG_ vars that could interfere.
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "TUSK_DB=") {
			continue
		}
		env = append(env, e)
	}
	env = append(env, "HOME="+c.home)
	cmd.Env = env

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return Result{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
}
```

Add `"os/exec"` to the imports.

- [ ] **Step 2: Run the test**

```bash
go test -v ./tests/e2e/ -run TestCLI_WithConfigFile -timeout 60s
```

Expected: PASS — tusk reads the config file and uses the custom DB path.

- [ ] **Step 3: Write E2E test for MCP disabled tools**

Add to `tests/e2e/config_test.go`:

```go
func TestMCP_DisabledTools(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	// Create config that disables the relation tool group.
	homeDir := t.TempDir()
	tuskConfigDir := filepath.Join(homeDir, ".config", "tusk")
	if err := os.MkdirAll(tuskConfigDir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	configContent := []byte(`
[mcp]
disabled_tool_groups = ["relation"]
`)
	if err := os.WriteFile(filepath.Join(tuskConfigDir, "config.toml"), configContent, 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Start MCP server with this config.
	env := newMCPEnvWithHome(t, binPath, homeDir)

	// List tools
	resp := env.send("tools/list", nil)
	if resp.Error != nil {
		t.Fatalf("tools/list error: %s", resp.Error)
	}

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("parsing tools/list: %v", err)
	}

	for _, tool := range result.Tools {
		if tool.Name == "tusk_relation_add" || tool.Name == "tusk_relation_remove" {
			t.Errorf("tool %s should be disabled but was listed", tool.Name)
		}
	}

	// Verify non-disabled tools are still present.
	foundTaskCreate := false
	for _, tool := range result.Tools {
		if tool.Name == "tusk_task_create" {
			foundTaskCreate = true
		}
	}
	if !foundTaskCreate {
		t.Error("tusk_task_create should be listed but was not found")
	}
}

// newMCPEnvWithHome starts an MCP server with a custom HOME directory.
func newMCPEnvWithHome(t *testing.T, binPath, home string) *mcpEnv {
	t.Helper()
	tmpFile, err := os.CreateTemp(t.TempDir(), "tusk-mcp-e2e-*.db")
	if err != nil {
		t.Fatalf("creating temp db: %v", err)
	}
	_ = tmpFile.Close()

	cmd := exec.Command(binPath, "--db", tmpFile.Name(), "mcp", "serve")

	// Set HOME to the custom dir so tusk picks up our config.
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "TUSK_DB=") {
			continue
		}
		env = append(env, e)
	}
	env = append(env, "HOME="+home)
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting mcp server: %v", err)
	}

	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})

	e := &mcpEnv{
		t:      t,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		nextID: 1,
	}

	e.initialize()
	return e
}
```

Add `"encoding/json"` and `"bufio"` to the imports if not already present.

- [ ] **Step 4: Run the test**

```bash
go test -v ./tests/e2e/ -run TestMCP_DisabledTools -timeout 60s
```

Expected: PASS

- [ ] **Step 5: Run all E2E tests for regressions**

```bash
go test -v ./tests/e2e/ -timeout 120s
```

Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add tests/e2e/config_test.go
git commit -m "test(e2e): add config file and MCP disabled tools E2E tests"
```

---

### Task 4: Write configuration reference documentation

**Files:**
- Create: `docs/configuration.md`

- [ ] **Step 1: Write the documentation**

Create `docs/configuration.md`:

```markdown
# Configuration Reference

Tusk works out of the box with no configuration. All settings have sensible defaults. You can optionally create a configuration file to customize behavior.

## Config File

Tusk looks for a config file at:

```
~/.config/tusk/config.toml
```

If the file doesn't exist, Tusk uses built-in defaults. There is no `tusk init` command — create the file manually when you need to override a setting.

## Precedence

Settings are resolved in this order (highest priority first):

1. **CLI flags** (e.g., `--db`)
2. **Environment variables** (e.g., `TUSK_DB`, `TUSK_STORAGE_PATH`)
3. **Config file** (`~/.config/tusk/config.toml`)
4. **Built-in defaults**

## Environment Variables

Every config key can be set via an environment variable with the `TUSK_` prefix. Nesting is represented with underscores:

| Config Key | Environment Variable |
|---|---|
| `storage.backend` | `TUSK_STORAGE_BACKEND` |
| `storage.path` | `TUSK_STORAGE_PATH` |
| `storage.postgres.dsn` | `TUSK_STORAGE_POSTGRES_DSN` |
| `urgency.priority_weight` | `TUSK_URGENCY_PRIORITY_WEIGHT` |
| `tui.color` | `TUSK_TUI_COLOR` |

The existing `TUSK_DB` environment variable continues to work and takes priority over `TUSK_STORAGE_PATH` for backwards compatibility.

---

## Sections

### `[storage]`

Database backend configuration.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `backend` | string | `"sqlite"` | Storage backend. Only `"sqlite"` is currently supported. |
| `path` | string | `"~/.local/share/tusk/tusk.db"` | Path to the SQLite database file. Tilde (`~`) is expanded to your home directory. |

```toml
[storage]
backend = "sqlite"
path = "~/.local/share/tusk/tusk.db"
```

#### `[storage.postgres]`

PostgreSQL settings (reserved for future use).

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `dsn` | string | `""` | PostgreSQL connection string. |

### `[urgency]`

Weights for the urgency scoring algorithm used to rank tasks. Higher weights increase a factor's influence on the urgency score.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `priority_weight` | float | `6.0` | Weight for task priority level |
| `due_weight` | float | `12.0` | Weight for due date proximity |
| `age_weight` | float | `2.0` | Weight for task age (older = higher urgency) |
| `blocking_weight` | float | `8.0` | Weight for tasks that block other tasks |
| `blocked_weight` | float | `-5.0` | Weight for tasks that are blocked (negative = lower urgency) |

```toml
[urgency]
priority_weight = 6.0
due_weight      = 12.0
age_weight      = 2.0
blocking_weight = 8.0
blocked_weight  = -5.0
```

### `[tui]`

Terminal UI and CLI output settings.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `date_format` | string | `"2006-01-02"` | Go [time format](https://pkg.go.dev/time#pkg-constants) for displaying dates |
| `color` | bool | `true` | Enable colored output. Set to `false` or use the `NO_COLOR` env var to disable. |
| `tree_indent` | int | `2` | Number of spaces per indent level in `tusk tree` output |
| `default_sort` | string | `"urgency"` | Default sort field for `tusk list` |

```toml
[tui]
date_format  = "2006-01-02"
color        = true
tree_indent  = 2
default_sort = "urgency"
```

### `[mcp]`

Control which tools and resources the MCP server exposes to AI agents. By default, everything is enabled. Use the disable lists to hide tools or resources that agents shouldn't access.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `disabled_tool_groups` | string[] | `[]` | Hide entire tool groups. Valid groups: `"task"`, `"relation"`, `"project"` |
| `disabled_tools` | string[] | `[]` | Hide individual tools by name |
| `disabled_resource_groups` | string[] | `[]` | Hide resource groups. Valid groups: `"task"`, `"project"`, `"workflow"` |
| `disabled_resources` | string[] | `[]` | Hide individual resources by URI template |

#### Available Tools

| Tool Name | Group | Description |
|-----------|-------|-------------|
| `tusk_task_create` | task | Create a new task |
| `tusk_task_get` | task | Get task details |
| `tusk_task_list` | task | List tasks with filters |
| `tusk_task_modify` | task | Modify task fields |
| `tusk_task_start` | task | Transition task to active |
| `tusk_task_done` | task | Transition task to completed |
| `tusk_task_delete` | task | Soft-delete a task |
| `tusk_task_annotate` | task | Add a note to a task |
| `tusk_task_tree` | task | Get task tree hierarchy |
| `tusk_relation_add` | relation | Create a relation between tasks |
| `tusk_relation_remove` | relation | Remove a relation |
| `tusk_project_list` | project | List all projects |
| `tusk_project_create` | project | Create a new project |

#### Available Resources

| URI Template | Group | Description |
|---|---|---|
| `tusk://tasks/{short_id}` | task | Full task details |
| `tusk://projects/{name}` | project | Project details |
| `tusk://projects/{name}/workflow` | workflow | Workflow statuses and transitions |

#### Example: Restrict agent to read-only task operations

```toml
[mcp]
disabled_tool_groups = ["relation", "project"]
disabled_tools = ["tusk_task_create", "tusk_task_modify", "tusk_task_start", "tusk_task_done", "tusk_task_delete", "tusk_task_annotate"]
disabled_resource_groups = ["workflow"]
```

---

## Full Example

See [`config/default.toml`](../config/default.toml) for a complete annotated example with all default values.
```

- [ ] **Step 2: Review the doc for accuracy**

Read through the document and verify:
- All field names match the Go struct definitions in `internal/config/config.go`
- All default values match what's set in `Load()`
- All tool names match those in `internal/mcp/server.go`
- All resource URIs match those in `internal/mcp/resources.go`
- The env var examples follow the `TUSK_` + uppercase + underscores pattern

- [ ] **Step 3: Commit**

```bash
git add docs/configuration.md
git commit -m "docs: add configuration reference documentation"
```

- [ ] **Step 4: Run full test suite as final verification**

```bash
make test
```

Expected: all tests pass. The configuration system is fully wired and documented.
