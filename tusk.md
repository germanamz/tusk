# Tusk

**A concurrent-safe task management tool with built-in MCP server, written in Go.**

Tusk combines the speed and CLI ergonomics of TaskWarrior with the structured hierarchy of Linear and the workflow flexibility of Jira — without the bloat of either. It ships as a single binary with SQLite persistence by default and exposes every capability through both a terminal interface and an MCP (Model Context Protocol) server, so AI agents can manage tasks alongside humans.

Beyond basic task management, tusk serves as a **player state manager** — tracking which player (human or AI agent) is working on which task at any given time. Players self-register and claim tasks, preventing overlapping work and race conditions. A built-in task queue lets agents pop the next highest-priority available task in a single atomic operation, minimizing coordination overhead and token usage.

Licensed under **Apache 2.0**.

---

## Why Tusk

Existing tools force a choice: TaskWarrior is fast but flat (no real hierarchy, no typed relations, file-level locking that breaks under concurrency). Jira and Linear are powerful but browser-bound and opaque to automation. None of them ship with an MCP interface.

Tusk occupies the gap:

- **Single binary** — no runtime, no daemon, no browser. Install and go.
- **Hierarchical tasks** — optional parent-child nesting to arbitrary depth. An "epic" is just a task with children.
- **Typed relations** — `blocks`, `relates_to`, `duplicates` as first-class edges between tasks. Cycle detection on `blocks`.
- **Configurable workflows** — define allowed status transitions per project. Default: `pending → active → completed | deleted`.
- **Concurrent-safe** — optimistic locking via version fields. Two MCP calls modifying the same task won't silently clobber each other.
- **Pluggable storage** — SQLite out of the box, but the repository layer is an interface. Swap in PostgreSQL, a JSON file, or a remote API without touching the service layer.
- **Built-in MCP server** — every CLI command is also an MCP tool. AI agents can create, query, modify, and relate tasks through the same service layer humans use.
- **Player state management** — players (humans or agents) self-register and claim tasks. Prevents overlapping work. `tusk pop` atomically picks and claims the highest-urgency available task.

---

## Architecture

```
┌──────────────────────────────────────────────┐
│              Interface Layer                  │
│  ┌──────┐   ┌────────────┐   ┌───────────┐  │
│  │ TUI  │   │ MCP Server │   │ REST API  │  │
│  └──┬───┘   └─────┬──────┘   └─────┬─────┘  │
└─────┼─────────────┼────────────────┼─────────┘
      │             │                │
      ▼             ▼                ▼
┌──────────────────────────────────────────────┐
│              Service Layer                   │
│  ┌─────────────┐ ┌──────────────┐            │
│  │ TaskService  │ │ ProjectService│           │
│  └─────────────┘ └──────────────┘            │
│  ┌────────────────┐ ┌──────────────────┐     │
│  │WorkflowService │ │ UrgencyEngine    │     │
│  └────────────────┘ └──────────────────┘     │
└──────────────────────┬───────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────┐
│           Repository Layer (interfaces)      │
│  ┌──────────┐ ┌──────────────┐ ┌──────────┐ │
│  │ TaskRepo │ │ RelationRepo │ │ProjectRepo│ │
│  └──────────┘ └──────────────┘ └──────────┘ │
└──────────────────────┬───────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────┐
│          Storage Implementations             │
│  ┌────────┐  ┌────────────┐  ┌───────────┐  │
│  │ SQLite │  │ PostgreSQL │  │ JSON File │  │
│  └────────┘  └────────────┘  └───────────┘  │
└──────────────────────────────────────────────┘
```

### Layer responsibilities

**Interface layer** — translates external protocols into service method calls. No business logic lives here. The TUI parses flags and renders output. The MCP server maps tool names to service methods and serializes results. The REST API (optional, future) does the same over HTTP.

**Service layer** — all business logic. Validation, workflow enforcement, cycle detection on `blocks` relations, urgency scoring, recurrence generation. Services accept repository interfaces via constructor injection and never import a concrete storage implementation.

