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
  - [x] Add JSON `settings` column to projects table (migration) — _settings moved to config in v0.4_
  - [x] `ProjectSettings` with `AutoCompleteConfig` and `AutoRevertConfig` (configurable trigger/target statuses)
  - [x] `TaskTxProvider` for atomic propagation within same transaction
  - [x] Auto-transition parent when all non-deleted children reach trigger status
  - [x] Auto-revert parent when a child moves away from trigger status
  - [x] Recursive propagation up ancestor chain
  - [x] Workflow validation respected — propagation silently stops if transition invalid
  - [x] Disabled by default, opt-in per project via settings
  - [x] E2E tests for propagation scenarios

### Initiative: Project Management

> CLI commands for project listing. Projects are config-driven (see v0.4 Config-based Projects); `create` and `modify` were removed in v0.4.

- [x] **Story: `tusk project` subcommand**
  - [x] `tusk project list` — list all projects

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

### Initiative: MCP Resources

> Expose tasks, projects, and workflows as readable resources.

- [x] **Story: MCP resource definitions**
  - [x] `tusk://tasks/{short_id}` resource
  - [x] `tusk://projects/{id}` resource
  - [x] `tusk://projects/{id}/workflow` resource

### Initiative: MCP Concurrency

> End-to-end optimistic locking through MCP tool I/O.

- [x] **Story: Version passing**
  - [x] Include `version` in all task tool responses
  - [x] Accept `version` in modify/start/done/delete tool inputs
  - [x] Return ErrConflict on version mismatch

---

## v0.4 — Configuration & Customization

**Goal:** Viper-based configuration system, config-driven projects and workflows, enabling custom statuses, transitions, and per-project workflow assignment — all without runtime DB tables for workflows.

### Initiative: Configuration System

> Viper-based config loading as foundation for all runtime settings.

- [x] **Story: Viper config loader**
  - [x] Add Viper dependency
  - [x] Load config from `~/.config/tusk/config.toml` with fallback to hardcoded defaults
  - [x] Support `TUSK_` environment variable prefix for all config keys
  - [x] Wire config into `cmd/tusk/main.go` DI setup
  - [x] Define `Config` struct covering `[urgency]`, `[tui]`, `[storage]`, `[workflows]`, and `[mcp]` sections

- [x] **Story: MCP visibility config schema**
  - [x] `[mcp.disabled_tool_groups]` — hide tools by group (e.g., `["workflow", "relation"]`)
  - [x] `[mcp.disabled_tools]` — hide individual tools (e.g., `["tusk_workflow_list"]`)
  - [x] `[mcp.disabled_resource_groups]` — hide resources by group
  - [x] `[mcp.disabled_resources]` — hide individual resource templates

### Initiative: Config-based Projects

> Projects become purely config-driven in-memory entities, same as workflows. Drop the `projects` table entirely. Project IDs become human-readable strings (e.g., `"default"`, `"backend"`). Tasks store `project_id` as a plain string column validated at the service layer against config — no FK constraint. A builtin `default` project exists when no config is present.

- [x] **Story: Drop projects table and migrate task references**
  - [x] Migration to drop `projects` table and remove FK constraint from `tasks.project_id`
  - [x] Migrate existing `tasks.project_id` UUID values to human-readable project IDs
  - [x] Update `tasks.project_id` column to plain TEXT (no FK)
  - [x] Drop `workflows.project_id` FK reference (handled by Declarative Workflows initiative)

- [x] **Story: Project config schema**
  - [x] Define `[projects.<id>]` TOML section with `workflow` and `settings` keys
  - [x] Add `ProjectsConfig` to `Config` struct
  - [x] Builtin `default` project with `kanban` workflow when no config is present
  - [x] Validate project config on load (referenced workflow must exist in config)

- [x] **Story: In-memory project repository and service**
  - [x] Rewrite `domain.Project` as config struct — `ID` (string), `Workflow` (string), `Settings` (ProjectSettings)
  - [x] Simplify `ProjectRepository` interface to read-only (`GetByID`, `List`)
  - [x] Implement in-memory `ProjectRepository` backed by config
  - [x] Remove SQLite `ProjectRepository` implementation
  - [x] Update `ProjectService` and `TaskService` for new interface
  - [x] Update CLI commands (`tusk project list`, remove `tusk project create`/`modify`)
  - [x] Update MCP tools (remove `tusk_project_create`, make project tools read-only)

### Initiative: Declarative Workflows

> Workflows become purely config-driven in-memory entities. Drop workflow DB tables entirely. A builtin `kanban` workflow provides the default. Projects reference a workflow by name, resolved from config at runtime.

