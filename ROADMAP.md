# Tusk Roadmap

## Product Vision

Deliver a concurrent-safe, single-binary task management tool that combines CLI speed with structured hierarchy and workflow flexibility, accessible to both humans (TUI) and AI agents (MCP server).

---

## v0.1 — Foundation

**Goal:** Core task management with CLI, SQLite persistence, and basic workflow.

### Initiative: Core Domain & Storage

> Establish domain types, repository interfaces, and SQLite backend.

- [x] **Story: Domain model**
  - [x] Define core types (Task, Relation, Project, Workflow, Tag, Annotation)
  - [x] Define repository interfaces (TaskRepository, RelationRepository, ProjectRepository, TagRepository, WorkflowRepository, AnnotationRepository)
  - [x] Define sentinel errors (ErrNotFound, ErrConflict, ErrCyclicBlock, ErrInvalidTransition, ErrDuplicateRelation)

- [x] **Story: SQLite storage**
  - [x] Implement SQLite store with WAL mode, busy_timeout, foreign keys
  - [x] Write initial migration (001_initial.up.sql / down.sql)
  - [x] Implement TaskRepository for SQLite
  - [x] Implement ProjectRepository for SQLite
  - [x] Implement TagRepository for SQLite
  - [x] Implement AnnotationRepository for SQLite
  - [x] Implement WorkflowRepository for SQLite
  - [x] Implement RelationRepository for SQLite

### Initiative: Task Service & Workflow

> Business logic for task CRUD with workflow validation and optimistic locking.

- [x] **Story: TaskService with CRUD**
  - [x] Create tasks with UUID + 8-char short ID generation
  - [x] Read tasks by ID and short ID
  - [x] Update tasks with optimistic locking (version field)
  - [x] Soft-delete tasks via workflow transition

- [x] **Story: Workflow validation**
  - [x] Enforce status transitions per project workflow
  - [x] Seed default workflow (pending, active, completed, deleted)
  - [x] Reject invalid transitions with ErrInvalidTransition

### Initiative: CLI Interface

> Basic TUI commands for task management.

- [x] **Story: Core CLI commands**
  - [x] `tusk add` — create tasks with inline project, tags, priority
  - [x] `tusk list` — list tasks sorted by urgency
  - [x] `tusk info` — full detail view of a single task
  - [x] `tusk modify` — update task fields
  - [x] `tusk done` — shorthand for active → completed
  - [x] `tusk delete` — shorthand for → deleted
  - [x] `tusk annotate` — add annotation to a task

- [x] **Story: Tag support**
  - [x] TagService implementation
  - [x] Wire tags into `add` / `modify` / `list` commands
  - [x] `+tag` / `-tag` syntax in CLI

- [x] **Story: Filter syntax**
  - [x] Lexer for filter tokens
  - [x] Parser for filter expressions
  - [x] Resolver to map filters to repository queries
  - [x] Support: `status:`, `priority:`, `project:`, `+tag`, `-tag`, `due:`, ranges (`..`)

### Initiative: Testing

> Automated test coverage for CLI workflows.

- [x] **Story: E2E test harness**
  - [x] Build harness with multi-mode execution (DB config modes x output formats)
  - [x] Step reference system (`$0.short_id`)
  - [x] Cover core CLI commands end-to-end

---

## v0.2 — Relations & Hierarchy

**Goal:** Typed relations between tasks, parent-child hierarchy, and tag management.

### Initiative: Relations

> First-class typed edges between tasks with cycle detection.

- [x] **Story: RelationService**
  - [x] Create relations (blocks, relates_to, duplicates)
  - [x] Cycle detection via DFS on `blocks` edges
  - [x] Prevent duplicate relations (ErrDuplicateRelation)
  - [x] Derive inverse relations at query time

- [x] **Story: Relation CLI commands**
  - [x] `tusk link <source> <type> <target>` — create relation
  - [x] `tusk unlink <source> <type> <target>` — remove relation
  - [x] Display relations in `tusk info` output

### Initiative: Parent-Child Hierarchy

> Optional task nesting to arbitrary depth.

