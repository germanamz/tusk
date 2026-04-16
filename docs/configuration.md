# Configuration Reference

Tusk works out of the box with no configuration. All settings have sensible defaults. You can optionally create a configuration file to customize behavior.

## Config File

Tusk picks a config file via a five-step chain. The first match wins — there is no merging between user files.

1. **`--config <path>`** — global CLI flag, consumed before Cobra parsing so it works on every subcommand. Missing file is a hard error.
2. **`TUSK_CONFIG=<path>`** — environment variable equivalent. Missing file is a hard error.
3. **Walk-up from the current directory** — Tusk walks from the caller's CWD toward the filesystem root and stops at the first `tusk.toml` it finds. No symlink resolution.
4. **Global `~/.config/tusk/config.toml`** — auto-created from defaults on first run **only when steps 1–3 all miss**. Running tusk inside a project that has its own `tusk.toml` never spawns a global file. The `TUSK_CONFIG_DIR` env var overrides the `~/.config/tusk` location.
5. **Embedded defaults.**

`--config` wins over `TUSK_CONFIG`; both bypass walk-up entirely. Individual `TUSK_*` environment variables (e.g. `TUSK_DB`, `TUSK_STORAGE_PATH`) still override values from whichever file won — that layering is unchanged.

### Walk-up example

```
/home/you/work/acme/tusk.toml       <- found
/home/you/work/acme/services/api/   <- CWD
```

Running `tusk task list` from `services/api/` uses the `tusk.toml` at `/home/you/work/acme/`. Relative paths inside that file — most importantly `storage.path` — resolve against `/home/you/work/acme/`, not the caller's CWD. So every subdirectory of the project hits the same database as `tusk` run from the project root. Absolute paths and `~`-prefixed paths are untouched.

### Workspace scope

One config file maps to one workspace: the `tusk.toml` at a project root owns the database at its `storage.path`, and every `tusk` invocation underneath that root shares it. The global `config.toml` owns its own workspace for invocations that walk up past any project boundary. There is no cross-workspace merging — each `tusk.toml` is an independent universe of tasks.

## Resolution Order

Settings are resolved in this order (highest priority first):

1. **CLI flags** (e.g., `--db`)
2. **Environment variables** (e.g., `TUSK_DB`, `TUSK_STORAGE_PATH`, `TUSK_CONFIG`)
3. **Config file**, resolved as:
   1. `--config <path>` (hard error if missing)
   2. `TUSK_CONFIG` (hard error if missing)
   3. Walk-up `tusk.toml` from CWD to filesystem root
   4. Global `~/.config/tusk/config.toml` (auto-created on first run only when 1–3 all miss)
4. **Built-in defaults**

## Inspecting the active file

- `tusk config show` prepends a `# active: <path>` TOML comment to its output so you can see which file is in effect — walk-up hits appear here verbatim. The body of the output remains valid TOML (the header is a comment).
- `tusk config path` prints the resolved file path to stdout — the walk-up hit when one is active, the global path otherwise. When no user file is active it prints the would-be global path to stdout and `(not yet created)` to stderr, so `tusk config path | xargs cat` continues to work when a file exists.
- `tusk config validate` validates the resolved file, including whatever `--config` / `TUSK_CONFIG` points at.
- `tusk config edit` opens the resolved file in `$VISUAL` / `$EDITOR`. With `--config` it opens that file directly; without it, the global file is created from defaults first if it doesn't yet exist.

## Writing Config

### `tusk config set`

`tusk config set <key> <value>` writes to whichever file `config show` reports as active:

- **Default** — writes to the walk-up `tusk.toml` when one is active, or the global `config.toml` when the resolver fell through to it.
- **`--global`** — forces writes to `~/.config/tusk/config.toml` regardless of walk-up. Creates the global file from defaults if it doesn't yet exist.
- **No active file, no `--global`** — returns `no config file found; run "tusk config init" or "tusk config init --local"`.

### `tusk config init`

```bash
tusk config init           # writes the global ~/.config/tusk/config.toml
tusk config init --local   # writes ./tusk.toml with the current effective config
```

