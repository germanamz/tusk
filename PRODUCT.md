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

Workflows define which statuses exist, which transitions between them are valid, which status is the **initial status** — the default for newly created tasks — and which statuses are **terminal** — indicating a task is finished. They are declared in configuration, not stored in the database.

Tusk ships with a built-in **kanban** workflow:

```
pending (initial) → active → completed (terminal)
                           → deleted (terminal)
active  → pending
completed → pending
```

Custom workflows can define any status set, transition graph, initial status, and terminal statuses. When `initial_status` is omitted from config, the first entry in the statuses list is used. Multiple terminal statuses are allowed (e.g., `completed`, `canceled`, `deleted`). Terminal statuses drive behavior throughout tusk: `tusk done` transitions to the first terminal status, `tusk available` and `tusk pop` derive actionable tasks as those not in a terminal status. Each project references a workflow by name. Any status change not defined in the workflow is rejected.

### Projects

Projects group tasks and bind them to a workflow. Like workflows, projects live in configuration. A built-in **default** project provides a zero-config starting point.

Projects can override urgency scoring weights and configure parent-child automation (auto-complete, auto-revert) independently.

### Tags

Flat labels for cross-cutting categorization. Tags carry an optional color for terminal rendering and can be filtered with `+tag` / `-tag` syntax. They exist independently of projects and can be applied to any task.

### Annotations

Timestamped, immutable notes attached to tasks. They serve as a running log of context, decisions, or status updates that shouldn't modify the task itself.

### Notes

A persistent notebook for players to record what they've learned, what worked, what didn't, and any context worth preserving. Unlike annotations (which are task-scoped and immutable), notes are player-scoped and support archiving.

Notes can be attached to a specific task or exist at the project level as free-standing entries. Each note carries a markdown body and optional key-value metadata for structured tagging (e.g., `topic=auth`, `type=discovery`).