**Repository layer** — Go interfaces defining CRUD + query operations. Each storage backend implements these interfaces. The repository is the transaction boundary: a single repository method call is atomic.

**Storage implementations** — concrete adapters. SQLite (default, WAL mode), PostgreSQL (future), JSON file (for portable/offline use). Each implementation handles its own connection pooling, migration, and locking semantics.

### Dependency rule

Dependencies flow downward only. The interface layer imports services. Services import repository interfaces. Repository implementations import nothing from the layers above. This makes it possible to swap any layer independently.

---

## Data Model

### Task

The central entity. Every trackable item is a Task.

| Field             | Type                | Description                                                  |
| ----------------- | ------------------- | ------------------------------------------------------------ |
| `id`              | UUID                | Primary key. Immutable, globally unique.                     |
| `short_id`        | string              | First 8 hex chars of UUID. Unique, used in CLI.              |
| `parent_id`       | UUID (nullable)     | Self-referential FK for hierarchy.                           |
| `project_id`      | UUID (nullable)     | FK to Project.                                               |
| `title`           | string              | Required. Short summary.                                     |
| `description`     | string              | Optional. Longer context, supports markdown.                 |
| `status`          | string              | Validated against project workflow. Default: `pending`.      |
| `priority`        | int (0–4)           | 0=none, 1=low, 2=medium, 3=high, 4=urgent.                   |
| `version`         | int                 | Optimistic lock. Starts at 1, incremented on every write.    |
| `due_at`          | datetime (nullable) | When the task is due.                                        |
| `wait_until`      | datetime (nullable) | Hidden from default views until this time.                   |
| `recurrence_rule` | string (nullable)   | RFC 5545 RRULE. Service creates next instance on completion. |
| `uda`             | JSON (nullable)     | User Defined Attributes. Schemaless, validated per project.  |
| `claimed_by`      | string (nullable)   | FK to Player. Who has reserved/is working on this task.      |
| `claimed_at`      | datetime (nullable) | When the claim was made.                                     |
| `created_at`      | datetime            | Set once on creation.                                        |
| `modified_at`     | datetime            | Updated on every write.                                      |

**Identity model**: UUID is canonical. `short_id` is derived (first 8 hex chars), stable across the task's lifetime, and used in all CLI interactions. If a collision is detected at creation time, the generator extends to 9+ chars. Users never need to type or see full UUIDs unless they want to.

**Hierarchy**: A task with `parent_id = NULL` is top-level. There are no forced types (no "epic" vs "story" distinction at the schema level). A task is an "epic" if it has children; a task is a "subtask" if it has a parent. Nesting depth is unlimited but the TUI indents, so practical depth is ~4 levels.

**Completion propagation** (configurable per project): when all children of a parent complete, the parent auto-transitions to `completed`. This can be disabled for cases where the parent represents ongoing work.

### Player

A human or AI agent that interacts with tusk. Players self-register on first contact.

| Field           | Type     | Description                                            |
| --------------- | -------- | ------------------------------------------------------ |
| `id`            | string   | Primary key. Self-declared unique identifier.          |
| `type`          | string   | `human` or `agent`.                                    |
| `registered_at` | datetime | First seen.                                            |
| `last_seen_at`  | datetime | Updated on every action.                               |

**Registration**: Players announce themselves by providing an ID on any action (CLI `--player` flag, MCP tool parameter). If the ID is new, the player is auto-registered. No predefined roster needed.

**Claim mechanics**: Players claim tasks to signal intent and prevent overlapping work.

- **Explicit claim** — `tusk claim <id>` reserves a task. The task's `claimed_by` is set to the player's ID.
- **Auto-claim on start** — `tusk start <id>` claims the task if unclaimed. If already claimed by someone else, returns `ErrTaskClaimed`.
- **Release** — `tusk release <id>` clears the claim. Also auto-released on `done` and `delete`.
- **No force-steal, no TTL** — if a player goes stale, managing that is the consumer's responsibility.

**Visibility**: `claimed_by` and `claimed_at` are included in all task responses. Filter support: `claimed_by:<player_id>`, `unclaimed:true`. `tusk available` lists unclaimed + actionable + unblocked tasks.

