# High-level Client — Design Spec

**Date:** 2026-04-08
**Goal:** Convenience `Client` type in the root package that wires up config, DB, and services for library consumers.

---

## Problem

After the package restructure (v0.8), tusk's core packages are importable — but consumers must replicate the wiring from `cmd/tusk/main.go` to use them: open SQLite, run migrations, create 7 repos, build an urgency engine, wire 6 services with the correct dependency graph. This is error-prone and couples consumers to internal wiring details.

## Solution

A single `Client` type in the root `tusk` package that handles all wiring behind `NewClient(Config)`. Two new files, one minor change to export `config.Validate()`.

---

## API Surface

```go
package tusk

import (
    "github.com/germanamz/tusk/config"
    "github.com/germanamz/tusk/service"
)

// Config holds configuration for creating a Client.
// Consumers build this programmatically — no file loading, no env vars.
type Config struct {
    DBPath    string                          // Required. Path to SQLite database file.
    Workflows map[string]config.WorkflowConfig // Optional. Zero → builtin kanban.
    Projects  map[string]config.ProjectConfig  // Optional. Zero → builtin default project.
    Urgency   config.UrgencyConfig             // Optional. Zero → default weights.
}

// Client provides access to all tusk services, backed by a SQLite database.
type Client struct {
    Tasks     *service.TaskService
    Tags      *service.TagService
    Relations *service.RelationService
    Projects  *service.ProjectService
    Workflows *service.WorkflowService
    Players   *service.PlayerService

    store *sqlite.Store // private — released via Close()
}

// NewClient creates a Client backed by a SQLite database at cfg.DBPath.
// It opens the database, runs migrations, and wires all services.
// Call Close when done.
func NewClient(cfg Config) (*Client, error) { ... }

// Close releases the underlying database connection.
func (c *Client) Close() error { return c.store.Close() }
```

Service fields use short names (`Tasks` not `TaskService`) — the `Client` type provides sufficient context.

---

## NewClient Wiring

`NewClient` replicates the wiring logic from `cmd/tusk/main.go`, minus Viper/CLI/TUI/MCP concerns:

1. **Validate** — return error if `DBPath` is empty.
2. **Apply defaults** — if `Workflows` is nil/empty, set builtin kanban workflow; if `Projects` is nil/empty, set builtin default project; if `Urgency` is zero-valued, set default weights. See Default Config section below.
3. **Validate config** — call `config.Config.validate()` logic to check cross-references (projects reference valid workflows, etc.). Since `validate()` is unexported, either export it or inline the same checks.
4. **Ensure directory** — `os.MkdirAll` on the parent directory of `DBPath`.
5. **Open SQLite** — `sqlite.New(cfg.DBPath, migrations.FS)` — WAL mode, busy_timeout, foreign_keys, auto-migrate.
6. **Create SQLite repos** — `TaskRepo`, `AnnotationRepo`, `TagRepo`, `RelationRepo`, `PlayerRepo`.
7. **Create inmem repos** — `ProjectRepository` from config projects, `WorkflowRepository` from config workflows.
8. **Create urgency engine** — `service.NewUrgencyEngine(weights)` mapping `UrgencyConfig` fields to `service.UrgencyWeights`.
9. **Wire services** — same dependency graph as `main.go`:
   - `WorkflowService(workflowRepo, projectRepo)`
   - `TaskService(taskRepo, annotationRepo, relationRepo, tagRepo, projectRepo, workflowSvc, store, urgencyEngine, playerRepo)`
   - `TagService(tagRepo)`
   - `RelationService(relationRepo, taskRepo, store)`
   - `ProjectService(projectRepo)`
   - `PlayerService(playerRepo)`
10. **Return** `&Client{...}` with services as public fields, store as private.

---

## Default Config

When config fields are zero-valued, `NewClient` injects builtin defaults. These are hardcoded constants matching `config/default.toml`:

### Builtin kanban workflow

```go
"kanban": config.WorkflowConfig{
    Statuses: []string{"pending", "active", "completed", "deleted"},
    Transitions: []config.WorkflowTransitionConfig{
        {From: "pending", To: "active"},
        {From: "pending", To: "deleted"},
        {From: "active", To: "completed"},
        {From: "active", To: "pending"},
        {From: "active", To: "deleted"},
        {From: "completed", To: "pending"},
    },
    HighlightStatuses: []string{"active"},
    DimStatuses:       []string{"completed", "deleted"},
}
```