- [x] **Story: Workflow config schema**
  - [x] Define `[workflows.<name>]` TOML schema with `statuses` and `transitions` keys
  - [x] Add `WorkflowsConfig` map to `Config` struct
  - [x] Builtin `kanban` workflow as default (pending, active, completed, deleted) when no config is present
  - [x] Validate workflow config on load (statuses referenced in transitions must exist, no orphan transitions)

- [x] **Story: Drop workflow DB tables**
  - [x] Migration to drop `workflow_transitions` and `workflows` tables
  - [x] Remove SQLite `WorkflowRepository` implementation
  - [x] Remove workflow seed data from migrations

- [x] **Story: In-memory workflow repository and service**
  - [x] Simplify `WorkflowRepository` interface (`GetByName`, `GetTransitions`, `List`)
  - [x] Implement in-memory `WorkflowRepository` backed by config
  - [x] Update `WorkflowService` for new interface
  - [x] Wire into DI in `cmd/tusk/main.go`
  - [x] Update `TaskService` if interface changes

- [x] **Story: Workflow CLI commands**
  - [x] `tusk workflow list` — list all workflows from config with their statuses and transitions
  - [x] `tusk workflow info <name>` — detailed view of a single workflow

- [x] **Story: MCP workflow tools**
  - [x] `tusk_workflow_list` — list all workflows from config
  - [x] Expose workflow name in project resource responses

### Initiative: MCP Configurability

> Config-driven control over which MCP tools and resources are exposed to agents.

- [x] **Story: MCP visibility wiring**
  - [x] Expose tool/resource groups as a convention in MCP registration (tag or prefix-based)
  - [x] Filter tool and resource registration at server startup based on config
  - [x] Validate config values against known tools/resources on startup (error on unknown entries)

---

## v0.5 — Rich Content

**Goal:** Enable rich task descriptions and structured metadata for agent orchestration.

### Initiative: Rich Descriptions

> Full markdown descriptions with file-based input for detailed task specs.

- [x] **Story: File-based description input**
  - [x] `--description @file.md` syntax on `tusk add` and `tusk modify` to read content from a file
  - [x] `tusk_task_create` and `tusk_task_modify` MCP tools accept full markdown descriptions
  - [x] `tusk info` renders description in full (no truncation)

### Initiative: User-Defined Attributes

> Expose the existing `uda` JSON column via CLI and MCP. Note: `Task.UDA` field and `tasks.uda` JSON column already exist in the domain and schema.

- [x] **Story: UDA CLI surface**
  - [x] `--uda key=value` on `tusk add` and `tusk modify`
  - [x] Display UDAs in `tusk info`
  - [x] `tusk_task_create` and `tusk_task_modify` MCP tools accept UDA fields

- [x] **Story: UDA filter support**
  - [x] `uda.key:value` filter syntax
  - [x] Expose in both CLI and MCP task list

---

## v0.6 — Urgency & UX

**Goal:** Smart task prioritization and polished terminal experience.

### Initiative: Advanced Filters

> Richer filter expressions for complex queries.

- [x] **Story: Quoted string support in filters**
  - [x] Enable `title:"some text"` and `description:"some text"` fields
  - [x] `description:` filter field for CLI and MCP task list

- [x] **Story: Boolean operators in filters**
  - [x] `AND` / `OR` / `NOT` operators
  - [x] Parenthesized grouping

### Initiative: TUI Polish

> Color, formatting, and quality-of-life improvements.

- [x] **Story: Color-coded output**
  - [x] Color by priority level
  - [x] Color by status
  - [x] Respect `NO_COLOR` / `--no-color` flag

- [x] **Story: Tag colors**
  - [x] CLI support for setting tag color (`tusk tag modify <name> --color <hex>`)
  - [x] Display colored tags in list/info/tree output
  - [x] Read default color settings from `[tui]` config section

- [x] **Story: Markdown description rendering**
  - [x] Terminal-rendered markdown in `tusk info` using glamour (charmbracelet)
  - [x] Respect `NO_COLOR` / `--no-color` flag for plain text fallback

### Initiative: Urgency Scoring

> Weighted multi-factor urgency algorithm for task ranking.

- [x] **Story: Urgency engine**
  - [x] Implement scoring with default weights (priority, due, age, status, blocking, blocked, tags, project, annotations, waiting)
  - [x] Sigmoid curve for due-date urgency
  - [x] Integrate urgency into default list sort

- [x] **Story: Configurable urgency weights**
  - [x] Read weights from config system (global defaults)
  - [x] `tusk next` — display highest-urgency actionable task (can ship with engine story using hardcoded defaults if needed earlier)

- [x] **Story: Per-project urgency overrides**
  - [x] Extend `ProjectSettings` with urgency weight overrides in config
  - [x] Merge project-level weights on top of global config at scoring time
  - [x] Expose overrides via `[projects.<id>.settings]` config section

