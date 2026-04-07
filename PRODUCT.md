# Tusk

**A concurrent-safe task manager for humans and AI agents.**

Tusk ships as a single binary with no external dependencies. It combines the CLI speed of TaskWarrior with structured hierarchy, typed relations, and a built-in MCP server — so AI agents and humans can manage tasks side by side, without stepping on each other's work.

Licensed under **Apache 2.0**.

---

## Why Tusk

Existing tools force a choice. TaskWarrior is fast but flat — no real hierarchy, no typed relations, file-level locking that breaks under concurrency. Jira and Linear are powerful but browser-bound and opaque to automation. None of them ship with an MCP interface.

Tusk occupies the gap:

- **Single binary** — no runtime, no daemon, no browser.
- **Dual interface** — every operation works from both the terminal and the MCP server. Humans and AI agents share one system with no translation layer.
- **Player coordination** — tusk doesn't just track tasks, it tracks *who is working on what*. Players claim tasks, and the system prevents overlapping work. A built-in task queue lets agents pop the next best task atomically.
- **Structured relationships** — hierarchical nesting, typed directed edges between tasks (blocks, relates_to, duplicates), and cycle detection on blocking chains.
- **Concurrent-safe by default** — optimistic locking on every mutable entity. Concurrent CLI invocations, scripts, and MCP agents can all hit the same database without clobbering each other.

---

## Core Concepts

### Tasks

Everything in tusk is a task. There are no epics, stories, or subtasks as distinct types — a task is an "epic" if it has children, and a "subtask" if it has a parent. This keeps the model flat where simple is enough and hierarchical where structure is needed, without forcing a taxonomy.

Each task carries a title, optional markdown description, status, priority (none through urgent), optional due date, and a computed urgency score. Tasks can hold arbitrary key-value metadata through user-defined attributes.

Tasks are identified by an 8-character short ID derived from their UUID. The short ID is stable for the task's lifetime and used in all human-facing interactions. The full UUID is used internally and in programmatic contexts.

Tasks are never physically removed. Deletion is a status transition through the project's workflow, preserving full history.

### Hierarchy

Tasks can nest to arbitrary depth via an optional parent reference. When all children of a parent reach a trigger status, the parent can auto-transition (e.g., auto-complete when all children complete). The reverse works too — reopening a child can auto-revert a completed parent. Both behaviors are configurable per project and disabled by default.

### Relations

Tasks can be linked with typed, directed edges independent of the parent-child hierarchy:

- **blocks** — A must complete before B can proceed. Before creating a blocks edge, tusk runs a depth-first search through existing blocks edges to prevent cycles.
- **relates_to** — informational association between related tasks.
- **duplicates** — marks one task as a duplicate of another.

Inverse relations (blocked_by, related_to, duplicated_by) are derived at query time by swapping source and target. One row per logical relation, no duplicate storage.

### Players

A player is any entity that works with tusk — a human at a terminal, an AI agent via MCP, or a script. Players self-register by providing an ID on any operation; no predefined roster is required. Each player is typed as `human` or `agent`, and tusk tracks when they were last active.

### Claiming

Players claim tasks to signal intent and prevent collisions. Starting a task auto-claims it if unclaimed; if another player already holds the claim, the operation is rejected. There is no force-steal and no TTL — stale player management is the consumer's concern. Claims are preserved after task completion and deletion for historical attribution.

### Task Queue

The **pop** operation atomically finds the highest-urgency unclaimed, unblocked task, claims it for the calling player, and starts it. One operation replaces what would otherwise be a list-filter-pick-claim sequence — eliminating race conditions and minimizing token usage in multi-agent setups. The **available** command provides the same filtered view without committing to a claim.

Both operations accept filters, so an agent can pop from a specific project, tag scope, or priority range.

### Workflows

Workflows define which statuses exist and which transitions between them are valid. They are declared in configuration, not stored in the database.