**Task queue**: `tusk pop` atomically finds the highest-urgency unclaimed unblocked task, claims it for the calling player, and returns it. One operation instead of list → filter → pick → claim. Critical for minimizing agent token usage.

### Annotation

Timestamped notes attached to a task. Immutable after creation.

| Field        | Type     | Description              |
| ------------ | -------- | ------------------------ |
| `id`         | UUID     | Primary key.             |
| `task_id`    | UUID     | FK to Task.              |
| `body`       | string   | The note content.        |
| `created_at` | datetime | When the note was added. |

### Relation

Typed directed edge between two tasks. Separate from parent-child hierarchy.

| Field           | Type     | Description                                   |
| --------------- | -------- | --------------------------------------------- |
| `id`            | UUID     | Primary key.                                  |
| `source_id`     | UUID     | FK to Task. The task that holds the relation. |
| `target_id`     | UUID     | FK to Task. The task being pointed to.        |
| `relation_type` | string   | One of: `blocks`, `relates_to`, `duplicates`. |
| `created_at`    | datetime | When the relation was created.                |

**Inverse relations** (`blocked_by`, `related_to`, `duplicated_by`) are derived at query time by swapping source and target. No duplicate rows.

**Cycle detection**: before inserting a `blocks` relation from A → B, the service runs a DFS from B through existing `blocks` edges. If it reaches A, the insert is rejected with an error. This prevents deadlock situations where tasks mutually block each other.

### Project

A container for tasks with its own workflow definition.

| Field              | Type     | Description                                                     |
| ------------------ | -------- | --------------------------------------------------------------- |
| `id`               | UUID     | Primary key.                                                    |
| `name`             | string   | Unique. Used in CLI filters (e.g. `tusk list project:backend`). |
| `description`      | string   | Optional.                                                       |
| `default_workflow` | string   | Name of the workflow to use for tasks in this project.          |
| `created_at`       | datetime | When the project was created.                                   |

### Tag

Flat labels for cross-cutting categorization.

| Field   | Type              | Description                                     |
| ------- | ----------------- | ----------------------------------------------- |
| `id`    | UUID              | Primary key.                                    |
| `name`  | string            | Unique. Used in CLI (e.g. `+bug`, `+frontend`). |
| `color` | string (nullable) | Hex color for TUI rendering.                    |

### TagAssignment

Join table between Task and Tag. Composite key: `(task_id, tag_id)`.

### Workflow

A named set of statuses and allowed transitions, scoped to a project.

| Field        | Type   | Description                                            |
| ------------ | ------ | ------------------------------------------------------ |
| `id`         | UUID   | Primary key.                                           |
| `project_id` | UUID   | FK to Project.                                         |
| `name`       | string | Identifier (e.g. `default`, `kanban`, `bug-tracking`). |
| `statuses`   | JSON   | Ordered list of valid status strings.                  |

### WorkflowTransition

Defines which status changes are allowed.

| Field         | Type   | Description         |
| ------------- | ------ | ------------------- |
| `id`          | UUID   | Primary key.        |
| `workflow_id` | UUID   | FK to Workflow.     |
| `from_status` | string | Source status.      |
| `to_status`   | string | Destination status. |

The default workflow ships with:

```
statuses: ["pending", "active", "completed", "deleted"]

transitions:
  pending  → active
  pending  → deleted
  active   → completed
  active   → pending
  active   → deleted
  completed → pending   (reopen)
```

Any transition not in this table is rejected by WorkflowService.

---

## Concurrency Model

Tusk must handle concurrent access safely because:

1. **Scripting** — shell scripts may fire multiple `tusk` commands in parallel.
2. **MCP** — AI agents issue rapid, often parallel tool calls.
3. **Future multi-user** — network repository backends serve multiple clients.

### Optimistic locking

Every mutable entity carries a `version` field. The update path is:

```sql
UPDATE tasks
SET title = ?, status = ?, ..., version = version + 1, modified_at = NOW()
WHERE id = ? AND version = ?;
```