Both variants refuse to overwrite an existing file. `init --local` captures the currently effective config (defaults plus whatever walk-up / global file / env vars are in play) so the new `tusk.toml` is a ready-to-edit snapshot.

## Environment Variables

Every config key can be set via an environment variable with the `TUSK_` prefix. Nesting is represented with underscores:

| Config Key | Environment Variable |
|---|---|
| `storage.backend` | `TUSK_STORAGE_BACKEND` |
| `storage.path` | `TUSK_STORAGE_PATH` |
| `storage.postgres.dsn` | `TUSK_STORAGE_POSTGRES_DSN` |
| `urgency.priority_weight` | `TUSK_URGENCY_PRIORITY_WEIGHT` |
| `tui.color` | `TUSK_TUI_COLOR` |
| `inline.max_expansion_size` | `TUSK_INLINE_MAX_EXPANSION_SIZE` |

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
| `tree_indent` | int | `2` | Number of spaces per indent level in `tusk task tree` output |
| `default_sort` | string | `"urgency"` | Default sort field for `tusk task list` |

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
| `disabled_tool_groups` | string[] | `[]` | Hide entire tool groups. Valid groups: `"task"`, `"task_relations"`, `"project"`, `"workflow"`, `"player"`, `"config"` |
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
| `tusk_task_link` | task_relations | Create a relation between tasks |
| `tusk_task_unlink` | task_relations | Remove a relation |
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
disabled_tool_groups = ["task_relations", "project"]
disabled_tools = ["tusk_task_create", "tusk_task_modify", "tusk_task_start", "tusk_task_done", "tusk_task_delete", "tusk_task_annotate"]
disabled_resource_groups = ["workflow"]
```

### `[inline]`

Configures the inline reference expander used across task-scoped CLI commands (`tusk task create`, `tusk task modify`, `tusk task annotate`). The expander runs on free-form string field values and substitutes file content or stdin at word-boundary `@` references. See PRODUCT.md's "Inline Syntax" section for the authoritative description of when `@` triggers, the `@@` escape, stdin semantics, and word-boundary rules.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `max_expansion_size` | int | `1048576` | Maximum size in bytes of a single `@` reference. Applied **per reference**, not per invocation — a command with two `@` references may expand up to `2 * max_expansion_size` bytes total. |

```toml
[inline]
max_expansion_size = 1048576  # 1 MB default
```

The `TUSK_INLINE_MAX_EXPANSION_SIZE` environment variable overrides this value (Viper auto-binds any `TUSK_`-prefixed env var to the matching config key).

#### Example: raise the per-reference cap to 5 MB

```toml
[inline]
max_expansion_size = 5242880  # 5 MB
```

Oversized references are rejected with the actual size and limit in the error message, e.g. `@./huge.log: file too large (7340032 bytes > 5242880 byte limit)`. Binary files are rejected separately via a NUL-byte scan on the first 8 KB of content — the size cap is not the binary gate.

---

## Full Example

See [`config/default.toml`](../config/default.toml) for a complete annotated example with all default values.

---

## Shell Completion

Tusk generates shell completion scripts on demand from the current Cobra command tree — no pre-baked artifacts ship in the repository or the release tarballs. Regenerate and reinstall after every upgrade so completion stays in sync with the binary.

| Shell      | Install path (user scope)                                                                             |
|------------|-------------------------------------------------------------------------------------------------------|
| bash       | `~/.local/share/bash-completion/completions/tusk`                                                     |
| zsh        | `"${fpath[1]}/_tusk"` — first writable directory in `$fpath`                                          |
| fish       | `~/.config/fish/completions/tusk.fish`                                                                |
| powershell | Append output to your `$PROFILE` (e.g. `tusk completion powershell \| Out-String \| Invoke-Expression`) |

```bash
# bash — user scope
tusk completion bash > ~/.local/share/bash-completion/completions/tusk

# zsh — drop in any directory listed in $fpath
tusk completion zsh > "${fpath[1]}/_tusk"

# fish — user scope
tusk completion fish > ~/.config/fish/completions/tusk.fish

# powershell — append to your profile
tusk completion powershell | Out-String | Invoke-Expression
```

Run `tusk completion --help` for the command reference.