Tusk ships with a built-in **kanban** workflow:

```
pending → active → completed
                 → deleted
active  → pending
completed → pending
```

Custom workflows can define any status set and transition graph. Each project references a workflow by name. Any status change not defined in the workflow is rejected.

### Projects

Projects group tasks and bind them to a workflow. Like workflows, projects live in configuration. A built-in **default** project provides a zero-config starting point.

Projects can override urgency scoring weights and configure parent-child automation (auto-complete, auto-revert) independently.

### Tags

Flat labels for cross-cutting categorization. Tags carry an optional color for terminal rendering and can be filtered with `+tag` / `-tag` syntax. They exist independently of projects and can be applied to any task.

### Annotations

Timestamped, immutable notes attached to tasks. They serve as a running log of context, decisions, or status updates that shouldn't modify the task itself.

### Urgency Scoring

Every task receives a numeric urgency score computed from weighted factors: priority, proximity to due date (sigmoid curve), age, active status, whether it blocks or is blocked by other tasks, tags, project membership, annotation count, and waiting state. The score determines default sort order across all views.

Weights are configurable globally and can be overridden per project, so different projects can express different prioritization philosophies without custom sort logic.

### User-Defined Attributes

Tasks support arbitrary key-value metadata via UDAs. Any string key-value pair can be attached, overwritten, or removed. UDAs are filterable (`uda.key:value`) and appear in all task responses across both interfaces.

---

## Interfaces

### CLI

Tusk exposes all operations through a command-line interface:

```bash
# Lifecycle
tusk add "Implement auth middleware" project:backend +api priority:3
tusk start a3f8b2c1
tusk done a3f8b2c1
tusk delete a3f8b2c1

# Viewing
tusk list                              # pending + active, sorted by urgency
tusk list project:backend +api         # filtered
tusk info a3f8b2c1                     # full task detail
tusk tree                              # hierarchical view
tusk next                              # highest-urgency actionable task

# Modification
tusk modify a3f8b2c1 priority:4 +urgent
tusk annotate a3f8b2c1 "Blocked by upstream API changes"

# Relations
tusk link a3f8b2c1 blocks b7c9d4e2
tusk unlink a3f8b2c1 blocks b7c9d4e2

# Player coordination
tusk player register german --type human
tusk claim a3f8b2c1 --player german
tusk release a3f8b2c1
tusk available
tusk pop --player german

# Tags
tusk tag list
tusk tag create bug --color "#ff0000"

# Configuration entities
tusk project list
tusk workflow list
tusk workflow info kanban
```

Output is available in human-readable text (with color, markdown rendering) and JSON (`--output json`) for scripting. Color respects `NO_COLOR` and `--no-color`.

### MCP Server

The MCP server mirrors the CLI through tool calls over stdio, so AI agents interact with the same system through the same service layer:

```bash
tusk mcp serve    # stdio transport
```

**Tools** — 19 tools covering task CRUD, lifecycle transitions, annotations, tree views, relations, player registration, claiming, available tasks, pop, and read-only project/workflow listing. All mutation tools accept a `version` parameter for end-to-end optimistic locking. All tools accept an optional `player_id` for liveness tracking and auto-registration.

**Resources** — tasks, projects, and workflows are also exposed as MCP resources for agents that prefer reading state over tool calls:

```
tusk://tasks/{short_id}
tusk://projects/{name}
tusk://projects/{name}/workflow
```

Tool and resource visibility is configurable — individual tools, resource templates, or entire groups can be hidden from agents.

---

## Filtering

Tusk provides a filter language inspired by TaskWarrior, extended with boolean logic:

```bash
tusk list project:backend +api priority:3..4 due:today..friday
tusk list (status:active AND +urgent) OR priority:4
tusk list claimed_by:agent-1
tusk available unclaimed:true project:backend
```

Filter capabilities:

- **Field match** — `project:name`, `status:pending,active`, `claimed_by:player`
- **Tags** — `+tag` to require, `-tag` to exclude
- **Ranges** — `priority:2..4`, `due:today..friday`
- **Relative dates** — `due:today`, `due:tomorrow`, `due:thisweek`
- **Hierarchy** — `parent:<short_id>` (direct children), `tree:<short_id>` (all descendants)
- **Text search** — `title:"some text"`, `description:"text"`
- **UDA** — `uda.key:value`
- **Boolean operators** — `AND`, `OR`, `NOT` with parenthesized grouping
- **Claim state** — `claimed_by:<player_id>`, `unclaimed:true`, `waiting:true`

When no status filter is specified, tusk defaults to `status:pending,active`.

---

## Concurrency Model

Tusk is designed for concurrent access from day one — multiple CLI invocations, parallel script commands, and rapid MCP tool calls from AI agents.

Every mutable entity carries a **version** field. Updates use optimistic locking: the write succeeds only if the version matches what was last read. On mismatch, the operation fails with a conflict error rather than silently overwriting. MCP responses include the current version so agents can pass it back on subsequent modifications, enabling end-to-end optimistic locking even across separate tool calls.

SQLite runs in WAL mode, allowing concurrent readers without blocking writers.

---

## Configuration

Tusk is configured via `~/.config/tusk/config.toml`. A default configuration is embedded in the binary and auto-created on first run.

Configuration governs:

- **Storage** — database path
- **Workflows** — custom status sets and allowed transitions
- **Projects** — workflow binding, automation settings, urgency weight overrides
- **Urgency weights** — global scoring adjustments
- **MCP** — transport settings, tool/resource visibility
- **TUI** — date format, color, tree indentation, default sort order

Environment variables with the `TUSK_` prefix override any config key. The `--db` flag overrides the database path directly.

---

## Storage

Tusk uses SQLite by default. The database is a single file (default: `~/.local/share/tusk/tusk.db`), migrations are embedded in the binary and run automatically, and WAL mode is enabled for concurrent access.

The storage layer is defined as a set of interfaces. The SQLite implementation is the shipped default, but the architecture allows plugging in alternative backends without touching the service layer.

---

## Planned

### Event Log and Live Dashboard

An append-only event log recording all mutations — task creation, status changes, claims, releases, relation changes. Bounded retention with configurable pruning.

Built on top of the event log: a real-time terminal dashboard with kanban-style task columns, a player activity feed, and idle-player detection. Refreshes by polling the event log.

### Undo

Revert the last mutation by reading the event log and applying the inverse operation. Covers task changes, status transitions, and claim operations.

### Recurrence

RFC 5545 RRULE strings on tasks. On completion of a recurring task, tusk auto-generates the next instance. Handles end dates and count limits.

### UDA Schema Validation

Per-project schemas for user-defined attributes. Projects define which UDA keys are allowed and what types/values they accept. Invalid metadata is rejected on create and update.

### Data Export

Full dump in JSON or flat export in CSV for backup and migration.

### MCP Streamable HTTP Transport

Network-accessible MCP server using Streamable HTTP (successor to SSE) for multi-client scenarios.

### PostgreSQL Backend

A PostgreSQL storage implementation for multi-user and networked deployments, with connection pooling and its own migration path.

### Interactive TUI

Extend the dashboard into a full interactive terminal interface — inline editing, status transitions, and task creation without leaving the TUI.

### REST API

RESTful HTTP endpoints mirroring CLI and MCP capabilities, with authentication and authorization.

### Webhooks

Fire webhooks on task state changes, powered by the event log. Integration point for Slack, email, CI pipelines, and external systems.

### Time Tracking

Start/stop timers on tasks, report time spent.

### File Attachments

Attach binary files to tasks, stored on the filesystem and referenced in the database.

### Bidirectional Sync

A sync protocol for merging task data across tusk instances, with conflict resolution.