If `rows_affected == 0`, someone else wrote first. The repository returns `ErrConflict`.

### Retry policy

The service layer wraps mutations with a configurable retry policy:

| Caller          | Max retries | Backoff                                     |
| --------------- | ----------- | ------------------------------------------- |
| Interactive CLI | 0           | None — surface conflict to user immediately |
| MCP server      | 3           | Jitter: 10ms, 25ms, 50ms                    |
| Batch/script    | 5           | Jitter: 10ms, 25ms, 50ms, 100ms, 200ms      |

On retry, the service re-reads the entity, reapplies the intended change, and writes again with the fresh version. If the change conflicts semantically (e.g. both callers changed the title to different values), it surfaces the conflict.

### SQLite specifics

- **WAL mode** enabled at database open. Allows concurrent readers.
- **`busy_timeout = 5000`** — writers retry for 5 seconds before failing.
- **Single writer** — SQLite serializes writes, so the optimistic lock is checked inside the write transaction. No phantom reads.

### MCP version passing

MCP tool responses include the task's current `version`. An agent that reads a task and later modifies it passes the version back:

```json
// MCP tool call
{
  "tool": "tusk_task_modify",
  "input": {
    "short_id": "a3f8b2c1",
    "version": 3,
    "status": "active"
  }
}
```

This enables end-to-end optimistic locking even when reads and writes happen across separate MCP calls.

---

## Urgency Scoring

Adapted from TaskWarrior's urgency algorithm. Each task gets a numeric urgency score computed from weighted factors:

| Factor        | Weight | Calculation                                         |
| ------------- | ------ | --------------------------------------------------- |
| Priority      | 6.0    | `priority * 1.5` (0, 1.5, 3, 4.5, 6)                |
| Due date      | 12.0   | Sigmoid curve: rises sharply as due date approaches |
| Age           | 2.0    | `days_since_creation / 365 * 2` (caps at 2.0)       |
| Active status | 4.0    | 4.0 if `status == active`, else 0                   |
| Blocking      | 8.0    | 8.0 if task blocks other tasks, else 0              |
| Blocked       | -5.0   | -5.0 if task is blocked by incomplete tasks         |
| Tags          | 1.0    | 1.0 per matching "urgency tag" (configurable)       |
| Project       | 1.0    | 1.0 if task belongs to a project                    |
| Annotations   | 1.0    | 0.5 per annotation (caps at 1.0)                    |
| Waiting       | -3.0   | -3.0 if `wait_until` is in the future               |

Total urgency determines default sort order. Users can override with explicit `--sort` flags. The weights are configurable in `~/.config/tusk/config.toml`.

---

## CLI Interface

### Core commands

```bash
# Create
tusk add "Implement auth middleware" project:backend +api priority:3
tusk add "Write tests for auth" parent:a3f8b2c1 +testing

# Read
tusk list                          # all pending tasks, sorted by urgency
tusk list project:backend +api     # filtered by project and tag
tusk list status:active            # filter by status
tusk info a3f8b2c1                 # full detail view of a task
tusk tree                          # hierarchical view of all tasks

# Update
tusk modify a3f8b2c1 priority:4 +urgent
tusk start a3f8b2c1               # shorthand: pending → active
tusk done a3f8b2c1                # shorthand: active → completed
tusk delete a3f8b2c1              # → deleted

# Annotate
tusk annotate a3f8b2c1 "Blocked by upstream API changes"

# Relations
tusk link a3f8b2c1 blocks b7c9d4e2
tusk link a3f8b2c1 relates_to c5e1f3a8
tusk unlink a3f8b2c1 blocks b7c9d4e2

# Projects
tusk project add backend "Backend services"
tusk project list

# Workflow
tusk workflow show backend
tusk workflow add-status backend "in_review"
tusk workflow add-transition backend active in_review

# Player management
tusk player register german --type human  # explicit registration
tusk claim a3f8b2c1 --player german       # reserve a task
tusk release a3f8b2c1                     # release claim
tusk available                            # unclaimed + actionable + unblocked
tusk pop --player german                  # atomic: pick highest-urgency + claim

# Undo
tusk undo                          # reverts last mutation

# Export / sync
tusk export --format json          # full dump
tusk export --format csv           # flat export
```