- [x] **Story: Parent-child task creation**
  - [x] `parent:<short_id>` on `tusk add` to create subtasks
  - [x] `parent:<short_id>` filter for listing direct children
  - [x] `tree:<short_id>` filter for listing all descendants

- [x] **Story: Tree CLI command**
  - [x] `tusk tree` — hierarchical indented view of all tasks
  - [x] E2E tests for tree display

- [x] **Story: Completion propagation**
  - [x] Add JSON `settings` column to projects table (migration)
  - [x] `ProjectSettings` with `AutoCompleteConfig` and `AutoRevertConfig` (configurable trigger/target statuses)
  - [x] `TaskTxProvider` for atomic propagation within same transaction
  - [x] Auto-transition parent when all non-deleted children reach trigger status
  - [x] Auto-revert parent when a child moves away from trigger status
  - [x] Recursive propagation up ancestor chain
  - [x] Workflow validation respected — propagation silently stops if transition invalid
  - [x] Disabled by default, opt-in per project via settings
  - [x] E2E tests for propagation scenarios

### Initiative: Project Management

> CLI commands for project CRUD and settings configuration.

- [x] **Story: `tusk project` subcommand**
  - [x] `tusk project list` — list all projects
  - [x] `tusk project create <name>` — create a project
  - [x] `tusk project modify <name>` — update project fields and settings
  - [x] Dot-path `--set` flag for JSON settings (e.g., `--set auto_complete_parent.trigger_status=completed`)

### Initiative: Tag Management

> Dedicated tag subcommand for CRUD operations.

- [x] **Story: `tusk tag` subcommand**
  - [x] `tusk tag create <name>` — create a tag
  - [x] `tusk tag list` — list all tags
  - [x] `tusk tag delete <name>` — delete a tag
  - [x] `tusk tag rename <old> <new>` — rename a tag

---

## v0.3 — MCP Server

**Goal:** Expose all capabilities via MCP protocol for AI agent integration.

### Initiative: MCP Server Core

> stdio-transport MCP server mapping tools to service methods.

- [x] **Story: MCP server with stdio transport**
  - [x] Server setup and lifecycle management
  - [x] stdio transport implementation
  - [x] Tool registration framework

- [x] **Story: Task tools**
  - [x] `tusk_task_create` — TaskService.Create
  - [x] `tusk_task_list` — TaskService.List
  - [x] `tusk_task_get` — TaskService.GetByShortID
  - [x] `tusk_task_modify` — TaskService.Update
  - [x] `tusk_task_start` — TaskService.Start
  - [x] `tusk_task_done` — TaskService.Complete
  - [x] `tusk_task_delete` — TaskService.Delete
  - [x] `tusk_task_annotate` — TaskService.Annotate
  - [x] `tusk_task_tree` — TaskService.Tree

- [x] **Story: Relation & project tools**
  - [x] `tusk_relation_add` — RelationService.Add
  - [x] `tusk_relation_remove` — RelationService.Remove
  - [x] `tusk_project_list` — ProjectService.List
  - [x] `tusk_project_create` — ProjectService.Create

### Initiative: MCP Resources

> Expose tasks, projects, and workflows as readable resources.

- [x] **Story: MCP resource definitions**
  - [x] `tusk://tasks/{short_id}` resource
  - [x] `tusk://projects/{name}` resource
  - [x] `tusk://projects/{name}/workflow` resource

### Initiative: MCP Concurrency

> End-to-end optimistic locking through MCP tool I/O.

- [x] **Story: Version passing**
  - [x] Include `version` in all task tool responses
  - [x] Accept `version` in modify/start/done/delete tool inputs
  - [x] Return ErrConflict on version mismatch

---

## v0.4 — Configuration & Customization

**Goal:** Viper-based configuration system with declarative workflow definitions, enabling custom statuses, transitions, and per-project workflow assignment.

### Initiative: Configuration System

> Viper-based config loading as foundation for all runtime settings.

- [ ] **Story: Viper config loader**
  - [ ] Add Viper dependency
  - [ ] Load config from `~/.config/tusk/config.toml` with fallback to hardcoded defaults
  - [ ] Support `TUSK_` environment variable prefix for all config keys
  - [ ] Wire config into `cmd/tusk/main.go` DI setup
  - [ ] Define `Config` struct covering `[urgency]`, `[tui]`, `[storage]`, `[workflows]`, and `[mcp]` sections

