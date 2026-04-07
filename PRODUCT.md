# Tusk

**A concurrent-safe task manager for humans and AI agents.**

Tusk ships as a single binary with no external dependencies. It combines the CLI speed of TaskWarrior with structured hierarchy, typed relations, and a built-in MCP server — so AI agents and humans can manage tasks side by side, without stepping on each other's work.

Licensed under **Apache 2.0**.

---

## What Tusk Does

Tusk is a task management tool that works equally well from the terminal and from AI agents via MCP. Every feature is available through both interfaces.

At its core, tusk tracks tasks — but it goes further by managing **who** is working on **what**. Players (humans or agents) register, claim tasks, and coordinate through a built-in task queue. This prevents overlapping work and makes multi-agent workflows practical.

---

## Tasks

A task is the central unit in tusk. Every trackable item — from a one-off todo to an epic spanning dozens of subtasks — is a task.

Each task has:

- A **title** and optional **description** (supports markdown)
- A **status** governed by the project's workflow
- A **priority** from none (0) to urgent (4)
- An optional **due date** and **wait-until date** (hidden from views until that time)
- A computed **urgency score** that determines sort order
- **User-defined attributes** for project-specific metadata

Tasks are identified by an 8-character short ID derived from their UUID. Short IDs are stable for the lifetime of the task and used in all CLI interactions.

### Hierarchy

Tasks can be nested. A task with children is effectively an "epic" — there are no forced types or distinctions at the data level. Nesting depth is unlimited.

When all children of a parent complete, the parent can auto-complete (configurable per project). The reverse also works: if a completed parent's child reopens, the parent can auto-revert.

### Soft Delete

Tasks are never removed from storage. Deletion transitions a task to `deleted` status through the workflow, preserving history.

---

## Relations

Tasks can be linked with typed, directed relations:

- **blocks** — task A must complete before task B can proceed
- **relates_to** — informational link between related tasks
- **duplicates** — marks a task as a duplicate of another

Inverse relations (`blocked_by`, `related_to`, `duplicated_by`) are derived automatically — no duplicate data.

**Cycle detection** prevents deadlocks: before creating a `blocks` relation from A to B, tusk checks whether B already transitively blocks A. If it does, the relation is rejected.

---

## Workflows

Workflows define which statuses exist and which transitions between them are allowed. They are defined in configuration, not in the database.

Tusk ships with a built-in **kanban** workflow:

```
pending → active → completed
                 → deleted
active  → pending (reopen)
completed → pending (reopen)
```

Custom workflows can define any set of statuses and transitions. Each project references a workflow by name.

---

## Projects

Projects group tasks and assign them a workflow. Like workflows, projects are defined in configuration.

A built-in **default** project exists without any configuration. Projects can override urgency scoring weights and configure parent task auto-completion behavior.

---

## Tags

Tags are flat labels for cross-cutting categorization. They support color assignment for visual distinction in terminal output.

Tags are managed independently and assigned to tasks. CLI syntax uses `+tag` to include and `-tag` to exclude when filtering.

---

## Annotations

Timestamped notes attached to tasks. Immutable after creation — they serve as a log of context, decisions, or status updates.

---

## Urgency Scoring

Every task gets a numeric urgency score computed from weighted factors:

| Factor        | Effect                                              |
| ------------- | --------------------------------------------------- |
| Priority      | Higher priority increases urgency                   |
| Due date      | Urgency rises sharply as the due date approaches    |
| Age           | Older tasks gradually gain urgency (caps at 1 year) |
| Active status | Active tasks get a boost                            |
| Blocking      | Tasks that block others are more urgent             |
| Blocked       | Tasks blocked by others are deprioritized           |
| Tags          | Configurable urgency tags add weight                |
| Project       | Having a project adds slight urgency                |
| Annotations   | Annotated tasks get a small boost                   |
| Waiting       | Tasks with a future wait-until date are deprioritized |

Urgency determines default sort order across all views. Weights are configurable globally and per project.

---

## Players and Task Claiming

A **player** is any entity — human or AI agent — that interacts with tusk. Players self-register on first contact by providing an ID. No predefined roster is needed.

### Claiming

Players claim tasks to signal intent and prevent overlapping work:

- **Explicit claim** — reserve a task for yourself
- **Auto-claim on start** — starting a task claims it if unclaimed; if claimed by someone else, the operation is rejected
- **Release** — clear a claim when you're done or changing focus
- **No force-steal** — if a player goes stale, managing that is the consumer's responsibility