### Filter syntax

Inspired by TaskWarrior but extended:

```bash
tusk list project:backend +api -docs priority:3..4 due:today..friday
```

- `project:name` — project filter
- `+tag` — include tag, `-tag` — exclude tag
- `field:value` — exact match
- `field:min..max` — range
- `due:today`, `due:tomorrow`, `due:thisweek` — relative dates
- `status:pending,active` — comma-separated OR
- `parent:short_id` — direct children only
- `tree:short_id` — all descendants
- `claimed_by:player_id` — tasks claimed by a specific player
- `unclaimed:true` — tasks with no active claim

---

## MCP Server

The MCP server exposes the same capabilities as the CLI. It starts alongside the CLI or as a standalone daemon:

```bash
tusk mcp serve                    # stdio transport (for IDE integration)
tusk mcp serve --transport sse --port 8080   # SSE transport
```

### Tool definitions

Every tool maps 1:1 to a service method:

| MCP Tool               | Service Method           | Description             |
| ---------------------- | ------------------------ | ----------------------- |
| `tusk_task_create`     | TaskService.Create       | Create a new task       |
| `tusk_task_list`       | TaskService.List         | List/filter tasks       |
| `tusk_task_get`        | TaskService.GetByShortID | Get a single task       |
| `tusk_task_modify`     | TaskService.Update       | Modify task fields      |
| `tusk_task_start`      | TaskService.Start        | Transition to active    |
| `tusk_task_done`       | TaskService.Complete     | Transition to completed |
| `tusk_task_delete`     | TaskService.Delete       | Transition to deleted   |
| `tusk_task_annotate`   | TaskService.Annotate     | Add annotation          |
| `tusk_task_tree`       | TaskService.Tree         | Get hierarchical view   |
| `tusk_relation_add`    | RelationService.Add      | Create a relation       |
| `tusk_relation_remove` | RelationService.Remove   | Remove a relation       |
| `tusk_project_list`    | ProjectService.List      | List projects           |
| `tusk_project_create`  | ProjectService.Create    | Create project          |
| `tusk_player_register` | PlayerService.Register   | Register a player       |
| `tusk_task_claim`      | TaskService.Claim        | Claim a task            |
| `tusk_task_release`    | TaskService.Release      | Release a claim         |
| `tusk_task_available`  | TaskService.Available    | List available tasks    |
| `tusk_task_pop`        | TaskService.Pop          | Claim next best task    |

### MCP resource support

Tasks, projects, and workflow definitions are also exposed as MCP resources for agents that prefer reading state via resources rather than tool calls:

```
tusk://tasks/{short_id}
tusk://projects/{name}
tusk://projects/{name}/workflow
```

---

## Project Structure

```
tusk/
├── cmd/
│   └── tusk/
│       └── main.go              # Entry point, DI wiring
├── internal/
│   ├── domain/                  # Core types (no dependencies)
│   │   ├── task.go
│   │   ├── relation.go
│   │   ├── project.go
│   │   ├── workflow.go
│   │   ├── tag.go
│   │   ├── annotation.go
│   │   ├── errors.go            # ErrConflict, ErrNotFound, etc.
│   │   └── filter.go
│   ├── service/                 # Business logic
│   │   ├── task.go
│   │   ├── project.go
│   │   ├── workflow.go
│   │   ├── urgency.go
│   │   └── relation.go
│   ├── repository/              # Interfaces
│   │   ├── task.go
│   │   ├── relation.go
│   │   ├── project.go
│   │   └── tag.go
│   ├── sqlite/                  # SQLite implementation
│   │   ├── store.go             # Connection, migrations
│   │   ├── task.go
│   │   ├── relation.go
│   │   ├── project.go
│   │   └── tag.go
│   ├── mcp/                     # MCP server
│   │   ├── server.go
│   │   ├── tools.go             # Tool definitions
│   │   └── resources.go         # Resource definitions
│   └── tui/                     # Terminal interface
│       ├── app.go
│       ├── commands.go
│       ├── filter.go
│       ├── render.go
│       └── tree.go
├── migrations/                  # SQL migration files
│   ├── 001_initial.up.sql
│   └── 001_initial.down.sql
├── config/
│   └── default.toml             # Default configuration
├── go.mod
├── go.sum
├── LICENSE                      # Apache 2.0
├── README.md
└── Makefile
```