- [ ] **Story: MCP visibility config schema**
  - [ ] `[mcp.disabled_tool_groups]` — hide tools by group (e.g., `["workflow", "relation"]`)
  - [ ] `[mcp.disabled_tools]` — hide individual tools (e.g., `["tusk_workflow_list"]`)
  - [ ] `[mcp.disabled_resource_groups]` — hide resources by group
  - [ ] `[mcp.disabled_resources]` — hide individual resource templates

### Initiative: Declarative Workflows

> Config-driven workflow definitions synced to the database on startup.

- [ ] **Story: Workflow definitions in config**
  - [ ] Define `[workflows.<name>]` TOML schema for custom statuses and transitions
  - [ ] Sync config-defined workflows to DB on startup (create/update, respect existing data)
  - [ ] Preserve seed default workflow when no config is present

- [ ] **Story: Workflow CLI commands**
  - [ ] `tusk workflow list` — list all workflows with their statuses and transitions
  - [ ] `tusk workflow info <name>` — detailed view of a single workflow

- [ ] **Story: Per-project workflow assignment**
  - [ ] `[projects.<name>]` config section with `workflow` key
  - [ ] `tusk project modify <name> --workflow <workflow_name>` CLI support
  - [ ] Validate workflow exists before assignment

- [ ] **Story: MCP workflow tools**
  - [ ] `tusk_workflow_list` — list all workflows
  - [ ] Expose workflow assignment in `tusk_project_create` and project modify tools

### Initiative: MCP Configurability

> Config-driven control over which MCP tools and resources are exposed to agents.

- [ ] **Story: MCP visibility wiring**
  - [ ] Expose tool/resource groups as a convention in MCP registration (tag or prefix-based)
  - [ ] Filter tool and resource registration at server startup based on config
  - [ ] Validate config values against known tools/resources on startup (warn on unknown entries)

---

## v0.5 — Rich Content

**Goal:** Enable rich task descriptions and structured metadata for agent orchestration.

### Initiative: Rich Descriptions

> Full markdown descriptions with file-based input for detailed task specs.

- [ ] **Story: File-based description input**
  - [ ] `--description @file.md` syntax on `tusk add` and `tusk modify` to read content from a file
  - [ ] `tusk_task_create` and `tusk_task_modify` MCP tools accept full markdown descriptions
  - [ ] `tusk info` renders description in full (no truncation)

### Initiative: User-Defined Attributes

> Expose the existing `uda` JSON column via CLI and MCP. Note: `Task.UDA` field and `tasks.uda` JSON column already exist in the domain and schema.

- [ ] **Story: UDA CLI surface**
  - [ ] `--uda key=value` on `tusk add` and `tusk modify`
  - [ ] Display UDAs in `tusk info`
  - [ ] `tusk_task_create` and `tusk_task_modify` MCP tools accept UDA fields

- [ ] **Story: UDA filter support**
  - [ ] `uda.key:value` filter syntax
  - [ ] Expose in both CLI and MCP task list

---

## v0.6 — Urgency & UX

**Goal:** Smart task prioritization and polished terminal experience.

### Initiative: Advanced Filters

> Richer filter expressions for complex queries.

- [ ] **Story: Quoted string support in filters**
  - [ ] Enable `title:"some text"` and `description:"some text"` fields

- [ ] **Story: Boolean operators in filters**
  - [ ] `AND` / `OR` / `NOT` operators
  - [ ] Parenthesized grouping

### Initiative: TUI Polish

> Color, formatting, and quality-of-life improvements.

- [ ] **Story: Color-coded output**
  - [ ] Color by priority level
  - [ ] Color by status
  - [ ] Respect `NO_COLOR` / `--no-color` flag

- [ ] **Story: Tag colors**
  - [ ] CLI support for setting tag color (`tusk tag modify <name> --color <hex>`)
  - [ ] Display colored tags in list/info/tree output
  - [ ] Read default color settings from `[tui]` config section

### Initiative: Urgency Scoring

> Weighted multi-factor urgency algorithm for task ranking.

