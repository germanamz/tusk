<p align="center">
  <img src="assets/banner.svg" alt="Tusk — Concurrent-safe task management CLI with MCP server" width="900"/>
</p>

<p align="center">
  <a href="#installation"><strong>Install</strong></a> &middot;
  <a href="#quick-start"><strong>Quick Start</strong></a> &middot;
  <a href="#go-library"><strong>Go Library</strong></a> &middot;
  <a href="#mcp-server"><strong>MCP Server</strong></a> &middot;
  <a href="#development"><strong>Development</strong></a> &middot;
  <a href="tusk.md"><strong>Design Spec</strong></a>
</p>

> ⚠️ **v1 rebuild in progress.** This README documents Tusk **v0.x**. The v0 line ended at [`v0.14.0`](https://github.com/germanamz/tusk/releases/tag/v0.14.0). Active development now targets **Tusk v1** — a local-first agent brain. See the [v1 design spec](docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md). v0 sources remain available on the [`v0-archive`](https://github.com/germanamz/tusk/tree/v0-archive) branch.

---

Tusk combines the speed and CLI ergonomics of TaskWarrior with structured hierarchy and workflow flexibility — without the bloat. It ships as a single binary with SQLite persistence and exposes every capability through both a terminal interface and an MCP (Model Context Protocol) server, so AI agents can manage tasks alongside humans.

## Features

- **Single binary** — no runtime, no daemon, no browser. Install and go.
- **Hierarchical tasks** — optional parent-child nesting to arbitrary depth.
- **Typed relations** — `blocks`, `relates_to`, `duplicates` as first-class edges with DFS cycle detection.
- **Configurable workflows** — database-backed workflows managed via `tusk workflow` commands with per-project assignment.
- **Concurrent-safe** — optimistic locking via version fields.
- **Pluggable storage** — SQLite out of the box; repository layer is an interface.
- **Built-in MCP server** — 27 MCP tools + 3 resource templates for AI agent integration (stdio transport).
- **TaskWarrior-like filters** — `status=pending,active`, `priority=2..4`, `due=today`, `+tag`, `-tag`, `uda.key=value`, boolean operators (`AND`, `OR`, `NOT`).
- **User-defined attributes (UDA)** — arbitrary key-value metadata on tasks with merge semantics.
- **Configuration system** — Viper-based TOML config with env var overrides and auto-creation.
- **Completion propagation** — auto-complete/revert parents based on children status, configurable per project.
- **Tree views** — full task hierarchy rendering with subtree support.

## Installation

### Quick install (Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/germanamz/tusk/main/install.sh | sh
```

This downloads the latest release binary for your platform to `~/.local/bin/tusk`. Override the install directory with `INSTALL_DIR`:

```bash
INSTALL_DIR=/usr/local/bin curl -fsSL https://raw.githubusercontent.com/germanamz/tusk/main/install.sh | sh
```

Supported platforms: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.

### From source

Requires Go 1.26+:

```bash
git clone https://github.com/germanamz/tusk.git
cd tusk
make build
```

The binary is compiled to `bin/tusk`.

### Database

Default path: `~/.local/share/tusk/tusk.db`. Override with `--db` flag or `TUSK_DB` environment variable.

## Quick Start

```bash
# Create tasks
tusk task create "Implement auth middleware" priority=3 +api
tusk task create "Write tests for auth" +testing
tusk task create "Deploy to staging" uda.env=staging uda.team=backend
tusk task create "Design spec" description=@./spec.md    # load from file

# List and filter
tusk task list                           # all pending tasks
tusk task list status=active +api        # filtered by status and tag
tusk task list priority=2..4             # priority range
tusk task list uda.env=staging           # filter by user-defined attribute

# Update tasks
tusk task modify a3f8b2c1 priority=4 +urgent
tusk task modify a3f8b2c1 uda.env=prod         # add/update a UDA
tusk task modify a3f8b2c1 description=@-       # load from stdin
tusk task start a3f8b2c1               # pending -> active
tusk task done a3f8b2c1                # active -> completed
tusk task delete a3f8b2c1              # -> deleted

# Hierarchy
tusk task create "Subtask" parent=a3f8b2c1
tusk task tree                         # full task tree
tusk task tree a3f8b2c1                # subtree from task

# Relations
tusk task link a3f8b2c1 blocks b4e9c3d2
tusk task unlink a3f8b2c1 blocks b4e9c3d2

# Annotate
tusk task annotate a3f8b2c1 "Blocked by upstream API changes"

# Task details
tusk task get a3f8b2c1

# Projects and workflows
tusk project list
tusk workflow list
tusk workflow info kanban

# Tags
tusk tag list --usage
tusk tag create sprint-1 --color blue
tusk tag rename sprint-1 sprint-2
```

## Filter Syntax

Inspired by TaskWarrior:

| Filter | Description |
|--------|-------------|
| `status=pending,active` | Comma-separated OR |
| `priority=2..4` | Range |
| `due=today` | Relative dates |
| `+tag` | Include tag |
| `-tag` | Exclude tag |
| `parent=<short_id>` | Direct children |
| `tree=<short_id>` | All descendants |
| `uda.key=value` | UDA exact match |
| `uda.key=` | UDA key absent or empty |
| `title="search text"` | Substring match in title |
| `AND`, `OR`, `NOT` | Boolean operators |
| `(...)` | Grouping |

## Configuration

Tusk resolves its config file in this order, first match wins:

1. `--config <path>` flag (hard error if missing)
2. `TUSK_CONFIG` env var (hard error if missing)
3. Walk-up `tusk.toml` from the current directory toward the filesystem root
4. Global `~/.config/tusk/config.toml` — auto-created on first run **only** when steps 1–3 all miss, so a project with its own `tusk.toml` never spawns a global file
5. Embedded defaults

Relative paths inside a `tusk.toml` (most importantly `storage.path`) resolve against the file's directory, so every subdirectory of a project shares the same database. `TUSK_*` environment variables still override individual values from the resolved file.

Custom workflows and projects are created via CLI commands:

```bash
# Workflows and projects are managed via CLI, not config file
tusk workflow create kanban \
  status=pending(initial) status=active(start,highlight) \
  status=completed(terminal,done) status=deleted(terminal,delete,dim) \
  transition=pending:active,active:completed,active:deleted,completed:pending

tusk project create backend workflow=kanban
```

## Architecture

Layered design with dependencies flowing downward only:

```
Interface Layer (CLI via Cobra, MCP server)
    |
Service Layer (business logic, validation)
    |
Repository Layer (Go interfaces only)
    |
Storage Implementations (SQLite with WAL mode)
```

Key design choices:
- **Optimistic locking** with version fields for concurrent access
- **UUID + 8-char short ID** for task identity (UUID internally, short ID for CLI)
- **Soft delete** via workflow status transitions
- **Database-backed projects and workflows** — managed via `tusk project` and `tusk workflow` commands with optimistic locking

## Go Library

Tusk's core packages are importable, so other Go programs can embed task management directly without shelling out to the CLI or speaking MCP. A high-level `Client` type wires up the database, migrations, and all services from a single config struct:

```go
client, err := tusk.NewClient(tusk.Config{
    DBPath: "/tmp/my-tasks.db",
})
defer client.Close()

task := &domain.Task{Title: "Build the thing", Priority: 3}
client.Tasks.Create(ctx, task)
```

The `Client` exposes service instances as public fields (`Tasks`, `Tags`, `Relations`, `Projects`, `Workflows`, `Players`), giving programmatic access to every operation available through CLI and MCP. Requires **v0.8.0+**.

The `Client` exposes service instances as public fields for full programmatic access to every operation available through CLI and MCP.

## MCP Server

Start the MCP server for AI agent integration:

```bash
tusk mcp serve              # stdio transport (IDE integration)
```

27 MCP tools expose full task management: task CRUD, lifecycle transitions, annotations, tree views, relations, player coordination, project/workflow CRUD, and configuration. 3 resource templates provide read-only views of tasks, projects, and workflows.

All mutation tools require a `version` parameter for optimistic locking. Tool and resource visibility is configurable via `config.toml`:

```toml
[mcp]
disabled_tool_groups = ["workflow"]
disabled_tools = ["tusk_task_tree"]
```

### Claude Code

Add Tusk as an MCP server in Claude Code:

```bash
claude mcp add tusk -- tusk mcp serve
```

No URL or network setup needed — Tusk uses stdio transport by default, so Claude Code launches it as a subprocess and communicates over stdin/stdout.

## Development

```bash
make build          # Compile to bin/tusk
make test           # Unit + e2e tests
make test-race      # Tests with race detector
make test-e2e       # E2e tests only
make vet            # go vet
make lint           # golangci-lint run
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines and [docs/dev-environment.md](docs/dev-environment.md) for the recommended dev-container + Zellij + Ghostty setup.

## Roadmap

Tusk v0 ended at `v0.14.0`. Active development targets v1 — see the [v1 design spec](docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md) for the planned direction.

See [tusk.md](tusk.md) for the v0 design spec.

## Acknowledgements

Tusk is heavily inspired by [TaskWarrior](https://taskwarrior.org/) — the gold standard for command-line task management. Tusk's filter syntax, urgency scoring model, and CLI ergonomics all build on ideas pioneered by TaskWarrior. If you haven't tried it, you should.

## License

Apache 2.0 — see [LICENSE](LICENSE).