---

## Configuration

Stored in `~/.config/tusk/config.toml`:

```toml
[storage]
backend = "sqlite"                    # sqlite | postgres | json
path = "~/.local/share/tusk/tusk.db"  # SQLite path

[storage.postgres]                    # only if backend = postgres
dsn = "postgres://user:pass@host/tusk"

[urgency]
priority_weight = 6.0
due_weight = 12.0
age_weight = 2.0
blocking_weight = 8.0
blocked_weight = -5.0

[mcp]
transport = "stdio"                   # stdio | sse
port = 8080                           # only for SSE
retry_max = 3
retry_base_ms = 10

[tui]
date_format = "2006-01-02"
color = true
tree_indent = 2
default_sort = "urgency"
```

---

## Initial SQLite Migration

```sql
-- 001_initial.up.sql

PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;

CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    default_workflow TEXT NOT NULL DEFAULT 'default',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    short_id TEXT NOT NULL UNIQUE,
    parent_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    due_at TEXT,
    wait_until TEXT,
    recurrence_rule TEXT,
    uda TEXT DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    modified_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_tasks_short_id ON tasks(short_id);
CREATE INDEX idx_tasks_parent_id ON tasks(parent_id);
CREATE INDEX idx_tasks_project_id ON tasks(project_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_due_at ON tasks(due_at);
CREATE INDEX idx_tasks_wait_until ON tasks(wait_until);

CREATE TABLE annotations (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_annotations_task_id ON annotations(task_id);

CREATE TABLE relations (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    relation_type TEXT NOT NULL CHECK (relation_type IN ('blocks', 'relates_to', 'duplicates')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(source_id, target_id, relation_type)
);

CREATE INDEX idx_relations_source ON relations(source_id);
CREATE INDEX idx_relations_target ON relations(target_id);

CREATE TABLE tags (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    color TEXT
);

CREATE TABLE tag_assignments (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, tag_id)
);

CREATE INDEX idx_tag_assignments_tag ON tag_assignments(tag_id);

CREATE TABLE workflows (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    statuses TEXT NOT NULL DEFAULT '["pending","active","completed","deleted"]',
    UNIQUE(project_id, name)
);

CREATE TABLE workflow_transitions (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    from_status TEXT NOT NULL,
    to_status TEXT NOT NULL,
    UNIQUE(workflow_id, from_status, to_status)
);

-- Default global project with default workflow
INSERT INTO projects (id, name, description, default_workflow)
VALUES ('00000000-0000-0000-0000-000000000000', '_default', 'Global default project', 'default');

INSERT INTO workflows (id, project_id, name, statuses)
VALUES ('00000000-0000-0000-0000-000000000001',
        '00000000-0000-0000-0000-000000000000',
        'default',
        '["pending","active","completed","deleted"]');

INSERT INTO workflow_transitions (id, workflow_id, from_status, to_status) VALUES
    ('00000000-0000-0000-0000-100000000001', '00000000-0000-0000-0000-000000000001', 'pending', 'active'),
    ('00000000-0000-0000-0000-100000000002', '00000000-0000-0000-0000-000000000001', 'pending', 'deleted'),
    ('00000000-0000-0000-0000-100000000003', '00000000-0000-0000-0000-000000000001', 'active', 'completed'),
    ('00000000-0000-0000-0000-100000000004', '00000000-0000-0000-0000-000000000001', 'active', 'pending'),
    ('00000000-0000-0000-0000-100000000005', '00000000-0000-0000-0000-000000000001', 'active', 'deleted'),
    ('00000000-0000-0000-0000-100000000006', '00000000-0000-0000-0000-000000000001', 'completed', 'pending');
```

