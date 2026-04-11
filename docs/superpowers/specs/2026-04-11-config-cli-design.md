# Config CLI Design

## Overview

CLI commands for managing tusk configuration without manual TOML editing. Covers inspection, mutation, and validation of the config file.

Part of v0.9 — Configuration Management. Scoped to the **Config CLI** initiative only; workflow CRUD, project CRUD, per-project DB paths, local config discovery, and MCP config tools are separate initiatives that build on the infrastructure introduced here.

## Decisions

- **Rewrite on write.** `config set` writes the full Config struct as TOML. No comment preservation — the CLI is the primary config interface, reducing the need for manual file editing.
- **`config show` outputs full effective config.** Merged defaults + file + env, rendered as TOML. Source annotations deferred to the Local Config Discovery initiative.
- **Viper for reads, go-toml for writes.** Viper stays the runtime read path (env vars, merging, discovery). `pelletier/go-toml/v2` handles TOML marshaling on the write path. Clean separation by concern.
- **`config set` limited to scalars and flat lists.** Structured mutations (workflow transitions, project settings) go through dedicated commands in the Workflow/Project CRUD initiatives.
- **`config set` rejects if no config file exists.** User must run `config init` first. Avoids ambiguity about file creation location, especially once local config discovery exists.
- **`config validate` validates the file only.** Parses the on-disk file without merging env vars or defaults. Catches authoring errors. The merged config is already validated on every `Load()` at runtime.
- **`config init` always communicates status.** Prints "Created <path>" or "Config file already exists: <path>".
- **`config get` uses Viper dot notation.** `v.Get(key)` on the merged config. Scalars print as plain text, complex values as JSON. Respects `--format json`.

## Config Package Changes

### New file: `config/write.go`

Three new public functions:

**`ConfigFilePath(opts ...Option) (string, error)`**

Resolves the config file path using the same logic as `Load()`: custom path option > `TUSK_CONFIG_DIR` env > `~/.config/tusk/config.toml`. Returns the path regardless of whether the file exists.

**`LoadFile(path string) (*Config, error)`**

Parses a single TOML file into a `Config` struct using `pelletier/go-toml/v2` directly. No Viper, no env merging, no embedded defaults. Used by `config set` (load-modify-write without env contamination) and `config validate` (file-only validation).

**`WriteConfig(cfg *Config, path string) error`**

Marshals the `Config` struct to TOML via `pelletier/go-toml/v2` and writes to disk atomically (write to temp file, rename). Single write path for all config mutations.

### Struct tag additions

All config types get `toml` struct tags alongside existing `mapstructure` tags:

```go
type Config struct {
    Storage   StorageConfig             `mapstructure:"storage"   toml:"storage"`
    Urgency   UrgencyConfig             `mapstructure:"urgency"   toml:"urgency"`
    TUI       TUIConfig                 `mapstructure:"tui"       toml:"tui"`
    MCP       MCPConfig                 `mapstructure:"mcp"       toml:"mcp"`
    Workflows map[string]WorkflowConfig `mapstructure:"workflows" toml:"workflows"`
    Projects  map[string]ProjectConfig  `mapstructure:"projects"  toml:"projects"`
}
```

Same pattern applied to all nested types (`StorageConfig`, `UrgencyConfig`, `TUIConfig`, `MCPConfig`, `WorkflowConfig`, `WorkflowTransitionConfig`, `ProjectConfig`, `ProjectSettingsConfig`, `AutoCompleteParentConfig`, `AutoRevertParentConfig`, `ProjectUrgencyConfig`, `PostgresConfig`).

### Existing code unchanged

`Load()` and `Validate()` remain untouched. The existing read path is proven and stays the runtime entry point.

## CLI Commands

New `buildConfigCmd()` method on `App` in `internal/tui/config.go`, following the same pattern as `buildTagCmd()` and `buildWorkflowCmd()`.

### `tusk config show`

- No arguments
- Loads effective config via `config.Load()`
- Marshals to TOML via `toml.Marshal()`, prints to stdout

### `tusk config get <key>`

- One argument: dot-path key (e.g., `urgency.due_weight`, `workflows.kanban.statuses`)
- Uses Viper's `v.Get(key)` on the merged config
- Scalars: print as plain text via `fmt.Fprintln`
- Complex values (maps, arrays): print as JSON
- `--format json`: always outputs JSON
- Unknown key: returns error

### `tusk config path`

- No arguments
- Prints the resolved config file path via `config.ConfigFilePath()`

### `tusk config set <key> <value>`

