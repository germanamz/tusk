# MCP Server Core — Design Spec

## Overview

Expose tusk's task management capabilities via MCP protocol for AI agent integration. Stdio transport, 13 tools mapping 1:1 to service methods, 3 resource templates for read-only state access. Uses `github.com/mark3labs/mcp-go` as the MCP SDK.

## Scope

Roadmap v0.3 — MCP Server Core initiative. Covers:
- MCP server with stdio transport
- Tool registration framework
- 9 task tools, 2 relation tools, 2 project tools
- 3 resource templates (tasks, projects, workflows)
- Optimistic locking via version passing

Out of scope: SSE transport (v0.5), tag management tools, urgency scoring.

---

## Package Structure

```
internal/mcp/
  server.go      — Server struct, New(), Serve(), tool & resource registration
  tools.go       — tool handler methods on Server
  resources.go   — resource handler methods on Server
  errors.go      — mapError() translating domain errors to MCP tool errors
```

Follows the same pattern as `internal/tui/` — a struct holding service dependencies with handler methods.

---

## Server Struct

```go
type Server struct {
    taskSvc     *service.TaskService
    tagSvc      *service.TagService
    relationSvc *service.RelationService
    projectSvc  *service.ProjectService
    server      *server.MCPServer
}
```

`New()` accepts the same service dependencies as `tui.New()`, creates the `mcp-go` server with capabilities, and registers all tools and resources. `Serve()` starts stdio transport via `server.ServeStdio()`.

Server options:
- `server.WithToolCapabilities(false)` — tools, no list-changed notifications
- `server.WithResourceCapabilities(false, false)` — resources, no subscribe/list-changed
- `server.WithInstructions(...)` — brief description of tusk capabilities for agent orientation
- `server.WithRecovery()` — panic recovery in handlers

---

## Tool Definitions

### Task Tools (9)

| Tool | Required Params | Optional Params | Service Call | Returns |
|------|----------------|-----------------|-------------|---------|
| `tusk_task_create` | `title` | `description`, `priority`, `project`, `parent`, `tags[]`, `due`, `wait_until` | `TaskService.Create` + `TagService.AssignToTask` | task JSON |
| `tusk_task_list` | — | `status[]`, `priority_min`, `priority_max`, `project`, `tags[]`, `exclude_tags[]`, `due_after`, `due_before`, `parent`, `root` | `TaskService.List` + `TagService.GetTaskTagsBatch` | array of task JSON |
| `tusk_task_get` | `short_id` | — | `GetByShortID` + tags + relations + annotations | full task JSON (rich) |
| `tusk_task_modify` | `short_id`, `version` | `title`, `description`, `priority`, `project`, `parent`, `due`, `wait_until`, `add_tags[]`, `remove_tags[]` | `TaskService.Update` + tag assign/remove | updated task JSON |
| `tusk_task_start` | `short_id`, `version` | — | `TaskService.Start` | updated task JSON |
| `tusk_task_done` | `short_id`, `version` | — | `TaskService.Complete` | updated task JSON |
| `tusk_task_delete` | `short_id`, `version` | — | `TaskService.Delete` | updated task JSON |
| `tusk_task_annotate` | `short_id`, `body` | — | `TaskService.Annotate` | annotation JSON |
| `tusk_task_tree` | — | `short_id`, `include_deleted` | `List`/`GetDescendants` + tree build | nested JSON: `{task, children: [{task, children: [...]}]}` |

### Relation Tools (2)

| Tool | Required Params | Service Call | Returns |
|------|----------------|-------------|---------|
| `tusk_relation_add` | `source`, `target`, `type` (enum: blocks, relates_to, duplicates) | `RelationService.Add` | relation JSON |
| `tusk_relation_remove` | `source`, `target`, `type` | `RelationService.Remove` | success message |

### Project Tools (2)

| Tool | Required Params | Optional Params | Service Call | Returns |
|------|----------------|-----------------|-------------|---------|
| `tusk_project_list` | — | — | `ProjectService.List` | array of project JSON |
| `tusk_project_create` | `name` | `description` | `ProjectService.Create` | project JSON |

### Design Decisions

- **`tusk_task_get` is the rich endpoint** — returns tags, relations, and annotations in one call to minimize agent round-trips.
- **`tusk_task_list` uses structured params** — agents work better with explicit fields than a DSL. Dates accept ISO 8601.
- **`tusk_task_modify` uses `add_tags`/`remove_tags`** — cleaner than `+tag`/`-tag` CLI syntax for structured input.
- **`tusk_task_tree` reuses TUI tree-building logic** — extracted to a shared function callable from both TUI and MCP.
- **Version required on all mutations** — enforces end-to-end optimistic locking.
- **No tag management tools** — tags are auto-created on assignment via `FindOrCreate`, and tag CRUD is admin work better done via CLI.

---

## Resource Templates

| Resource URI | Handler | Returns |
|---|---|---|
| `tusk://tasks/{short_id}` | `GetByShortID` + tags + relations + annotations | Same rich format as `tusk_task_get` |
| `tusk://projects/{name}` | `ProjectService.GetByName` | Project JSON with settings |
| `tusk://projects/{name}/workflow` | `WorkflowService.GetStatuses` + transitions | Workflow statuses and allowed transitions |

Resources are read-only views. The task resource matches `tusk_task_get` output for consistency. The workflow resource lets agents discover valid transitions before attempting them.

---

## Error Handling

A single `mapError()` function translates domain errors to `mcp.NewToolResultError`:

| Domain Error | MCP Error Text |
|---|---|
| `ErrNotFound` | `"not found: <context>"` |
| `ErrConflict` | `"version conflict: task was modified, re-fetch and retry"` |
| `ErrInvalidTransition` | `"invalid status transition: <from> → <to>"` |
| `ErrCyclicBlock` | `"would create a dependency cycle"` |
| `ErrCyclicParent` | `"would create a parent-child cycle"` |
| `ErrDuplicateRelation` | `"relation already exists"` |
| `ErrSourceNotFound` | `"source task not found"` |
| `ErrTargetNotFound` | `"target task not found"` |
| anything else | `"internal error: <err.Error()>"` |

All domain errors return `mcp.NewToolResultError(msg), nil`. The Go `error` return is reserved for infrastructure failures (DB connection lost, etc.).

---

## CLI Integration

New Cobra command:

```
tusk mcp serve    — start MCP server with stdio transport
```

The `serve` command:
1. Uses existing service dependencies already injected into `App`
2. Constructs `mcp.New(taskSvc, tagSvc, relationSvc, projectSvc)`
3. Calls `Server.Serve()` which blocks on `server.ServeStdio()`

No changes to `cmd/tusk/main.go` — DI wiring is identical. The MCP server consumes the same services the TUI already constructs.

SSE transport is deferred to v0.5. The `--transport` flag can be added later without breaking changes.

---

## Dependencies

New Go module dependency:
- `github.com/mark3labs/mcp-go` — MCP SDK providing server, tool/resource registration, stdio transport

---

## Testing Strategy

- **Unit tests per tool handler** — mock services, verify correct param extraction, service calls, and response format
- **Unit tests for `mapError()`** — verify all sentinel error mappings
- **Integration tests** — spin up real SQLite + services, call tool handlers, verify end-to-end behavior
- **E2E tests** — launch `tusk mcp serve` as subprocess, send JSON-RPC over stdin, verify stdout responses