---

## Go Interface Definitions

### Repository interfaces

```go
// repository/task.go
package repository

type TaskRepository interface {
    Create(ctx context.Context, task *domain.Task) error
    GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error)
    GetByShortID(ctx context.Context, shortID string) (*domain.Task, error)
    Update(ctx context.Context, task *domain.Task) error // version check
    Delete(ctx context.Context, id uuid.UUID, version int) error
    List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error)
    GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error)
    GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error)
}

type RelationRepository interface {
    Create(ctx context.Context, rel *domain.Relation) error
    Delete(ctx context.Context, id uuid.UUID) error
    GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
    GetBlocking(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
    GetBlockedBy(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
    Exists(ctx context.Context, sourceID, targetID uuid.UUID, relType string) (bool, error)
}

type ProjectRepository interface {
    Create(ctx context.Context, project *domain.Project) error
    GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)
    GetByName(ctx context.Context, name string) (*domain.Project, error)
    List(ctx context.Context) ([]*domain.Project, error)
    Update(ctx context.Context, project *domain.Project) error
    Delete(ctx context.Context, id uuid.UUID) error
}

type TagRepository interface {
    Create(ctx context.Context, tag *domain.Tag) error
    GetByName(ctx context.Context, name string) (*domain.Tag, error)
    List(ctx context.Context) ([]*domain.Tag, error)
    AssignToTask(ctx context.Context, taskID, tagID uuid.UUID) error
    RemoveFromTask(ctx context.Context, taskID, tagID uuid.UUID) error
    GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error)
}

type WorkflowRepository interface {
    GetByProjectAndName(ctx context.Context, projectID uuid.UUID, name string) (*domain.Workflow, error)
    GetTransitions(ctx context.Context, workflowID uuid.UUID) ([]*domain.WorkflowTransition, error)
    Create(ctx context.Context, wf *domain.Workflow) error
    AddTransition(ctx context.Context, t *domain.WorkflowTransition) error
}

type PlayerRepository interface {
    Register(ctx context.Context, player *domain.Player) error
    GetByID(ctx context.Context, id string) (*domain.Player, error)
    UpdateLastSeen(ctx context.Context, id string) error
    List(ctx context.Context) ([]*domain.Player, error)
}

type AnnotationRepository interface {
    Create(ctx context.Context, ann *domain.Annotation) error
    GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Annotation, error)
    Delete(ctx context.Context, id uuid.UUID) error
}
```

### Domain errors

```go
// domain/errors.go
package domain

var (
    ErrNotFound       = errors.New("not found")
    ErrConflict       = errors.New("version conflict")
    ErrCyclicBlock    = errors.New("relation would create a cycle in blocks graph")
    ErrInvalidTransition = errors.New("status transition not allowed by workflow")
    ErrDuplicateRelation = errors.New("relation already exists")
    ErrTaskClaimed       = errors.New("task is already claimed by another player")
)
```

---

## Design Decisions Log

