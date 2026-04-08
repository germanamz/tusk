# v0.8 Programmatic Access — Design Spec

**Date:** 2026-04-07
**Goal:** Expose tusk's core APIs as importable Go packages so other programs can embed tusk as a library.

---

## Problem

All of tusk's packages live under `internal/`, making them inaccessible to external Go programs. The only way to interact with tusk is through the CLI or MCP server. Programs that want to use tusk's task management capabilities must shell out to the binary or speak MCP — there's no way to import and call tusk directly as a library.

## Solution

Two-step milestone:

1. **Package restructure** — Move core packages out of `internal/` to top-level, making them importable.
2. **High-level Client** — Add a convenience `Client` type in the root package that wires everything together.

---

## Package Restructure

### Packages to move

| From | To | Rationale | Dep Tier |
|---|---|---|---|
| `internal/domain` | `domain` | Core types needed by all consumers | 0 (no deps) |
| `internal/config` | `config` | Config types needed by Client | 0 (no deps) |
| `internal/repository` | `repository` | Interface definitions for custom backends | 1 (domain) |
| `internal/filter` | `filter` | Filter parsing for programmatic queries | 1 (domain) |
| `internal/service` | `service` | Business logic — the primary API surface | 2 (domain, repository) |
| `internal/sqlite` | `sqlite` | Default storage implementation | 2 (domain, repository) |
| `internal/inmem` | `inmem` | Config-backed in-memory repos | 2 (config, domain, repository) |

Packages move in dependency-tier order (0 → 1 → 2) so each story only references packages already moved or moving in the same batch.

### Packages that stay in `internal/`

| Package | Rationale |
|---|---|
| `internal/tui` | CLI rendering, not a public API |
| `internal/mcp` | MCP server wiring, not a public API |

### Migration details

- `migrations/` already lives at the top level — no change needed.
- `cmd/tusk/main.go` import paths update from `internal/X` to top-level.
- `internal/tui` and `internal/mcp` update their imports to reference the new top-level packages.
- All test files move with their packages.
- This is a pure refactor — no behavioral changes, all existing tests must pass.

---

## High-level Client

Lives in the root package: `github.com/germanamz/tusk`.

### API surface

```go
package tusk

import (
    "github.com/germanamz/tusk/config"
    "github.com/germanamz/tusk/service"
)

// Client provides access to all tusk services, backed by a SQLite database.
type Client struct {
    Tasks     *service.TaskService
    Tags      *service.TagService
    Relations *service.RelationService
    Projects  *service.ProjectService
    Workflows *service.WorkflowService
    Players   *service.PlayerService
}

// Config holds the configuration for creating a Client.
// Consumers build this struct programmatically — no file loading or env vars.
type Config struct {
    DBPath    string                       // Path to SQLite database file
    Workflows config.WorkflowsConfig       // Workflow definitions
    Projects  config.ProjectsConfig        // Project definitions
    Urgency   config.UrgencyConfig         // Urgency scoring weights
}

// NewClient creates a Client backed by a SQLite database at cfg.DBPath.
// It opens the database, runs migrations, and wires all services.
// Call Close when done.
func NewClient(cfg Config) (*Client, error) { ... }

// Close releases the underlying database connection.
func (c *Client) Close() error { ... }
```

### What NewClient does

1. Ensures the DB directory exists.
2. Opens SQLite with WAL mode, busy_timeout, foreign keys — same as CLI.
3. Runs embedded migrations.
4. Creates SQLite repos (task, tag, annotation, relation, player).
5. Creates in-memory repos (project, workflow) from config.
6. Builds urgency engine from config weights.
7. Wires all services with correct dependencies.
8. Returns the `Client` with services as public fields.

### What NewClient does NOT do

- Load config from files, Viper, or environment variables.
- Set up CLI rendering or MCP server.
- Manage process lifecycle (signals, etc.).

### Default config behavior

When config fields are zero-valued:
- `DBPath` empty — return error (required).
- `Workflows` empty — use builtin kanban workflow.
- `Projects` empty — use builtin default project.
- `Urgency` empty — use default weights.

This mirrors what the CLI does when no config file exists.

### Consumer usage

```go
package main

import (
    "log"
    "github.com/germanamz/tusk"
)

func main() {
    client, err := tusk.NewClient(tusk.Config{
        DBPath: "/tmp/my-tasks.db",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Use services directly
    task, err := client.Tasks.Create(ctx, service.CreateTaskInput{
        Title:   "Build the thing",
        Project: "default",
    })
}
```

### Consumer with custom backend

For consumers who want full control, the moved packages are directly importable:

```go
import (
    "github.com/germanamz/tusk/domain"
    "github.com/germanamz/tusk/repository"
    "github.com/germanamz/tusk/service"
    "github.com/germanamz/tusk/sqlite"
)

// Wire your own repos, skip the Client entirely
```

---

## Scope

### In scope

- Move packages out of `internal/`
- Update all import paths (cmd, internal/tui, internal/mcp, tests)
- Root package `Client` type with `NewClient` / `Close`
- Tests for `NewClient` (open, create task, close)

### Out of scope

- Wrapper methods on `Client` (consumers use service types directly)
- Config file loading in the client
- Any new service methods or domain changes
- MCP or CLI changes beyond import path updates
