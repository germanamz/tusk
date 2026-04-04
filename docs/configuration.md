# Configuration Reference

Tusk works out of the box with no configuration. All settings have sensible defaults. You can optionally create a configuration file to customize behavior.

## Config File

Tusk looks for a config file at:

```
~/.config/tusk/config.toml
```

If the file doesn't exist, Tusk uses built-in defaults. There is no `tusk init` command -- create the file manually when you need to override a setting.

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