- Two arguments: dot-path key and value string
- Flow:
  1. `ConfigFilePath()` to resolve path
  2. If file doesn't exist, return error: `no config file found; run "tusk config init" to create one`
  3. `LoadFile(path)` to parse the file into `*Config` (go-toml, no defaults, no env)
  4. Marshal the loaded `Config` back to a TOML byte buffer, load that into a fresh Viper instance (this gives Viper the file-only config as its base, enabling dot-path `Set()`)
  5. Call `v.Set(key, parsedValue)` — for `[]string` target fields, split value on commas before setting
  6. `v.Unmarshal()` back to a new `*Config`
  7. `Validate()` the result — reject and leave file untouched if invalid
  8. `WriteConfig(cfg, path)` to write back
- Unknown keys rejected with error: `unknown config key: "<key>"`
- Detection: walk Config struct mapstructure tags to build a set of valid dot-paths

### `tusk config edit`

- No arguments
- Checks `$VISUAL`, then `$EDITOR`
- If neither set, returns error: `$EDITOR is not set`
- Opens the config file path in the editor via `exec`

### `tusk config init`

- No arguments
- If file exists: print `Config file already exists: <path>`, exit 0
- If file doesn't exist: write embedded defaults to path, print `Created <path>`, exit 0

### `tusk config validate`

- No arguments
- `LoadFile(path)` + `Validate()` on the file
- On success: print `Config valid`, exit 0
- On validation failure: print error details, exit 1
- On malformed TOML: print parse error, exit 1

## Value Parsing for `config set`

Type inference from the target struct field, not from the value string. Viper + mapstructure handles coercion:

- `tui.color` is a `bool` field → `"false"` coerced to `false`
- `urgency.due_weight` is a `float64` field → `"10.0"` coerced to `10.0`
- `mcp.disabled_tools` is a `[]string` field → `"tusk_task_delete,tusk_task_tree"` split on commas

Map-keyed paths like `workflows.kanban.highlight_statuses "active,pending"` target a known struct field within a map entry. Partial entries (e.g., creating a new workflow with only statuses) are allowed — `Validate()` catches incomplete configurations before writing.

## App Wiring

The `App` struct needs access to config load options so config commands can resolve the file path:

```go
type App struct {
    // ... existing fields ...
    loadOpts []config.Option  // new
}
```

Constructor signature adds `loadOpts`:

```go
func New(..., loadOpts []config.Option) *App
```

`cmd/tusk/main.go` passes the load options through.

## File Changes

### New files

| File | Purpose |
|---|---|
| `config/write.go` | `WriteConfig()`, `LoadFile()`, `ConfigFilePath()` |
| `config/write_test.go` | Unit tests for write, load-file, path resolution |
| `internal/tui/config.go` | `buildConfigCmd()` + all `runConfig*` handlers |

### Modified files

| File | Change |
|---|---|
| `config/config.go` | Add `toml` struct tags to all config types |
| `internal/tui/app.go` | Add `loadOpts` field, call `a.root.AddCommand(a.buildConfigCmd())` |
| `cmd/tusk/main.go` | Pass load options to `App` constructor |
| `go.mod` | Promote `pelletier/go-toml/v2` from indirect to direct |
| `tests/e2e/config_test.go` | New E2E scenarios for all config commands |

## Error Handling

| Scenario | Behavior |
|---|---|
| `config set` with no config file | Error: `no config file found; run "tusk config init" to create one` |
| `config set` with unknown key | Error: `unknown config key: "<key>"` |
| `config set` fails validation | Error from `Validate()`, file untouched |
| `config get` with unknown key | Error: `unknown config key: "<key>"` |
| `config edit` with no `$EDITOR` | Error: `$EDITOR is not set` |
| `config validate` with bad TOML | Parse error, exit 1 |
| `config validate` with invalid refs | Validation errors, exit 1 |

## Testing

### Unit tests (`config/write_test.go`)

- `WriteConfig` round-trips through `LoadFile` correctly
- `LoadFile` on nonexistent file returns error
- `LoadFile` on malformed TOML returns parse error
- `ConfigFilePath` respects options and env var
- Valid dot-path detection accepts known keys, rejects unknown

### E2E tests (`tests/e2e/config_test.go`)

- `config init` creates file, second run reports "already exists"
- `config path` prints the path
- `config show` outputs valid TOML
- `config get` returns correct scalar values
- `config get` returns JSON for complex values
- `config set` modifies a scalar, persists across invocations
- `config set` modifies a list field with comma-separated values
- `config set` rejects unknown keys
- `config set` rejects invalid config (e.g., project referencing nonexistent workflow)
- `config set` rejects when no config file exists
- `config validate` passes on valid file
- `config validate` fails on invalid file