- [ ] **Story: Urgency engine**
  - [ ] Implement scoring with default weights (priority, due, age, status, blocking, blocked, tags, project, annotations, waiting)
  - [ ] Sigmoid curve for due-date urgency
  - [ ] Integrate urgency into default list sort

- [ ] **Story: Configurable urgency weights**
  - [ ] Read weights from config system (global defaults)
  - [ ] `tusk next` — display highest-urgency actionable task (can ship with engine story using hardcoded defaults if needed earlier)

- [ ] **Story: Per-project urgency overrides**
  - [ ] Extend `ProjectSettings` with urgency weight overrides
  - [ ] Merge project-level weights on top of global config at scoring time
  - [ ] Expose overrides via `tusk project modify --set` and MCP project tools

---

## v0.7 — Player Management

**Goal:** Track which player (human or agent) is working on which task, preventing overlapping work and enabling atomic task queue operations.

### Initiative: Player Entity & Registration

> Self-registering player model persisted to DB.

- [ ] **Story: Player domain and storage**
  - [ ] Define Player entity (`id` string PK, `type`, `registered_at`, `last_seen_at`)
  - [ ] PlayerRepository interface and SQLite implementation
  - [ ] Migration adding `players` table and `claimed_by`/`claimed_at` columns to `tasks`
  - [ ] PlayerService with Register and UpdateLastSeen methods
  - [ ] `ErrTaskClaimed` sentinel error

- [ ] **Story: Player CLI**
  - [ ] `tusk player register <id> --type human|agent` — explicit registration
  - [ ] `--player <id>` global flag for CLI (auto-registers on first use)

- [ ] **Story: MCP player registration**
  - [ ] `tusk_player_register` tool
  - [ ] `player_id` parameter on MCP tool calls (auto-registers on first use)
  - [ ] Update `last_seen_at` on every player action

### Initiative: Task Claiming

> Claim mechanics to prevent overlapping work between players.

- [ ] **Story: Claim and release**
  - [ ] TaskService.Claim — set `claimed_by`/`claimed_at`, reject if already claimed (`ErrTaskClaimed`)
  - [ ] TaskService.Release — clear claim, validate caller is the claimant
  - [ ] Auto-claim on `tusk start` if unclaimed, reject if claimed by another
  - [ ] Auto-release on `done` and `delete`
  - [ ] `tusk claim <id>` / `tusk release <id>` CLI commands
  - [ ] `tusk_task_claim` / `tusk_task_release` MCP tools

- [ ] **Story: Player visibility**
  - [ ] Include `claimed_by` and `claimed_at` in all task responses (CLI + MCP)
  - [ ] Filter support: `claimed_by:<player_id>`, `unclaimed:true`
  - [ ] `tusk available` — convenience: unclaimed + actionable status + not blocked
  - [ ] `tusk_task_available` MCP tool

### Initiative: Task Queue

> Atomic pop operation for efficient agent orchestration. Depends on urgency scoring (v0.6) to rank tasks.

- [ ] **Story: `tusk pop`**
  - [ ] TaskService.Pop — atomically find highest-urgency unclaimed unblocked task, claim for player, return it
  - [ ] `tusk pop --player <id>` CLI command
  - [ ] `tusk_task_pop` MCP tool with `player_id` input
  - [ ] Respect filters (optional: `tusk pop project:backend`)

---

## v0.8 — Live Dashboard

**Goal:** Real-time TUI dashboard for monitoring task state and player activity, powered by an event log.

### Initiative: Event Log

> Append-only event table recording all mutations. Foundation for dashboard, undo, and future webhooks.

- [ ] **Story: Event log infrastructure**
  - [ ] Define event types (task_created, task_modified, status_changed, task_claimed, task_released, task_completed, task_deleted, relation_added, relation_removed)
  - [ ] Migration adding `events` table (id, event_type, entity_id, player_id, payload JSON, created_at)
  - [ ] EventRepository interface and SQLite implementation
  - [ ] Emit events from TaskService, RelationService on every mutation
  - [ ] Bounded retention (configurable max events, prune on write)

### Initiative: TUI Dashboard

> Bubbletea-based live dashboard for orchestrator situational awareness.

