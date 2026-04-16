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

Workflows define which statuses exist, which transitions between them are valid, and how each status behaves. Workflows live in the workspace database alongside tasks and projects — they are mutated through `tusk workflow` commands (or their MCP counterparts), not by hand-editing a config file.

Each status carries a set of **roles** that determine how tusk treats it:

| Role | Meaning | Constraint |
|------|---------|------------|
| `initial` | Default status for newly created tasks | Exactly one per workflow |
| `start` | Target for `tusk task start` and `tusk task pop` | Exactly one; must have valid transition from `initial` |
| `terminal` | Task is finished; excluded from `available`/`pop` | At least one per workflow |
| `done` | Target for `tusk task done` | Exactly one; must also be `terminal` |
| `delete` | Target for `tusk task delete` | Exactly one; must also be `terminal` |
| `highlight` | Emphasized in terminal output | Any number; combinable with any other role |
| `dim` | Deemphasized in terminal output | Any number; combinable with any other role |

Tusk ships with a built-in **kanban** workflow:

```
pending (initial) → active (start, highlight) → completed (terminal, done)
                                               → deleted (terminal, delete, dim)
active  → pending
completed (dim) → pending
```

Custom workflows can define any status set, transition graph, and role assignments. Roles are attached directly to individual statuses — no separate top-level fields. Each project references a workflow by name. Any status change not defined in the workflow is rejected.

### Projects

Projects group tasks and bind them to a workflow. Like workflows, projects live in the workspace database and are managed through `tusk project` commands. A built-in **default** project provides a zero-config starting point.

Projects can override urgency scoring weights and configure parent-child automation (auto-complete, auto-revert) independently. Overrides are stored alongside the project row, not in the config file.