---

## v0.7 — Player Management

**Goal:** Track which player (human or agent) is working on which task, preventing overlapping work and enabling atomic task queue operations.

### Initiative: Player Entity & Registration

> Self-registering player model persisted to DB.

- [x] **Story: Player domain and storage**
  - [x] Define Player entity (`id` string PK, `type`, `registered_at`, `last_seen_at`)
  - [x] PlayerRepository interface and SQLite implementation
  - [x] Migration adding `players` table and `claimed_by`/`claimed_at` columns to `tasks`
  - [x] PlayerService with Register and UpdateLastSeen methods
  - [x] `ErrTaskClaimed` sentinel error

- [x] **Story: Player CLI**
  - [x] `tusk player register <id> --type human|agent` — explicit registration
  - [x] `--player <id>` global flag for CLI (auto-registers on first use)

- [x] **Story: MCP player registration**
  - [x] `tusk_player_register` tool
  - [x] `player_id` parameter on MCP tool calls (auto-registers on first use)
  - [x] Update `last_seen_at` on every player action

### Initiative: Task Claiming

> Claim mechanics to prevent overlapping work between players.

- [x] **Story: Claim and release**
  - [x] TaskService.Claim — set `claimed_by`/`claimed_at`, reject if already claimed (`ErrTaskClaimed`)
  - [x] TaskService.Release — clear claim, validate caller is the claimant
  - [x] Auto-claim on `tusk start` if unclaimed, reject if claimed by another
  - [x] Claims preserved after `done` and `delete` (historical attribution — design decision, replaces auto-release)
  - [x] `tusk claim <id>` / `tusk release <id>` CLI commands
  - [x] `tusk_task_claim` / `tusk_task_release` MCP tools

- [x] **Story: Player visibility**
  - [x] Include `claimed_by` and `claimed_at` in all task responses (CLI + MCP)
  - [x] Filter support: `claimed_by:<player_id>`, `unclaimed:true`

### Initiative: Task Queue

> Atomic pop operation for efficient agent orchestration. Depends on urgency scoring (v0.6) to rank tasks.

- [x] **Story: Available tasks**
  - [x] `tusk available` — convenience: unclaimed + actionable status + not blocked
  - [x] `tusk_task_available` MCP tool

- [x] **Story: `tusk pop`**
  - [x] TaskService.Pop — atomically find highest-urgency unclaimed unblocked task, claim for player, return it
  - [x] `tusk pop --player <id>` CLI command
  - [x] `tusk_task_pop` MCP tool with `player_id` input
  - [x] Respect filters (optional: `tusk pop project:backend`)

---

## v0.8 — Programmatic Access

**Goal:** Expose tusk's core APIs as importable Go packages so other programs can embed tusk as a library.

### Initiative: Package Restructure

> Move core packages out of `internal/` to top-level, making them importable by external Go programs.

- [x] **Story: Move foundational packages (domain, config)**
  - [x] Move `internal/domain` → `domain`
  - [x] Move `internal/config` → `config`
  - [x] Update all import paths across the codebase

- [x] **Story: Move interface and filter packages (repository, filter)**
  - [x] Move `internal/repository` → `repository`
  - [x] Move `internal/filter` → `filter`
  - [x] Update all import paths across the codebase

- [x] **Story: Move service and storage packages (service, sqlite, inmem)**
  - [x] Move `internal/service` → `service`
  - [x] Move `internal/sqlite` → `sqlite`
  - [x] Move `internal/inmem` → `inmem`
  - [x] Update all import paths across the codebase
  - [x] Verify `internal/tui` and `internal/mcp` remain in `internal/`

### Initiative: High-level Client

> Convenience `Client` type in the root package that wires up config, DB, and services for consumers.

- [x] **Story: Client type and constructor**
  - [x] Define `Config` struct (DBPath, Workflows, Projects, Urgency)
  - [x] Implement `NewClient(Config) (*Client, error)` — open DB, run migrations, wire services
  - [x] Implement `Close() error` for cleanup
  - [x] Expose services as public fields (Tasks, Tags, Relations, Projects, Workflows, Players)
  - [x] Default to builtin kanban workflow and default project when config fields are zero-valued

- [x] **Story: Client tests**
  - [x] Test NewClient opens DB and creates task successfully
  - [x] Test NewClient with zero-valued config uses defaults
  - [x] Test NewClient with empty DBPath returns error
  - [x] Test Close releases DB connection

---

## v0.9 — Configuration Management

**Goal:** CLI commands for managing configuration without manual TOML editing — create, modify, and inspect workflows, projects, and settings from the terminal.

### Initiative: Config CLI

> Read, write, and validate configuration from the command line.