Claims are preserved after task completion or deletion for historical attribution.

### Task Queue

The **pop** operation atomically finds the highest-urgency unclaimed, unblocked task, claims it, and starts it — all in one step. This replaces the list-filter-pick-claim dance that wastes tokens and introduces race conditions in multi-agent setups.

The **available** command lists all unclaimed, actionable, unblocked tasks sorted by urgency — useful for browsing before committing.

Both operations support filters, so agents can pop from a specific project or tag scope.

---

## Filtering

Tusk provides a rich filter syntax inspired by TaskWarrior:

```
tusk list project:backend +api priority:3..4 due:today..friday
```

Supported filters:

- `project:name` — by project
- `+tag` / `-tag` — include or exclude tags
- `status:pending,active` — comma-separated status values
- `priority:2..4` — numeric ranges
- `due:today`, `due:tomorrow`, `due:thisweek` — relative dates
- `parent:<short_id>` — direct children
- `tree:<short_id>` — all descendants
- `claimed_by:<player_id>` — tasks claimed by a player
- `unclaimed:true` — unclaimed tasks
- `title:"some text"` / `description:"text"` — text search
- `uda.key:value` — user-defined attribute filter
- `waiting:true` — tasks with a future wait-until date

Filters support boolean operators (`AND`, `OR`, `NOT`) and parenthesized grouping for complex queries.

When no status filter is specified, tusk defaults to showing `pending` and `active` tasks.

---

## Concurrency

Tusk is built for concurrent access. Multiple CLI invocations, scripts, and MCP agents can safely operate on the same database simultaneously.

Every mutable entity carries a **version** field. Updates only succeed if the version matches — if someone else wrote first, the operation fails with a conflict error rather than silently overwriting. MCP responses include the current version so agents can pass it back on subsequent modifications.

SQLite runs in WAL mode, allowing concurrent readers without blocking writers.

---

## CLI

Tusk provides a command-line interface for all operations:

```bash
# Task lifecycle
tusk add "Implement auth middleware" project:backend +api priority:3
tusk start a3f8b2c1
tusk done a3f8b2c1
tusk delete a3f8b2c1

# Viewing tasks
tusk list                              # pending + active, sorted by urgency
tusk list project:backend +api         # filtered
tusk info a3f8b2c1                     # full detail
tusk tree                              # hierarchical view
tusk next                              # single highest-urgency actionable task

# Modification
tusk modify a3f8b2c1 priority:4 +urgent
tusk annotate a3f8b2c1 "Blocked by upstream API changes"

# Relations
tusk link a3f8b2c1 blocks b7c9d4e2
tusk unlink a3f8b2c1 blocks b7c9d4e2

# Tags
tusk tag list
tusk tag create bug --color "#ff0000"

# Player management
tusk player register german --type human
tusk claim a3f8b2c1 --player german
tusk release a3f8b2c1
tusk available
tusk pop --player german

# Configuration entities
tusk project list
tusk workflow list
tusk workflow info kanban
```

Output supports both human-readable text (with color) and JSON (`--output json`) for scripting. Color respects the `NO_COLOR` environment variable and `--no-color` flag. Markdown descriptions are rendered with syntax highlighting in the terminal.

---

## MCP Server

The MCP server exposes every capability through tool calls, enabling AI agents to manage tasks programmatically.

```bash
tusk mcp serve    # stdio transport for IDE integration
```

### Tools

| Tool                   | Description                        |
| ---------------------- | ---------------------------------- |
| `tusk_task_create`     | Create a new task                  |
| `tusk_task_get`        | Get a single task by short ID      |
| `tusk_task_list`       | List and filter tasks              |
| `tusk_task_modify`     | Modify task fields                 |
| `tusk_task_start`      | Transition to active               |
| `tusk_task_done`       | Transition to completed            |
| `tusk_task_delete`     | Transition to deleted              |
| `tusk_task_annotate`   | Add an annotation                  |
| `tusk_task_tree`       | Get hierarchical task view         |
| `tusk_task_next`       | Get highest-urgency actionable task |
| `tusk_task_claim`      | Claim a task for a player          |
| `tusk_task_release`    | Release a claim                    |
| `tusk_task_available`  | List available tasks               |
| `tusk_task_pop`        | Atomically claim next best task    |
| `tusk_relation_add`    | Create a relation between tasks    |
| `tusk_relation_remove` | Remove a relation                  |
| `tusk_project_list`    | List projects                      |
| `tusk_workflow_list`   | List workflows                     |
| `tusk_player_register` | Register a player                  |