Projects do not own their own database. Every project in a given workspace shares the database declared by the active config file's `storage.path` — the config file's directory is the scope. See [Workspace Scope](#workspace-scope).

### Tags

Flat labels for cross-cutting categorization. Tags carry an optional color for terminal rendering and can be filtered with `+tag` / `-tag` syntax. They exist independently of projects and can be applied to any task.

### Annotations

Timestamped, immutable notes attached to tasks. They serve as a running log of context, decisions, or status updates that shouldn't modify the task itself.

### Notes

A persistent notebook for players to record what they've learned, what worked, what didn't, and any context worth preserving. Unlike annotations (which are task-scoped and immutable), notes are player-scoped and support archiving.

Notes can be attached to a specific task or exist at the project level as free-standing entries. Each note carries a markdown body and optional key-value metadata for structured tagging (e.g., `meta.topic=auth`, `meta.type=discovery`). Metadata keys are namespaced under `meta.` — symmetric with task UDAs under `uda.` — so inline tokens like `project=` or `task=` remain unambiguous command arguments.

To avoid context overload, tusk displays only a **trailing window** of recent notes — the N most recent entries. The window size is configurable at four levels: global config, per-project config, per-player (stored in the player's DB record), and CLI flag override. A `--since` filter provides optional time-bounded queries on top of the count-based window.

By default, players see only their own notes. The `--all-players` flag or `--player <id>` flag reveals other players' notes, with the same trailing window applied.

Notes are append-only. They cannot be edited after creation, but can be **archived** — removing them from the active window without deleting them. The `--archived` flag includes archived notes in listings.

### Urgency Scoring

Every task receives a numeric urgency score computed from weighted factors: priority, proximity to due date (sigmoid curve), age, active status, whether it blocks or is blocked by other tasks, tags, project membership, annotation count, and waiting state. The score determines default sort order across all views.

Weights are configurable globally and can be overridden per project, so different projects can express different prioritization philosophies without custom sort logic.

### User-Defined Attributes

Tasks support arbitrary key-value metadata via UDAs. UDAs are set via inline `uda.key=value` syntax on `tusk task create` and `tusk task modify`, and filtered with the same `uda.key=value` syntax on `tusk task list`. Any string key-value pair can be attached, overwritten, or removed. UDAs appear in all task responses across both interfaces.

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
tusk task create "Implement auth middleware" project=backend +api priority=3
tusk task create "Deploy monitoring" project=ops +infra priority=3 uda.env=prod uda.region=eu
tusk task start a3f8b2c1
tusk task done a3f8b2c1
tusk task delete a3f8b2c1

# Viewing
tusk task list                         # pending + active, sorted by urgency
tusk task list project=backend +api    # filtered
tusk task get a3f8b2c1                 # full task detail
tusk task tree                         # hierarchical view
tusk task next                         # highest-urgency actionable task

# Modification
tusk task modify a3f8b2c1 priority=4 +urgent
tusk task modify a3f8b2c1 uda.team=backend                              # add/update a UDA
tusk task modify a3f8b2c1 uda.env=                                      # delete a UDA key
tusk task modify a3f8b2c1 description=@./spec.md                       # load from file
cat spec.md | tusk task modify a3f8b2c1 description=@-                  # load from stdin
tusk task modify a3f8b2c1 description="see @./notes.md for background"  # mid-string
tusk task modify a3f8b2c1 description="@@literal-at-sign in body"       # escape
tusk task annotate a3f8b2c1 "Blocked by upstream API changes"
tusk task annotate a3f8b2c1 @./investigation.md                         # annotate from file
tusk undo                              # revert last mutation (workspace-wide)

# Relations
tusk task link a3f8b2c1 blocks b7c9d4e2
tusk task unlink a3f8b2c1 blocks b7c9d4e2

# Player coordination
tusk player register german --type human
tusk task claim a3f8b2c1 --player german
tusk task release a3f8b2c1
tusk task available
tusk task pop --player german

# Tags
tusk tag list
tusk tag create bug --color "#ff0000"

# Notes
tusk note add "caching strategy won't work" project=backend
tusk note add "retry logic needed" --task a3f8b2c1 meta.topic=auth
tusk note list                             # own notes, trailing window
tusk note list --all-players               # all players' notes
tusk note list --player agent-1            # specific player
tusk note list --window 50 --since 7d      # overrides
tusk note list --archived                  # include archived
tusk note archive <note_id>

# Time tracking
tusk task timer start a3f8b2c1
tusk task timer stop a3f8b2c1

# Attachments
tusk task attach a3f8b2c1 spec.pdf

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
tusk project create backend workflow=kanban
tusk project modify backend urgency.blocking-weight=15
tusk project delete backend
tusk workflow list
tusk workflow info kanban
tusk workflow create sprint status=pending(initial) status=active(start,highlight) status=done(terminal,done,dim) transition=pending:active,active:done
tusk workflow modify sprint +status=in-review +transition=active:in-review
tusk workflow delete sprint

# Shell completion
tusk completion bash        # emit bash completion script
tusk completion zsh         # emit zsh completion script
tusk completion fish        # emit fish completion script
tusk completion powershell  # emit powershell completion script
```

Output is available in human-readable text (with color, markdown rendering) and JSON (`--output json`) for scripting. Color respects `NO_COLOR` and `--no-color`.

Shell completion scripts are generated on demand from the current command tree — tusk does not ship pre-baked completion artifacts. After every upgrade, regenerate and reinstall for your shell:

```bash
# bash — user scope
tusk completion bash > ~/.local/share/bash-completion/completions/tusk

# zsh — drop in any directory listed in $fpath
tusk completion zsh > "${fpath[1]}/_tusk"

# fish — user scope
tusk completion fish > ~/.config/fish/completions/tusk.fish

# powershell — append to your profile
tusk completion powershell | Out-String | Invoke-Expression
```

### Go Library

Tusk's core packages are importable, so other Go programs can embed tusk directly as a library without shelling out to the CLI or speaking MCP.

A high-level `Client` type in the root package wires up the database, migrations, and all services from a single `Config` struct:

```go
client, err := tusk.NewClient(tusk.Config{
    DBPath: "/tmp/my-tasks.db",
})
defer client.Close()

task := &domain.Task{
    Title:    "Build the thing",
    Priority: 3,
}
if err := client.Tasks.Create(ctx, task); err != nil {
    log.Fatal(err)
}
```

The `Client` exposes service instances as public fields (`Tasks`, `Tags`, `Relations`, `Projects`, `Workflows`, `Players`), so every operation available through CLI and MCP is available programmatically.

For consumers who need full control, the building-block packages (`domain`, `service`, `repository`, `sqlite`, `filter`, `config`) are importable directly. Custom storage backends can implement the repository interfaces without using the `Client` at all.

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
tusk task list project=backend +api priority=3..4 due=today..friday
tusk task list (status=active AND +urgent) OR priority=4
tusk task list claimed_by=agent-1
tusk task available unclaimed=true project=backend
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

Tusk uses a shared inline syntax across all commands — filters, task creation, modification, and config management. Entity properties flow through this syntax, never through ad-hoc command-line flags: `priority=3`, `project=backend`, `due=today`, `+tag`, `parent=a3f8b2c1`, `uda.env=prod`, and `description=@./spec.md` are all the same shape on both `tusk task create` and `tusk task modify`. There is exactly one way to set a field on a task, and the lexer/AST owns every entity-shaped input; command-line flags are reserved for invocation-level concerns that aren't entity properties (actor identity, output format, config scoping).

The syntax is built on a common lexer that understands three primitives:

- **Fields** — `key=value` pairs. The `=` separates key from value. `uda.key=value` on create and modify sets a user-defined attribute; an empty value on modify deletes the key. Bare `key=value` that is not a reserved field or a `uda.*` field is rejected as unknown — typos on UDA keys surface loudly instead of slipping through.
- **Token prefix modifiers** (`+`, `-`, …) — A neutral, extensible marker on the whole `key=value` or tag token. The lexer maintains an open registry of recognized prefix characters — currently `+` and `-`, with room for future additions — and records only which registered prefix a token carried, if any. The lexer lifts the prefix into the AST, attaches no meaning, and lets each consumer command interpret it. The same marker can mean different things in different contexts: in filters `+tag` includes and `-tag` excludes; in task commands `+tag` adds a tag and `-tag` removes one; in workflow config `+status=review`/`-status=review` append to and remove from a list; in project config on numeric urgency weights `+urgency.blocking-weight=2` adds 2 to the current value and `-urgency.blocking-weight=1` subtracts 1, while bare `urgency.blocking-weight=0` sets the absolute value. Commands are free to reject modifiers on fields where they don't make sense.
- **Value modifiers** — attached to a value half of a `key=value` pair:
  - `,` — Unordered set. `status=pending,active` is a set — order doesn't matter and duplicates are deduplicated.
  - `:` — Ordered sequence. `transition=pending:active` preserves order and allows duplicates — items appear in the sequence they were placed (from → to).
  - `..` — Range. `priority=2..4` defines a range in filters.
  - `()` — Group. Attaches structured metadata to a value. `status=pending(initial,highlight)` groups roles onto a status. Modifiers nest inside groups — the `,` inside `()` is a set within the group. Distinguished from boolean grouping by position: `(` immediately after a value (no whitespace) is a group modifier; `(` preceded by whitespace is a boolean grouping operator.
- **Quoted strings** — `title="some text"` for values containing spaces, with `\"` for escaped quotes. Quoted values are opaque to lexer modifier tokenization — `title="pending(initial)"` yields the plain string `pending(initial)`, not a value with a group. Quoting is the escape hatch for any value that would otherwise trigger a lexer modifier. Quoted values are **not** opaque to inline `@` reference expansion (see below): the lexer handles shell/lexer-level escaping, while `@@` handles literal `@` inside the final value.
- **Inline `@` reference expansion** — After the lexer has decoded a string field value (`description=`, `title=`, or a positional annotation body), free-form string fields run through an inline expander that substitutes file content or stdin at word-boundary `@` references. This is a pure consumer-layer text pass, not a lexer modifier — the mid-string case `description="see @./notes.md for details"` expands inline to produce a composite value.
  - **Word boundary** — `@` only triggers at the start of the value or after ASCII whitespace. `foo@bar.com` and `user@host` are never expanded, so email addresses and similar tokens pass through untouched.
  - **Bare path** — `@./spec.md` scans until the next whitespace; `~/` expands via `os.UserHomeDir` and absolute paths pass through.
  - **Quoted path** — `@"./name with space.txt"` scans a quoted span so paths containing spaces work.
  - **Escape** — `@@` at a word boundary collapses to a literal `@`, so `description="@@literal-at-sign in body"` stores a value that begins with `@`.
  - **Stdin** — `@-` reads standard input. Stdin may only be referenced once per invocation; a TTY guard rejects `@-` when stdin is not piped instead of hanging for keyboard input.
  - **Mid-string** — `description="see @./notes.md for background"` substitutes file content inline, producing a single string with the file body spliced where the reference sat.
  - **One level deep** — substituted content is **not** re-scanned for nested references; an `@` that appears inside loaded file content is treated as literal text.
  - **Size cap** — per-reference, configured via `inline.max_expansion_size` (default 1 MB). Over-cap files are rejected with actual size and limit in the error message.
  - **Binary detection** — content is rejected via a NUL-byte scan on the first 8 KB (git's approach); the error points at the future attachment support path rather than loading binary into a string field.
  - The same pipeline applies to positional string bodies, so `tusk task annotate <id> @./notes.md` and `tusk task annotate <id> @-` work with the same semantics as `description=@...`.

Token prefix modifiers and value modifiers are composable — groups can contain sets, sets can contain sequences, enabling recursive structure from the same primitives. Individual commands define which fields and modifiers they accept. The lexer tokenizes uniformly; domain-specific validators determine what's valid in each context.

---

## Concurrency Model

Tusk is designed for concurrent access from day one — multiple CLI invocations, parallel script commands, and rapid MCP tool calls from AI agents.

Every mutable entity carries a **version** field. Updates use optimistic locking: the write succeeds only if the version matches what was last read. On mismatch, the operation fails with a conflict error rather than silently overwriting. MCP responses include the current version so agents can pass it back on subsequent modifications, enabling end-to-end optimistic locking even across separate tool calls.

SQLite runs in WAL mode, allowing concurrent readers without blocking writers.

---

## Configuration

Tusk uses TOML configuration files with single-file resolution: it picks the nearest config file walking upward from the current directory, and only that file is active. There is no merging between user config files — the first match wins.

Resolution order (highest to lowest priority):

1. `--config <path>` flag
2. `TUSK_CONFIG` env var (single file path)
3. Nearest `tusk.toml` walking upward from CWD to filesystem root
4. Global `~/.config/tusk/config.toml`
5. Embedded defaults

The embedded default configuration is always the baseline. The active user file (if any) overrides individual keys on top of those defaults. Environment variables with the `TUSK_` prefix override any resolved value. The `--db` flag overrides the database path directly.

When `--config` or `TUSK_CONFIG` is set, a missing target file is a hard error — Tusk refuses to start rather than silently falling through to defaults. The global `~/.config/tusk/config.toml`, by contrast, falls through silently when missing and is auto-created from defaults on first run.

Relative paths inside a `tusk.toml` (`storage.path = "./tusk.db"`) resolve against the directory that contains the file, so running `tusk` from any subdirectory of a project lands on the same database.

A default configuration is embedded in the binary. Running `tusk config init` creates a global config file with defaults; `tusk config init --local` writes a full dump of the current effective config to `./tusk.toml`. Global config auto-creation on first run is skipped when a local `tusk.toml` is already in the walk-up path — running tusk inside a project with its own config never spawns a global file.

Configuration governs:

- **Storage** — database backend and connection settings (one database per config file)
- **Urgency weights** — global scoring defaults (projects can override these in the database)
- **MCP** — transport settings, tool/resource visibility, field-level write restrictions
- **TUI** — date format, color, tree indentation, default sort order
- **Dashboard** — refresh interval, layout, visible columns

Projects and workflows are *not* configuration — they live in the workspace database and are managed through `tusk project` and `tusk workflow` commands. `tusk config show` still renders `[projects.*]` and `[workflows.*]` sections for continuity, hydrated from the database at display time and marked read-only in the output.

### Config Management

Configuration can be inspected and modified from the CLI without manual file editing:

```bash
tusk config show                          # effective config, with active file path in header
tusk config path                          # print active config file path
tusk config get urgency.due_weight        # single value lookup
tusk config set urgency.due_weight 10.0   # write to active config file
tusk config set --global tui.color false  # force write to global config
tusk config edit                          # open active config in $EDITOR
tusk config validate                      # check for errors
```

Workflow and project management commands write directly to the workspace database, using the same inline `key=value` syntax as task modify. Like every other mutable entity, projects and workflows carry a `version` for optimistic locking. List fields support `+`/`-` prefixes for additive/subtractive operations on modify:

```bash
# Workflows
tusk workflow create sprint \
  status=pending(initial) status=active(start,highlight) \
  status=done(terminal,done,dim) \
  transition=pending:active,active:done
tusk workflow modify sprint +status=in-review +transition=active:in-review
tusk workflow modify sprint status=active(start,highlight)  # update roles
tusk workflow delete sprint

# Projects
tusk project create backend workflow=kanban
tusk project modify backend urgency.blocking-weight=15 auto-complete.trigger=completed
tusk project delete backend
```

### Workspace Scope

A config file defines a **workspace**: one database, scoped to the directory that contains the file. The directory acts as a namespace — tasks, projects, and workflows all live in the single database at `storage.path`, and that path resolves relative to the config file's own directory (unless absolute or `~`-prefixed).

Walking into a subdirectory of a project with a `tusk.toml` keeps you inside the same workspace: tusk picks the same config via walk-up, so the database (and everything inside it — projects, workflows, urgency overrides) stays consistent. Walking into an unrelated directory that has its own `tusk.toml` switches you to a different workspace with a different database, and therefore a different set of projects and workflows.

The global `~/.config/tusk/config.toml` defines a default workspace used whenever walk-up finds no local `tusk.toml`. Its `storage.path` defaults to `~/.local/share/tusk/tusk.db`.

There is no cross-workspace query. Each invocation operates on exactly one workspace — whichever one the active config file declares. Projects within a workspace still share a single database, so multi-project operations (`tusk task list`, `available`, `next`, `pop`) run against that one database, filtering by project when scoped.

---

## Storage

Tusk uses SQLite by default. Each workspace maps to a single database file (default global: `~/.local/share/tusk/tusk.db`; local workspaces typically point `storage.path` at a file inside the project directory). Migrations are embedded in the binary and run automatically, and WAL mode is enabled for concurrent access.

The storage layer is defined as a set of interfaces. SQLite is the shipped default. PostgreSQL is supported as an alternative backend for multi-user and networked deployments, with its own connection pooling and migration path. The interface boundary means adding a new backend requires no changes to the service layer.

---

## Data Portability

Tusk supports full data export for backup, migration, and interoperability:

- **JSON export** — complete dump of all tasks, relations, annotations, tags, and players.
- **CSV export** — flat tabular export of tasks for spreadsheet workflows.

Bidirectional sync allows merging task data across tusk instances. The sync protocol defines a conflict resolution strategy so that two instances that have diverged can be reconciled without data loss.