- [ ] **Story: Config inspection**
  - [ ] `tusk config show` — display current effective configuration (merged defaults + file + env)
  - [ ] `tusk config get <key>` — get a specific value using dot notation (e.g., `urgency.due_weight`)
  - [ ] `tusk config path` — print resolved config file path

- [ ] **Story: Config mutation**
  - [ ] `tusk config set <key> <value>` — set a config value and write to file
  - [ ] `tusk config edit` — open config file in `$EDITOR`
  - [ ] `tusk config init` — create config file with defaults if none exists (no-op if file present)

- [ ] **Story: Config validation**
  - [ ] `tusk config validate` — parse and validate config, report errors (unknown keys, invalid references, type mismatches)
  - [ ] Run validation on `config set` before writing

### Initiative: Workflow Management CLI

> Create, modify, and remove workflows via CLI commands that write to the config file.

- [ ] **Story: Workflow CRUD commands**
  - [ ] `tusk workflow create <name> [key:value...] --status <status> --transition from:to` — inline key:value for simple fields (e.g., `highlight:active`, `dim:completed,deleted`); `--status` and `--transition` flags for structural parts (repeatable)
  - [ ] `tusk workflow modify <name> [key:value...]` — inline key:value syntax (e.g., `highlight:active,in-review`, `dim:completed,deleted`); transitions use `--transition from:to` / `--rm-transition from:to` flags, statuses use `--add-status` / `--rm-status`
  - [ ] `tusk workflow delete <name>` — remove workflow from config (reject if referenced by a project)

### Initiative: Project Management CLI

> Create, modify, and remove projects via CLI commands that write to the config file.

- [ ] **Story: Project CRUD commands**
  - [ ] `tusk project create <name> [key:value...]` — inline key:value syntax (e.g., `workflow:kanban`, `db-path:/data/b.db`, `auto-complete.trigger:completed`, `urgency.blocking-weight:15`)
  - [ ] `tusk project modify <name> [key:value...]` — inline key:value syntax matching task modify (e.g., `workflow:scrum`, `db-path:/data/b.db`, `auto-complete.trigger:completed`, `urgency.blocking-weight:15`)
  - [ ] `tusk project delete <name>` — remove project from config (reject if tasks reference it)

- [ ] **Story: Per-project database path**
  - [ ] `[projects.<name>].db_path` config key — optional SQLite file path per project
  - [ ] Projects without `db_path` use the global `storage.path`
  - [ ] Open and migrate per-project DB on first access, reuse connection for subsequent operations
  - [ ] `db-path:/path/to/file.db` supported in project create/modify inline syntax
  - [ ] Cross-project commands (e.g., unfiltered `tusk list`) query all project databases and merge results

### Initiative: Local Config Discovery

> Walk-up config resolution analogous to `package.json` in Node.js — tusk uses the nearest config file from the CWD upwards, enabling project-scoped configuration alongside the global config.

- [ ] **Story: Config resolution chain**
  - [ ] Resolution order (highest to lowest priority): CWD `tusk.toml` → `~/.config/tusk/config.toml` → walk upward from CWD to filesystem root looking for `tusk.toml`
  - [ ] `--config <path>` flag bypasses discovery and uses the given file directly

- [ ] **Story: Config layering**
  - [ ] Merge all discovered configs in resolution order — local overrides global, global overrides ancestor
  - [ ] `tusk config show` displays effective merged config with source annotations (local / global / ancestor path)
  - [ ] `tusk config set` writes to the local config when present, global otherwise; `--global` flag forces global

- [ ] **Story: Config init for local projects**
  - [ ] `tusk config init --local` creates a `tusk.toml` in CWD with minimal defaults
  - [ ] Local config can scope storage path, projects, workflows, and urgency weights to the directory tree

### Initiative: MCP Config Tools

> Expose configuration management to AI agents via MCP tools.

- [ ] **Story: Config MCP tools**
  - [ ] `tusk_config_show` — read effective configuration
  - [ ] `tusk_config_set` — set a config value
  - [ ] `tusk_workflow_create` / `tusk_workflow_modify` / `tusk_workflow_delete` — workflow management
  - [ ] `tusk_project_create` / `tusk_project_modify` / `tusk_project_delete` — project management

---

## v0.10 — Live Dashboard

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

## v0.11 — Advanced Features

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

> Revert the last mutation using the event log from v0.10.

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

- [ ] **Story: Supporting PostgreSQL repositories**
  - [ ] TagRepository for PostgreSQL
  - [ ] RelationRepository for PostgreSQL
  - [ ] AnnotationRepository for PostgreSQL

Note: ProjectRepository and WorkflowRepository are in-memory (config-backed) and do not need PostgreSQL implementations.

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
  - [ ] Fire webhooks on task state changes (powered by event log from v0.10)

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