| Decision          | Chosen                               | Rejected                                | Rationale                                                                                                    |
| ----------------- | ------------------------------------ | --------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| Identity          | UUID + 8-char short ID               | Auto-incrementing integer (TaskWarrior) | Incrementing IDs shift on completion, breaking scripts and MCP references.                                   |
| Hierarchy         | Optional `parent_id` on Task         | Typed hierarchy (Epic/Story/Task)       | Forced types add schema complexity without CLI benefit. A task is an epic if it has children.                |
| Relations         | Separate `relations` table, directed | Embedded UUID list in Task              | Typed relations enable cycle detection and richer queries. Separate table is normalized.                     |
| Inverse relations | Derived at query time                | Stored as duplicate rows                | Avoids consistency bugs. One row per logical relation.                                                       |
| Workflow          | Per-project state machine            | Global enum                             | Projects have different needs. A kanban board and a bug tracker don't share status sets.                     |
| Status field type | String (validated by workflow)       | Go enum/iota                            | Enum at Go level would require recompilation to add statuses. String + workflow validation is flexible.      |
| Concurrency       | Optimistic locking (`version`)       | Pessimistic locking (mutexes)           | Pessimistic locking doesn't work across process boundaries (MCP, scripts). Optimistic locking is portable.   |
| Storage default   | SQLite (WAL)                         | PostgreSQL                              | Single-binary philosophy. No external dependencies for default use case.                                     |
| UDA               | JSON column                          | Separate key-value table                | Simpler queries, atomic read/write of all UDAs per task.                                                     |
| Recurrence        | RFC 5545 RRULE string                | Custom DSL                              | Industry standard. Libraries exist for parsing. No invention needed.                                         |
| MCP transport     | stdio + SSE                          | WebSocket                               | stdio for IDE integration (standard MCP), SSE for network access. WebSocket adds complexity without benefit. |
| Player identity   | Self-declared string ID              | UUID or named entity with resolution    | Consumers own their naming. Tusk shouldn't add name resolution burden for identifiers it doesn't control.    |
| Claim conflict    | Hard reject (`ErrTaskClaimed`)       | Force-steal, TTL-based expiry           | Stale player management is the consumer's concern. Keeps tusk's claim semantics simple and predictable.      |
| Task queue        | Atomic `pop` (urgency-based)         | Client-side list → filter → claim       | Single atomic operation minimizes agent token usage and eliminates race conditions between list and claim.    |

---

## Roadmap

### v0.1 — Foundation

- [x] Domain types and repository interfaces
- [x] SQLite implementation with migrations
- [x] TaskService with CRUD, workflow validation, optimistic locking
- [x] Basic CLI: `add`, `list`, `info`, `modify`, `done`, `delete`, `annotate`
- [x] Tag support: TagService, wire into CLI `add`/`modify`/`list`
- [x] Filter syntax parser
- [x] Automated end-to-end CLI tests

### v0.2 — Relations and hierarchy

- [x] RelationService with cycle detection
- [x] `link`, `unlink` CLI commands
- [x] Parent-child task creation and `tree` CLI command
- [x] Completion propagation
- [x] `tusk tag` subcommand: create, list, delete, rename tags
- [x] Project management CLI commands

### v0.3 — MCP server

- [x] MCP server with stdio transport
- [x] All CLI commands as MCP tools
- [x] Task/project/workflow resources
- [x] Version passing in MCP tool I/O

### v0.4 — Configuration & Customization

- [ ] Viper-based config loader (`~/.config/tusk/config.toml`)
- [ ] MCP visibility config schema (tool/resource group + individual toggles)
- [ ] Declarative workflow definitions in config
- [ ] Workflow CLI commands (`list`, `info`)
- [ ] Per-project workflow assignment
- [ ] MCP workflow tools
- [ ] MCP visibility wiring

### v0.5 — Urgency & UX

- [ ] Quoted string and boolean operator filter support
- [ ] Color-coded output (priority, status, tags)
- [ ] Urgency scoring engine with sigmoid due-date curve
- [ ] Configurable urgency weights + per-project overrides
- [ ] `tusk next` — highest-urgency actionable task

### v0.6 — Player Management

- [ ] Player entity and self-registration
- [ ] Task claiming (explicit + auto-claim on start)
- [ ] Player visibility (filters, `tusk available`)
- [ ] `tusk pop` — atomic claim-next-best-task queue
- [ ] MCP player tools

### v0.7 — Advanced Features

### v0.7 — Live Dashboard

- [ ] Event log infrastructure (append-only events table)
- [ ] `tusk dashboard` — live task board + player activity feed
- [ ] Bubbletea-based TUI with polling updates

### v0.8 — Advanced Features

- [ ] UDA CLI surface and schema validation
- [ ] Export (JSON, CSV)
- [ ] Recurrence (RRULE parsing, instance generation)
- [ ] Streamable HTTP transport for MCP
- [ ] Undo (powered by event log)

### Future

- [ ] PostgreSQL repository implementation
- [ ] Interactive TUI (inline editing from dashboard)
- [ ] REST API
- [ ] Webhook notifications (powered by event log)
- [ ] Time tracking
- [ ] Bidirectional sync