- [ ] **Story: Task board view**
  - [ ] `tusk dashboard` �� long-running TUI command
  - [ ] Tasks organized by status columns (kanban-style)
  - [ ] Live updates via event log polling (1-2 second interval)
  - [ ] Color-coded by priority and claim status (bubbletea owns its own styling, independent of v0.6 TUI Polish)

- [ ] **Story: Player activity feed**
  - [ ] Activity stream panel showing recent events ("agent-1 claimed X", "german completed Y")
  - [ ] Filter by player or event type
  - [ ] Highlight stuck/idle players (claimed but no activity for configurable duration)

- [ ] **Story: Dashboard layout**
  - [ ] Split view: task board + activity feed
  - [ ] Keyboard navigation between panels
  - [ ] Configurable via `[dashboard]` config section (refresh interval, layout, visible columns)

---

## v0.9 — Advanced Features

**Goal:** Recurrence, additional transports, data portability, and undo.

### Initiative: UDA Schema Validation

> Per-project validation for user-defined attributes. Builds on the UDA CLI surface from v0.5.

- [ ] **Story: UDA schema validation**
  - [ ] Per-project UDA schema definition in `ProjectSettings`
  - [ ] Validate UDA values against schema on create/update

### Initiative: Data Portability

> Export capabilities for data backup and migration.

- [ ] **Story: Export**
  - [ ] `tusk export --format json` — full dump
  - [ ] `tusk export --format csv` — flat export

### Initiative: Recurrence

> Automatic task instance generation from RFC 5545 RRULE.

- [ ] **Story: RRULE support**
  - [ ] Parse RFC 5545 RRULE strings
  - [ ] Generate next instance on task completion
  - [ ] Handle recurrence edge cases (end date, count limit)

### Initiative: MCP Streamable HTTP Transport

> Network-accessible MCP server for multi-client scenarios. Targets Streamable HTTP (successor to deprecated SSE transport).

- [ ] **Story: Streamable HTTP transport**
  - [ ] Streamable HTTP transport implementation
  - [ ] `tusk mcp serve --transport http --port <port>`

### Initiative: Undo

> Revert the last mutation using the event log from v0.8.

- [ ] **Story: Undo command**
  - [ ] `tusk undo` — revert last mutation by reading event log and applying inverse
  - [ ] Support undo for task CRUD, status transitions, and claim operations

---

## Future

**Goal:** Scale to multi-user, richer interfaces, and deeper integrations.

### Initiative: PostgreSQL Backend

- [ ] **Story: PostgreSQL infrastructure**
  - [ ] Connection pooling, migration support, and test harness
  - [ ] PostgreSQL migration files mirroring SQLite schema

- [ ] **Story: Core PostgreSQL repositories**
  - [ ] TaskRepository for PostgreSQL
  - [ ] ProjectRepository for PostgreSQL
  - [ ] WorkflowRepository for PostgreSQL

- [ ] **Story: Supporting PostgreSQL repositories**
  - [ ] TagRepository for PostgreSQL
  - [ ] RelationRepository for PostgreSQL
  - [ ] AnnotationRepository for PostgreSQL

### Initiative: Interactive TUI

- [ ] **Story: Interactive task management**
  - [ ] Extend dashboard with inline task editing and status transitions
  - [ ] Task creation and modification without leaving the TUI

### Initiative: REST API

- [ ] **Story: HTTP REST API**
  - [ ] RESTful endpoints mirroring CLI/MCP capabilities
  - [ ] Authentication and authorization

### Initiative: Integrations & Extensions

- [ ] **Story: Webhook notifications**
  - [ ] Fire webhooks on task state changes (powered by event log from v0.8)

- [ ] **Story: Time tracking**
  - [ ] Start/stop timer on tasks
  - [ ] Report time spent

### Initiative: Binary Attachments

- [ ] **Story: File attachments**
  - [ ] Attach binary files to tasks (stored on filesystem, referenced in DB)
  - [ ] `tusk attach <id> <file>` / `tusk_task_attach` MCP tool
  - [ ] List and retrieve attachments via CLI and MCP

### Initiative: Bidirectional Sync

- [ ] **Story: Sync protocol**
  - [ ] Define sync format and conflict resolution strategy
  - [ ] `tusk sync export` / `tusk sync import` with merge semantics
