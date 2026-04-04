# Configuration System Design

**Initiative:** v0.4 — Configuration & Customization (Configuration System subset)  
**Date:** 2026-04-04  
**Status:** Approved

---

## Overview

Add a Viper-based configuration system to Tusk. The system loads settings from a TOML config file at `~/.config/tusk/config.toml`, with environment variable overrides via `TUSK_` prefix, falling back to hardcoded defaults. Zero configuration is required — Tusk works out of the box.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Config library | Viper (non-global instance) | Standard for Cobra CLIs; handles TOML + env overlay + defaults |
| Config file location | `~/.config/tusk/config.toml` | XDG-compliant |
| Zero config | Yes — no `tusk init` | Works out of the box; config file is optional |
| Struct scope | All sections upfront | `Storage`, `Urgency`, `TUI`, `MCP` defined now; refined as features land |
| DI wiring | Pass sub-structs | Components receive only their relevant config section |
| MCP groups | Explicit tag at registration | Each tool/resource declares its group; more flexible than prefix convention |

## Config Struct

New package: `internal/config/`

```go
type Config struct {
    Storage StorageConfig `mapstructure:"storage"`
    Urgency UrgencyConfig `mapstructure:"urgency"`
    TUI     TUIConfig     `mapstructure:"tui"`
    MCP     MCPConfig     `mapstructure:"mcp"`
}

type StorageConfig struct {
    Backend  string         `mapstructure:"backend"`
    Path     string         `mapstructure:"path"`
    Postgres PostgresConfig `mapstructure:"postgres"`
}

type PostgresConfig struct {
    DSN string `mapstructure:"dsn"`
}

type UrgencyConfig struct {
    PriorityWeight float64 `mapstructure:"priority_weight"`
    DueWeight      float64 `mapstructure:"due_weight"`
    AgeWeight      float64 `mapstructure:"age_weight"`
    BlockingWeight float64 `mapstructure:"blocking_weight"`
    BlockedWeight  float64 `mapstructure:"blocked_weight"`
}

type MCPConfig struct {
    DisabledToolGroups     []string `mapstructure:"disabled_tool_groups"`
    DisabledTools          []string `mapstructure:"disabled_tools"`
    DisabledResourceGroups []string `mapstructure:"disabled_resource_groups"`
    DisabledResources      []string `mapstructure:"disabled_resources"`
}

type TUIConfig struct {
    DateFormat  string `mapstructure:"date_format"`
    Color       bool   `mapstructure:"color"`
    TreeIndent  int    `mapstructure:"tree_indent"`
    DefaultSort string `mapstructure:"default_sort"`
}
```

### Defaults

| Field | Default |
|-------|---------|
| `storage.backend` | `"sqlite"` |
| `storage.path` | `"~/.local/share/tusk/tusk.db"` |
| `urgency.priority_weight` | `6.0` |
| `urgency.due_weight` | `12.0` |
| `urgency.age_weight` | `2.0` |
| `urgency.blocking_weight` | `8.0` |
| `urgency.blocked_weight` | `-5.0` |
| `tui.date_format` | `"2006-01-02"` |
| `tui.color` | `true` |
| `tui.tree_indent` | `2` |
| `tui.default_sort` | `"urgency"` |
| `mcp.disabled_tool_groups` | `[]` (empty) |
| `mcp.disabled_tools` | `[]` (empty) |
| `mcp.disabled_resource_groups` | `[]` (empty) |
| `mcp.disabled_resources` | `[]` (empty) |

## Config Loading

`config.Load() (*Config, error)` performs:

1. Create a new `viper.New()` instance (no global state).
2. Set hardcoded defaults for every field.
3. Set config name `config`, type `toml`, search path `~/.config/tusk/`.
4. Bind `TUSK_` environment variable prefix. Nested keys use `_` separator (e.g., `TUSK_STORAGE_PATH`, `TUSK_TUI_COLOR`).
5. Call `ReadInConfig()`. If file not found, proceed silently with defaults. Any other error (malformed TOML) is returned.
6. Unmarshal into `Config` struct.
7. Expand `~` in `Storage.Path` to the user's home directory.

### DB Path Precedence

The `--db` CLI flag and `TUSK_DB` env var predate the config system and continue to work as top-priority overrides:

```
--db flag > TUSK_DB env var > config.Storage.Path > hardcoded default
```

This is resolved in `main.go` after `config.Load()`, not inside the config package.

## MCP Visibility Filtering

### Group Annotations

The MCP Server struct maintains internal maps associating each tool and resource with a group:

```go
type Server struct {
    // ... existing fields
    cfg            MCPConfig
    toolGroups     map[string]string // tool name -> group
    resourceGroups map[string]string // resource URI template -> group
}
```

### Group Assignments

| Tool | Group |
|------|-------|
| `tusk_task_create` | `task` |
| `tusk_task_get` | `task` |
| `tusk_task_list` | `task` |
| `tusk_task_modify` | `task` |
| `tusk_task_start` | `task` |
| `tusk_task_done` | `task` |
| `tusk_task_delete` | `task` |
| `tusk_task_annotate` | `task` |
| `tusk_task_tree` | `task` |
| `tusk_relation_add` | `relation` |
| `tusk_relation_remove` | `relation` |
| `tusk_project_list` | `project` |
| `tusk_project_create` | `project` |

| Resource Template | Group |
|---|---|
| `tusk://tasks/{short_id}` | `task` |
| `tusk://projects/{name}` | `project` |
| `tusk://projects/{name}/workflow` | `workflow` |

### Filtering Logic

Before calling `AddTool()` or `AddResourceTemplate()`, check whether the tool/resource name or its group appears in the config's disabled lists. If disabled, skip registration — the tool/resource never appears in the MCP schema.

```go
func (s *Server) isToolEnabled(name, group string) bool {
    return !contains(s.cfg.DisabledTools, name) &&
           !contains(s.cfg.DisabledToolGroups, group)
}
```

Same pattern for `isResourceEnabled`.

### Validation

On startup, warn to stderr if any disabled tool/resource/group name doesn't match a known registration. This catches typos without failing hard.

## DI Wiring Changes

### Updated main.go Flow

1. `config.Load()` — first thing, before anything else.
2. Resolve DB path with full precedence chain.
3. Open store, create repos, create services (unchanged).
4. Pass `cfg.TUI` to TUI app constructor.
5. Pass `cfg.MCP` to MCP server constructor.

### Constructor Signature Changes

```go
// MCP server gains MCPConfig parameter
tuskmcp.New(taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, version, cfg.MCP)

// TUI app gains TUIConfig parameter
tui.NewApp(taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, cfg.TUI)
```

Components receive only their relevant sub-struct — never the full Config.

## Documentation

### New File: `docs/configuration.md`

User-facing reference structured as:

1. **Overview** — Zero config, optional file, precedence rules.
2. **Config file location** — `~/.config/tusk/config.toml`.
3. **Environment variables** — `TUSK_` prefix convention with nesting via `_`.
4. **Section reference** — Each config section with table of fields, types, defaults, descriptions, example TOML, and corresponding env vars.
5. **Full example** — Complete annotated config showing all fields with defaults.

The existing `config/default.toml` is updated to match the final schema and serves as the canonical example referenced from the docs.

## Testing

### Unit Tests: `internal/config/`

| Test | Description |
|------|-------------|
| `TestLoad_Defaults` | No config file, no env vars — all defaults correct |
| `TestLoad_File` | Temp TOML file — values override defaults |
| `TestLoad_EnvOverride` | `TUSK_STORAGE_PATH` etc. — env takes precedence over file |
| `TestLoad_FileNotFound` | No file — no error, defaults used |
| `TestLoad_MalformedFile` | Invalid TOML — error returned |
| `TestLoad_TildeExpansion` | `~/foo` in storage path — resolves to home dir |

### Unit Tests: MCP Visibility

| Test | Description |
|------|-------------|
| `TestToolFiltering_DisabledTool` | Disable `tusk_task_create` — not registered |
| `TestToolFiltering_DisabledGroup` | Disable group `relation` — both relation tools gone |
| `TestResourceFiltering_DisabledResource` | Disable specific resource template — not registered |
| `TestResourceFiltering_DisabledGroup` | Disable group `workflow` — workflow resource gone |
| `TestValidation_UnknownGroup` | Disable `nonexistent_group` — stderr warning |

### E2E Tests

| Test | Description |
|------|-------------|
| `TestCLI_WithConfigFile` | Config file with custom DB path — tusk uses it |
| `TestMCP_DisabledTools` | MCP server with disabled tools — tool list excludes them |

## Out of Scope

These are handled by other stories in the v0.4 initiative, not this design:

- Workflow definitions in config (`[workflows.<name>]`)
- Per-project workflow assignment
- Workflow CLI commands (`tusk workflow list`, `tusk workflow info`)
- MCP workflow tools (`tusk_workflow_list`)
