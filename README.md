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

---

Tusk combines the speed and CLI ergonomics of TaskWarrior with structured hierarchy and workflow flexibility — without the bloat. It ships as a single binary with SQLite persistence and exposes every capability through both a terminal interface and an MCP (Model Context Protocol) server, so AI agents can manage tasks alongside humans.

## Features

- **Single binary** — no runtime, no daemon, no browser. Install and go.
- **Hierarchical tasks** — optional parent-child nesting to arbitrary depth.
- **Typed relations** — `blocks`, `relates_to`, `duplicates` as first-class edges with DFS cycle detection.
- **Configurable workflows** — declarative TOML-based workflows with per-project assignment.
- **Concurrent-safe** — optimistic locking via version fields.
- **Pluggable storage** — SQLite out of the box; repository layer is an interface.
- **Built-in MCP server** — 14 MCP tools + 3 resource templates for AI agent integration (stdio transport).
- **TaskWarrior-like filters** — `status:pending,active`, `priority:2..4`, `due:today`, `+tag`, `-tag`, `uda.key:value`.
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
tusk add "Implement auth middleware" priority:3 +api
tusk add "Write tests for auth" +testing
tusk add "Deploy to staging" --uda env=staging --uda team=backend

# List and filter
tusk list                          # all pending tasks
tusk list status:active +api       # filtered by status and tag
tusk list priority:2..4            # priority range
tusk list uda.env:staging          # filter by user-defined attribute

# Update tasks
tusk modify a3f8b2c1 priority:4 +urgent
tusk modify a3f8b2c1 --uda env=prod   # merge UDA key
tusk start a3f8b2c1               # pending -> active
tusk done a3f8b2c1                # active -> completed
tusk delete a3f8b2c1              # -> deleted

# Hierarchy
tusk add "Parent task" parent:a3f8b2c1
tusk tree                          # full task tree
tusk tree a3f8b2c1                 # subtree from task

# Relations
tusk link a3f8b2c1 blocks b4e9c3d2
tusk unlink a3f8b2c1 blocks b4e9c3d2

# Annotate
tusk annotate a3f8b2c1 "Blocked by upstream API changes"

# Task details
tusk info a3f8b2c1

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
| `status:pending,active` | Comma-separated OR |
| `priority:2..4` | Range |
| `due:today` | Relative dates |
| `+tag` | Include tag |
| `-tag` | Exclude tag |
| `parent:<short_id>` | Direct children |
| `tree:<short_id>` | All descendants |
| `uda.key:value` | UDA exact match |
| `uda.key:` | UDA key absent or empty |

## Configuration

On first run, Tusk creates `~/.config/tusk/config.toml` with sensible defaults. Config is loaded in priority order:

1. Environment variables (`TUSK_*` prefix)
2. Config file (`~/.config/tusk/config.toml`)
3. Embedded defaults

Define custom workflows and projects in the config:

```toml
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]

[[workflows.kanban.transitions]]
from = "pending"
to = ["active", "deleted"]

[projects.backend]
workflow = "kanban"
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
Storage Implementations (SQLite with WAL mode, in-memory for config entities)
```

Key design choices:
- **Optimistic locking** with version fields for concurrent access
- **UUID + 8-char short ID** for task identity (UUID internally, short ID for CLI)
- **Soft delete** via workflow status transitions
- **Declarative workflows** — TOML-defined, with per-project assignment
- **Config-driven projects** — projects and workflows live in config, not the database

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

See [docs/programmatic-usage.md](docs/programmatic-usage.md) for the full API guide.

## MCP Server

Start the MCP server for AI agent integration:

```bash
tusk mcp serve              # stdio transport (IDE integration)
```

14 MCP tools expose full task management: create, get, list, modify, start, done, delete, annotate, tree, relation add/remove, project list, and workflow list. 3 resource templates provide read-only views of tasks, projects, and workflows.

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

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## Roadmap

**Current: v0.5 complete** — foundation, relations, MCP server, config system, and rich content are all shipped.

See [ROADMAP.md](ROADMAP.md) for the full roadmap with initiatives and stories.

See [tusk.md](tusk.md) for the full design spec.

## Acknowledgements

Tusk is heavily inspired by [TaskWarrior](https://taskwarrior.org/) — the gold standard for command-line task management. Tusk's filter syntax, urgency scoring model, and CLI ergonomics all build on ideas pioneered by TaskWarrior. If you haven't tried it, you should.

## License

Apache 2.0 — see [LICENSE](LICENSE).