To avoid context overload, tusk displays only a **trailing window** of recent notes — the N most recent entries. The window size is configurable at four levels: global config, per-project config, per-player (stored in the player's DB record), and CLI flag override. A `--since` filter provides optional time-bounded queries on top of the count-based window.

By default, players see only their own notes. The `--all-players` flag or `--player <id>` flag reveals other players' notes, with the same trailing window applied.

Notes are append-only. They cannot be edited after creation, but can be **archived** — removing them from the active window without deleting them. The `--archived` flag includes archived notes in listings.

### Urgency Scoring

Every task receives a numeric urgency score computed from weighted factors: priority, proximity to due date (sigmoid curve), age, active status, whether it blocks or is blocked by other tasks, tags, project membership, annotation count, and waiting state. The score determines default sort order across all views.

Weights are configurable globally and can be overridden per project, so different projects can express different prioritization philosophies without custom sort logic.

### User-Defined Attributes

Tasks support arbitrary key-value metadata via UDAs. Any string key-value pair can be attached, overwritten, or removed. UDAs are filterable (`uda.key=value`) and appear in all task responses across both interfaces.

Projects can define UDA schemas — which keys are allowed, what types and values they accept. When a schema is defined, invalid metadata is rejected on create and update. Without a schema, UDAs are free-form.

### Recurrence

Tasks can carry an RFC 5545 RRULE string describing a recurrence pattern. When a recurring task is completed, tusk generates the next instance automatically based on the rule. End dates and count limits are respected — a recurring task stops generating instances once its rule is exhausted.

### Event Log

Tusk maintains an append-only event log recording every mutation — task creation, status changes, field modifications, claims, releases, relation changes. Each event captures what happened, to which entity, by which player, and when.

The event log has bounded retention with configurable pruning, so it doesn't grow without limit. It serves as the foundation for undo, the live dashboard, and webhook notifications.

### Undo

The **undo** operation reverts the last mutation by reading the most recent event from the log and applying its inverse. It covers task field changes, status transitions, and claim operations. Undo is a single-step revert, not a full history traversal.

### Time Tracking

Tasks support start/stop timers for tracking time spent. A player starts a timer on a task, works, and stops it. Accumulated time is recorded per task and reportable. This is distinct from task status — a task can be active without a running timer, and a timer can run across status transitions.

### File Attachments

Binary files can be attached to tasks. Attachments are stored on the filesystem and referenced in the database. They are accessible through both CLI and MCP, and included in export operations.

---

## Interfaces

### CLI

Tusk exposes all operations through a command-line interface:

```bash
# Lifecycle
tusk add "Implement auth middleware" project=backend +api priority=3
tusk start a3f8b2c1
tusk done a3f8b2c1
tusk delete a3f8b2c1

# Viewing
tusk list                              # pending + active, sorted by urgency
tusk list project=backend +api         # filtered
tusk info a3f8b2c1                     # full task detail
tusk tree                              # hierarchical view
tusk next                              # highest-urgency actionable task

# Modification
tusk modify a3f8b2c1 priority=4 +urgent
tusk annotate a3f8b2c1 "Blocked by upstream API changes"
tusk undo                              # revert last mutation

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

# Notes
tusk note add "caching strategy won't work" project=backend
tusk note add "retry logic needed" --task a3f8b2c1 topic=auth
tusk note list                             # own notes, trailing window
tusk note list --all-players               # all players' notes
tusk note list --player agent-1            # specific player
tusk note list --window 50 --since 7d      # overrides
tusk note list --archived                  # include archived
tusk note archive <note_id>

# Time tracking
tusk timer start a3f8b2c1
tusk timer stop a3f8b2c1

# Attachments
tusk attach a3f8b2c1 spec.pdf

# Data portability
tusk export --format json              # full dump
tusk export --format csv               # flat export

# Configuration
tusk config show                         # effective merged config
tusk config get urgency.due_weight       # single value
tusk config set urgency.due_weight 10.0  # write to config
tusk config init --local                 # create local tusk.toml

# Projects & workflows
tusk project list
tusk project create backend workflow=kanban db-path=/data/b.db
tusk project modify backend urgency.blocking-weight=15
tusk project delete backend
tusk workflow list
tusk workflow info kanban
tusk workflow create sprint status=pending,active,done transition=pending:active,active:done
tusk workflow modify sprint highlight=active dim=done
tusk workflow delete sprint
```

Output is available in human-readable text (with color, markdown rendering) and JSON (`--output json`) for scripting. Color respects `NO_COLOR` and `--no-color`.

### Go Library

Tusk's core packages are importable, so other Go programs can embed tusk directly as a library without shelling out to the CLI or speaking MCP.

A high-level `Client` type in the root package wires up the database, migrations, and all services from a single `Config` struct:

```go
client, err := tusk.NewClient(tusk.Config{
    DBPath: "/tmp/my-tasks.db",
})
defer client.Close()

task, _ := client.Tasks.Create(ctx, service.CreateTaskInput{
    Title:   "Build the thing",
    Project: "default",
})
```

The `Client` exposes service instances as public fields (`Tasks`, `Tags`, `Relations`, `Projects`, `Workflows`, `Players`), so every operation available through CLI and MCP is available programmatically.

For consumers who need full control, the building-block packages (`domain`, `service`, `repository`, `sqlite`, `inmem`, `filter`, `config`) are importable directly. Custom storage backends can implement the repository interfaces without using the `Client` at all.

Configuration is purely programmatic — no file loading, no environment variables. When config fields are omitted, the built-in kanban workflow and default project apply, same as a fresh CLI install.

### MCP Server

The MCP server mirrors the CLI through tool calls, so AI agents interact with the same system through the same service layer:

```bash
tusk mcp serve                                     # stdio transport
tusk mcp serve --transport http --port 8080        # Streamable HTTP transport
```

**Tools** cover task CRUD, lifecycle transitions, annotations, tree views, relations, player registration, claiming, available tasks, pop, project/workflow management, configuration, and notes. All mutation tools accept a `version` parameter for end-to-end optimistic locking. All tools accept an optional `player_id` for liveness tracking and auto-registration. Configurable field-level restrictions prevent agents from modifying sensitive fields via MCP.

**Resources** — tasks, projects, and workflows are also exposed as MCP resources for agents that prefer reading state over tool calls:

```
tusk://tasks/{short_id}
tusk://projects/{name}
tusk://projects/{name}/workflow
```

Tool and resource visibility is configurable — individual tools, resource templates, or entire groups can be hidden from agents.

### Live Dashboard

A real-time terminal dashboard for monitoring task state and player activity:

- **Task board** — kanban-style columns organized by status, refreshing live by polling the event log.
- **Player activity feed** — a stream of recent actions ("agent-1 claimed X", "german completed Y"), filterable by player or event type.
- **Idle player detection** — highlights players who claimed tasks but haven't acted for a configurable duration.

The dashboard is a read-only view — it polls the same database that CLI and MCP write to, without interfering with their operations. Layout, refresh interval, and visible columns are configurable.

### REST API

RESTful HTTP endpoints mirror CLI and MCP capabilities for integration with web applications and external services. Supports authentication and authorization for multi-user deployments.

### Webhooks

Task state changes fire webhook notifications to configured endpoints, powered by the event log. This is the integration point for Slack, email, CI pipelines, and external systems that need to react to task activity without polling.

---

## Filtering

Tusk provides a filter language inspired by TaskWarrior, extended with boolean logic:

```bash
tusk list project=backend +api priority=3..4 due=today..friday
tusk list (status=active AND +urgent) OR priority=4
tusk list claimed_by=agent-1
tusk available unclaimed=true project=backend
```

Filter capabilities:

- **Field match** — `project=name`, `status=pending,active`, `claimed_by=player`
- **Tags** — `+tag` to require, `-tag` to exclude
- **Ranges** — `priority=2..4`, `due=today..friday`
- **Relative dates** — `due=today`, `due=tomorrow`, `due=thisweek`
- **Hierarchy** — `parent=<short_id>` (direct children), `tree=<short_id>` (all descendants)
- **Text search** — `title="some text"`, `description="text"`
- **UDA** — `uda.key=value`
- **Boolean operators** — `AND`, `OR`, `NOT` with parenthesized grouping
- **Claim state** — `claimed_by=<player_id>`, `unclaimed=true`, `waiting=true`

When no status filter is specified, tusk defaults to `status=pending,active`.

### Inline Syntax

Tusk uses a shared inline syntax across all commands — filters, task creation, modification, and config management. The syntax is built on a common lexer that understands three primitives:

- **Fields** — `key=value` pairs. The `=` separates key from value.
- **Modifiers** — `+`, `-`, `,`, `:`, and `..` are first-class modifiers attached as token metadata, not hardcoded behaviors. Each command context decides what a modifier means:
  - `+` / `-` — In filters: `+tag` includes, `-tag` excludes. In task commands: `+tag` adds, `-tag` removes. In config commands: `+status=review` adds to a list, `-status=review` removes from it.
  - `,` — Unordered set. `status=pending,active` is a set — order doesn't matter and duplicates are deduplicated.
  - `:` — Ordered sequence. `transition=pending:active` preserves order and allows duplicates — items appear in the sequence they were placed (from → to).
  - `..` — Range. `priority=2..4` defines a range in filters.
- **Quoted strings** — `title="some text"` for values containing spaces, with `\"` for escaped quotes.

Individual commands define which fields and modifiers they accept. The lexer tokenizes uniformly; domain-specific validators determine what's valid in each context.

---

## Concurrency Model

Tusk is designed for concurrent access from day one — multiple CLI invocations, parallel script commands, and rapid MCP tool calls from AI agents.

Every mutable entity carries a **version** field. Updates use optimistic locking: the write succeeds only if the version matches what was last read. On mismatch, the operation fails with a conflict error rather than silently overwriting. MCP responses include the current version so agents can pass it back on subsequent modifications, enabling end-to-end optimistic locking even across separate tool calls.

SQLite runs in WAL mode, allowing concurrent readers without blocking writers.

---

## Configuration

Tusk uses TOML configuration files with a layered resolution chain. Configuration is discovered in priority order:

1. **Local** — `tusk.toml` in the current working directory
2. **Global** — `~/.config/tusk/config.toml`
3. **Ancestor** — walk upward from CWD to filesystem root, looking for `tusk.toml`

Each layer merges on top of the next — local overrides global, global overrides ancestor. The `--config <path>` flag bypasses discovery entirely. Environment variables with the `TUSK_` prefix override any config key. The `--db` flag overrides the database path directly.

A default configuration is embedded in the binary. Running `tusk config init` creates a global config file with defaults; `tusk config init --local` creates a `tusk.toml` in CWD for project-scoped configuration.

Configuration governs:

- **Storage** — database backend and connection settings (including per-project database paths)
- **Workflows** — custom status sets and allowed transitions
- **Projects** — workflow binding, automation settings, UDA schemas, urgency weight overrides, optional per-project DB path
- **Urgency weights** — global scoring adjustments
- **MCP** — transport settings, tool/resource visibility
- **TUI** — date format, color, tree indentation, default sort order
- **Dashboard** — refresh interval, layout, visible columns

### Config Management

Configuration can be inspected and modified from the CLI without manual file editing:

```bash
tusk config show                          # effective merged config with source annotations
tusk config get urgency.due_weight        # single value lookup
tusk config set urgency.due_weight 10.0   # write to local config (or global if no local)
tusk config set --global tui.color false  # force write to global config
tusk config edit                          # open config in $EDITOR
tusk config validate                      # check for errors
```

Workflow and project management commands also write to the config file, using the same inline `key=value` syntax as task modify. List fields support `+`/`-` prefixes for additive/subtractive operations on modify:

```bash
# Workflows
tusk workflow create sprint status=pending,active,done \
  transition=pending:active,active:done highlight=active
tusk workflow modify sprint +status=in-review +transition=active:in-review
tusk workflow modify sprint dim=done,archived
tusk workflow delete sprint

# Projects
tusk project create backend workflow=kanban db-path=/data/backend.db
tusk project modify backend urgency.blocking-weight=15 auto-complete.trigger=completed
tusk project delete backend
```

### Per-Project Database

Each project can optionally specify its own SQLite database file via `db-path`. Projects without a `db-path` use the global storage path. Cross-project commands query all project databases and merge results.

---

## Storage

Tusk uses SQLite by default. The database is a single file (default: `~/.local/share/tusk/tusk.db`), migrations are embedded in the binary and run automatically, and WAL mode is enabled for concurrent access.

The storage layer is defined as a set of interfaces. SQLite is the shipped default. PostgreSQL is supported as an alternative backend for multi-user and networked deployments, with its own connection pooling and migration path. The interface boundary means adding a new backend requires no changes to the service layer.

---

## Data Portability

Tusk supports full data export for backup, migration, and interoperability:

- **JSON export** — complete dump of all tasks, relations, annotations, tags, and players.
- **CSV export** — flat tabular export of tasks for spreadsheet workflows.

Bidirectional sync allows merging task data across tusk instances. The sync protocol defines a conflict resolution strategy so that two instances that have diverged can be reconciled without data loss.