All mutation tools accept a `version` parameter for optimistic locking. All tools accept an optional `player_id` parameter for player liveness tracking.

### Resources

Tasks, projects, and workflows are also available as MCP resources:

```
tusk://tasks/{short_id}
tusk://projects/{name}
tusk://projects/{name}/workflow
```

Tool and resource visibility can be configured to hide specific tools or groups from agents.

---

## User-Defined Attributes

Tasks support arbitrary key-value metadata through user-defined attributes (UDAs). These are schemaless — any string key-value pair can be attached.

```bash
tusk add "Deploy service" --uda environment=production --uda region=us-east-1
tusk modify a3f8b2c1 --uda environment=staging    # overwrite
tusk list uda.environment:production               # filter by UDA
```

UDAs appear in task info and are included in all MCP responses. File-based input is supported for descriptions: `--description @file.md`.

---

## Configuration

Tusk is configured via `~/.config/tusk/config.toml`. A default configuration is embedded in the binary and written on first run.

Configurable areas:

- **Storage** — database path
- **Urgency weights** — global and per-project scoring adjustments
- **Workflows** — custom status sets and transitions
- **Projects** — workflow assignment, automation settings, urgency overrides
- **MCP** — transport settings, tool/resource visibility
- **TUI** — date format, color, tree indentation, default sort

Environment variables with the `TUSK_` prefix can override any config value. The `--db` flag overrides the database path.

---

## Storage

Tusk uses SQLite by default with WAL mode enabled. The database is a single file at `~/.local/share/tusk/tusk.db` (configurable). Migrations are embedded in the binary and run automatically.

The storage layer is designed as a set of interfaces — the SQLite implementation is the shipped default, but the architecture supports alternative backends.

---

## Planned Features

The following features are planned but not yet implemented.

### Live Dashboard

A real-time terminal dashboard for monitoring task state and player activity.

- **Task board** — kanban-style columns showing tasks by status, refreshing live
- **Player activity feed** — stream of recent actions ("agent-1 claimed X", "german completed Y") with filtering by player or event type
- **Idle player detection** — highlight players who claimed tasks but haven't acted for a configurable duration

The dashboard is powered by an **event log** — an append-only record of all mutations (task created, status changed, claimed, released, etc.) with bounded retention.

### Undo

Revert the last mutation using the event log. Supports undoing task changes, status transitions, and claim operations.

```bash
tusk undo    # revert last mutation
```

### Recurrence

Automatic generation of recurring tasks using RFC 5545 RRULE strings. When a recurring task is completed, tusk creates the next instance based on the recurrence rule. Handles end dates and count limits.

### UDA Schema Validation

Per-project schemas for user-defined attributes. Projects can define which UDA keys are allowed and what types/values they accept, so invalid metadata is rejected on create and update.

### Data Export

Full data portability via export:

```bash
tusk export --format json    # full dump
tusk export --format csv     # flat export
```

### MCP Streamable HTTP Transport

Network-accessible MCP server for multi-client scenarios, using the Streamable HTTP transport (successor to SSE):

```bash
tusk mcp serve --transport http --port 8080
```

### PostgreSQL Backend

A PostgreSQL storage backend for multi-user and networked deployments, with connection pooling and its own migration path.

### Interactive TUI

Extend the dashboard into a full interactive terminal interface — inline task editing, status transitions, and task creation without leaving the TUI.

### REST API

RESTful HTTP endpoints mirroring CLI and MCP capabilities, with authentication and authorization.

### Webhook Notifications

Fire webhooks on task state changes, powered by the event log. Enables integration with external systems like Slack, email, or CI pipelines.

### Time Tracking

Start and stop timers on tasks, and report time spent:

```bash
tusk timer start a3f8b2c1
tusk timer stop a3f8b2c1
```

### File Attachments

Attach binary files to tasks, stored on the filesystem and referenced in the database:

```bash
tusk attach a3f8b2c1 spec.pdf
```

### Bidirectional Sync

A sync protocol for merging task data across instances, with conflict resolution:

```bash
tusk sync export
tusk sync import
```