### Builtin default project

```go
"default": config.ProjectConfig{
    Workflow: "kanban",
}
```

### Default urgency weights

```go
config.UrgencyConfig{
    PriorityWeight:    6.0,
    DueWeight:         12.0,
    AgeWeight:         2.0,
    ActiveWeight:      4.0,
    BlockingWeight:    8.0,
    BlockedWeight:     -5.0,
    TagsWeight:        1.0,
    ProjectWeight:     1.0,
    AnnotationsWeight: 1.0,
    WaitingWeight:     -3.0,
}
```

**Note on duplication:** These values duplicate what's in `config/default.toml`. This is acceptable because the Client intentionally bypasses Viper/file-loading. If a future change adds a `config.DefaultWorkflows()` / `config.DefaultUrgencyWeights()` helper, the Client should switch to using it — but adding those helpers is out of scope for this story.

### Zero-value detection for UrgencyConfig

`UrgencyConfig` is a struct of `float64` fields — all zero means "not set". Since all default weights are non-zero (the lowest is `-5.0`), checking `cfg.Urgency == (config.UrgencyConfig{})` reliably detects "use defaults". This works because a consumer who intentionally sets all weights to zero would get a no-op urgency engine, which is a valid (if unusual) choice — and they can do so by setting at least one weight to a tiny non-zero value.

---

## Config Validation

`NewClient` must validate that projects reference workflows that exist in the config (same check as `config.Config.validate()`). Two options:

1. **Export the validate method** — rename `config.Config.validate()` to `config.Config.Validate()`. Pros: single source of truth. Cons: exposes an internal detail on a type the Client doesn't fully use.
2. **Inline the validation** — replicate the cross-reference check in `NewClient`. Pros: no changes to config package. Cons: duplication.

**Decision:** Option 1 — export `Validate()`. It's a one-line change, and the validation logic belongs with the config types. The Client constructs a `config.Config{Workflows: cfg.Workflows, Projects: cfg.Projects}` and calls `Validate()` on it. The method only checks workflow/project cross-references — it ignores Storage, TUI, and MCP sections.

---

## Tests

Four integration tests in `client_test.go`, all using `t.TempDir()` for isolated DBs:

### TestNewClient_CreateAndGetTask

- `NewClient` with only `DBPath` (temp dir).
- Create a task via `client.Tasks.Create()` with title and project "default".
- Retrieve via `client.Tasks.GetByShortID()`.
- Assert title matches.
- `client.Close()`.

### TestNewClient_DefaultConfig

- `NewClient` with only `DBPath`.
- `client.Projects.List()` — assert "default" project present.
- `client.Workflows.List()` — assert "kanban" workflow present.
- `client.Close()`.

### TestNewClient_EmptyDBPath

- `NewClient(Config{})` — assert non-nil error returned.

### TestClose

- `NewClient`, then `Close()`.
- Attempt `client.Tasks.Create()` — assert it fails (DB connection closed).

No mocks. Real SQLite via temp files — fast and proves actual wiring.

---

## File Layout

### Files to create

| File | Contents |
|------|----------|
| `client.go` | `Config`, `Client`, `NewClient`, `Close`, default constants |
| `client_test.go` | 4 integration tests |

### Files to modify

| File | Change |
|------|--------|
| `config/config.go` | Export `validate()` → `Validate()` |

### Estimated size

~120 lines for `client.go`, ~80 lines for `client_test.go`.

---

## Scope

### In scope

- `Config` struct reusing `config.WorkflowConfig`, `config.ProjectConfig`, `config.UrgencyConfig`
- `Client` struct with 6 public service fields and private store
- `NewClient` wiring: validate → defaults → mkdir → SQLite → repos → services
- `Close` for lifecycle cleanup
- Export `config.Validate()`
- 4 integration tests

### Out of scope

- Wrapper methods on Client (consumers call services directly)
- Config file/env var loading in the Client
- `config.DefaultWorkflows()` or similar helpers (future cleanup)
- Changes to existing services, repos, or domain
- Refactoring `main.go` to use Client
- Any new service methods or domain changes
