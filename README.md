<p align="center">
  <img src="assets/banner.svg" alt="Tusk — Concurrent-safe task management CLI with MCP server" width="900"/>
</p>

<p align="center">
  <a href="#installation"><strong>Install</strong></a> &middot;
  <a href="#quick-start"><strong>Quick Start</strong></a> &middot;
  <a href="#mcp-server"><strong>MCP Server</strong></a> &middot;
  <a href="#development"><strong>Development</strong></a> &middot;
  <a href="tusk.md"><strong>Design Spec</strong></a>
</p>

---

Tusk combines the speed and CLI ergonomics of TaskWarrior with structured hierarchy and workflow flexibility — without the bloat. It ships as a single binary with SQLite persistence and exposes every capability through both a terminal interface and an MCP (Model Context Protocol) server, so AI agents can manage tasks alongside humans.

## Features

- **Single binary** — no runtime, no daemon, no browser. Install and go.
- **Hierarchical tasks** — optional parent-child nesting to arbitrary depth.
- **Typed relations** — `blocks`, `relates_to`, `duplicates` as first-class edges with cycle detection.
- **Configurable workflows** — define allowed status transitions per project.
- **Concurrent-safe** — optimistic locking via version fields.
- **Pluggable storage** — SQLite out of the box; repository layer is an interface.
- **Built-in MCP server** — every CLI command is also an MCP tool for AI agent integration.
- **TaskWarrior-like filters** — `status:pending,active`, `priority:2..4`, `due:today`, `+tag`, `-tag`.

## Installation

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

# List and filter
tusk list                          # all pending tasks, sorted by urgency
tusk list status:active +api       # filtered by status and tag
tusk list priority:2..4            # priority range

# Update tasks
tusk modify a3f8b2c1 priority:4 +urgent
tusk start a3f8b2c1               # pending -> active
tusk done a3f8b2c1                # active -> completed
tusk delete a3f8b2c1              # -> deleted

# Annotate
tusk annotate a3f8b2c1 "Blocked by upstream API changes"

# Task details
tusk info a3f8b2c1
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
- **Per-project workflows** with configurable status transitions

## MCP Server

Start the MCP server for AI agent integration:

```bash
tusk mcp serve                                    # stdio transport (IDE integration)
tusk mcp serve --transport sse --port 8080        # SSE transport (network)
```

All CLI commands are available as MCP tools with optimistic locking via version passing.

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

- [x] **v0.1** — Foundation: domain types, SQLite, TaskService, CLI, tags, filters, e2e tests
- [ ] **v0.2** — Relations and hierarchy: cycle detection, tree view, completion propagation
- [ ] **v0.3** — MCP server: stdio transport, all CLI commands as tools
- [ ] **v0.4** — Urgency and UX: scoring engine, configurable weights, color output
- [ ] **v0.5** — Advanced: recurrence, UDA, SSE transport, export

See [tusk.md](tusk.md) for the full design spec.

## Acknowledgements

Tusk is heavily inspired by [TaskWarrior](https://taskwarrior.org/) — the gold standard for command-line task management. Tusk's filter syntax, urgency scoring model, and CLI ergonomics all build on ideas pioneered by TaskWarrior. If you haven't tried it, you should.

## License

Apache 2.0 — see [LICENSE](LICENSE).
