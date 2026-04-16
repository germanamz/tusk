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

- [x] **Story: Config inspection**
  - [x] `tusk config show` — display current effective configuration (merged defaults + file + env)
  - [x] `tusk config get <key>` — get a specific value using dot notation (e.g., `urgency.due_weight`)
  - [x] `tusk config path` — print resolved config file path

- [x] **Story: Config mutation**
  - [x] `tusk config set <key> <value>` — set a config value and write to file
  - [x] `tusk config edit` — open config file in `$EDITOR`
  - [x] `tusk config init` — create config file with defaults if none exists (no-op if file present)

- [x] **Story: Config validation**
  - [x] `tusk config validate` — parse and validate config, report errors (unknown keys, invalid references, type mismatches)
  - [x] Run validation on `config set` before writing

### Initiative: Inline Syntax Migration

> Extract shared parsing infrastructure from the filter package and migrate all CLI inline syntax from `key:value` to `key=value`, freeing `:` for use within values (e.g., workflow transitions `pending:active`). Prerequisite for Workflow and Project Management CLI initiatives.

- [x] **Story: Extract shared lexer and AST**
  - [x] Extract generic lexer (tokenization, quoted strings, `key=value` fields) from `filter/` into a shared parsing package
  - [x] First-class modifier support in the lexer — primitives: `+` (additive), `-` (subtractive), `,` (unordered set, deduplicated), `:` (ordered sequence, no dedup), `..` (range), `()` (group); extensible to future modifiers
  - [x] Composable modifiers — modifiers nest within groups: `status=pending(initial,highlight)` contains a `,` set inside a `()` group; the lexer's modifier system is recursive
  - [x] Position-based `()` disambiguation — `(` immediately after a value (no whitespace) is a group modifier on that value; `(` preceded by whitespace is a boolean grouping operator; no per-application configuration needed
  - [x] Quoted strings are opaque — `"value"` is a literal string, no modifier tokenization inside; `title="pending(initial)"` yields the plain string `pending(initial)`
  - [x] Extract AST types (`FieldFilter`, `TagFilter`, free text) into the shared package
  - [x] Filter package and future consumers define domain-specific token lists and field validators on top of the shared foundation

- [x] **Story: Migrate `key:value` to `key=value` across CLI**
  - [x] Update lexer field detection from `:` to `=` separator
  - [x] Update all CLI commands (`add`, `modify`, `list`, `available`, `pop`, etc.)
  - [x] Covers all `key:value` patterns across the codebase: filter fields from v0.1 (`status:`, `priority:`, `project:`, `due:`), quoted strings from v0.6 (`title:`, `description:`), claim filters from v0.7 (`claimed_by:`, `unclaimed:`), UDA filters from v0.5 (`uda.key:`), and inline syntax on `add`/`modify`
  - [x] Update filter syntax documentation and help text
  - [x] Update E2E tests for new syntax

### Initiative: Workflow Management CLI

> Create, modify, and remove workflows via CLI commands that write to the config file. Mutation logic lives in the `config` package for reuse by MCP tools.

- [x] **Story: Workflow CRUD commands**
  - [x] `tusk workflow create <name> [fields...]` — all-inline syntax: `status=pending(initial) status=active(start) status=completed(terminal,done) status=deleted(terminal,delete) transition=pending:active,active:completed,active:deleted`
  - [x] `tusk workflow modify <name> [fields...]` — replace: `status=active(start,highlight)`; additive: `+status=review +transition=active:review`; subtractive: `-status=review -transition=active:review`
  - [x] `tusk workflow delete <name>` — remove workflow from config (reject if referenced by a project)

- [x] **Story: Status roles schema**
  - [x] Change `WorkflowConfig.Statuses` from `[]string` to `map[string]StatusConfig` — each status carries a `roles` list
  - [x] Built-in roles: `initial` (default for new tasks), `start` (target for `tusk start`/`pop`), `terminal` (task is finished), `done` (target for `tusk done`), `delete` (target for `tusk delete`), `highlight` (emphasized in output), `dim` (deemphasized in output)
  - [x] Remove top-level `highlight_statuses` and `dim_statuses` fields — replaced by `highlight` and `dim` roles on individual statuses
  - [x] Migration of existing `WorkflowConfig`: map `highlight_statuses`/`dim_statuses` lists to roles on the corresponding status entries
  - [x] Replace hardcoded `"pending"` fallback in `TaskService.Create` — look up the status with `initial` role
  - [x] Replace hardcoded `"active"` in `TaskService.Start`/`Pop` — look up the status with `start` role
  - [x] Replace hardcoded `"completed"` in `TaskService.Complete` — look up the status with `done` role
  - [x] Replace hardcoded `"deleted"` in `TaskService.Delete` — look up the status with `delete` role
  - [x] Replace hardcoded `"pending","active"` in `TaskService.Available`/`Pop` — derive actionable statuses as those without the `terminal` role
  - [x] Validation: exactly one `initial` status; exactly one `start` status with valid transition from `initial`; at least one `terminal` status; `done` and `delete` roles must be on statuses that also have `terminal`; at most one status per `initial`, `start`, `done`, `delete` role

- [x] **Story: Config package workflow mutations**
  - [x] `config.CreateWorkflow(name, WorkflowConfig)` — add workflow to config, validate, write
  - [x] `config.ModifyWorkflow(name, WorkflowMutation)` — apply field changes (replace/add/remove), validate, write
  - [x] `config.DeleteWorkflow(name)` — validate no project references, remove, write

### Initiative: Inline Syntax Modifier AST

> Promote the `+`/`-` token prefix to a first-class AST property with both list-op and arithmetic-op variants. Prerequisite for Project Management CLI urgency weight mutations; lets commands stop hand-rolling prefix parsing.

- [x] **Story: Field modifier AST**
  - [x] Extend `syntax.FieldFilter` with a `Modifier` field carrying the raw prefix rune only (empty = bare). No domain semantics attached at the AST level.
  - [x] Treat the set of recognized modifier prefixes as an open, extensible registry in the syntax package — initially `+` and `-`, designed so new prefixes (e.g. `?`, `*`) can be added without changing the `FieldFilter` shape or the consumer-facing API. Adding a new prefix is a one-line registry change plus consumer opt-in.
  - [x] Lexer consults the registry when scanning a token's first character: if the char is a registered modifier and is followed by a field/tag body, strip it into the AST marker; otherwise treat it as part of the value
  - [x] `FieldFilter.Key`/`FieldFilter.Value` always expose the bare form; modifier carried separately so consumers pattern-match on it without re-parsing strings
  - [x] The syntax package is explicit that it does not interpret modifiers — whether `+` means "append to a list", "add arithmetically", "include", or something else is entirely the consumer command's decision. The same neutral AST shape serves all of them, and the same applies to any future modifier.
  - [x] Migrate `internal/tui/workflow_parse.go` to read `FieldFilter.Modifier` instead of inspecting string prefixes — the workflow command interprets `+`/`-` as list add/remove on `status`/`transition`
  - [x] Migrate filter and task add/modify parsers to use the same field; list/tag semantics they assign are unchanged externally
  - [x] Unit tests cover lexing each registered modifier into the AST with no semantic interpretation, plus a "register a new modifier" test that proves the extensibility path works without touching consumer code. Consumer-level tests live in their respective packages and cover the interpretation layer.

### Initiative: Project Management CLI ✅

> Create, modify, and remove projects via CLI commands that write to the config file.

- [x] **Story: Project CRUD commands**
  - [x] `tusk project create <name> [fields...]` — inline syntax: `workflow=kanban db-path=/data/b.db auto-complete.trigger=completed urgency.blocking-weight=15`
  - [x] `tusk project modify <name> [fields...]` — inline syntax: bare assignment replaces (`workflow=sprint`, `urgency.blocking-weight=10`); `+key=value`/`-key=value` apply arithmetic deltas on numeric urgency weights (`+urgency.blocking-weight=2` adds 2, `-urgency.blocking-weight=1` subtracts 1)
  - [x] Numeric delta resolution: when the project override is unset, the delta applies relative to the effective global urgency weight and the result is written as a new project override
  - [x] Accepted fields: `workflow`, `db-path`, `auto-complete.trigger`, `auto-complete.target`, `auto-revert.trigger`, `auto-revert.target`, and every `urgency.<weight>` key
  - [x] `tusk project delete <name>` — removes project from config; rejects if any tasks reference it; rejects deleting the built-in `default` project; `--force` bypasses both guards and emits a warning with the task count
  - [x] Config package mutations: `config.CreateProject`, `config.ModifyProject`, `config.DeleteProject` — mirror the workflow mutation helpers, reusable by MCP tools
  - [x] Task-reference check passes a callback into `config.DeleteProject` so the config package stays free of service/repository imports

- [x] **Story: Per-project database path**
  - [x] `[projects.<name>].db_path` config key — optional SQLite file path per project; `db-path=...` in inline syntax writes it, `db-path=` clears it
  - [x] Paths resolve relative to the config file's directory (absolute paths used as-is, `~` expanded); projects without `db_path` use the global `storage.path`
  - [x] Store registry: lazily open and migrate per-project databases on first access, reuse the connection for subsequent operations, close all on shutdown
  - [x] Task, annotation, relation, and tag repositories are bundled per store; services resolve the correct bundle via the registry using the task's project ID
  - [x] Cross-project reads (unfiltered `tusk list`, `available`, `next`) fan out across all known stores and merge results, re-sorting by urgency in memory before applying limit/offset
  - [x] `tusk pop` picks the highest-urgency candidate across stores, then claims it in its own store with retry on optimistic-lock conflict
  - [x] Relations must link tasks within the same store — `RelationService.Create` rejects cross-store links to preserve referential integrity; documented as a per-project-DB constraint

### Initiative: Workspace Scope Collapse

> Internal refactor that makes the config file's directory the one workspace namespace. Removes per-project `db_path`, collapses `StoreRegistry` to a single store, and drops cross-store fan-out. No user-visible config resolution changes yet — tusk still reads the global `config.toml` only. Ships first so the rest of the config work has a simple "one config, one DB" invariant to build on.

- [x] **Story: Remove per-project db_path from the config schema**
  - [x] Delete `[projects.<name>].db_path` from the config types and TOML schema
  - [x] Remove `db-path=` from `tusk project create` / `tusk project modify` inline syntax, and remove it from the accepted-fields list
  - [x] Update `config/default.toml` comments and any docs-embedded examples
  - [x] Explicitly supersedes the v0.8 "Per-Project Databases" stories, which remain in the roadmap as historical record

- [x] **Story: Collapse StoreRegistry to a single workspace store**
  - [x] Replace `StoreRegistry` with a single `Store` opened from `cfg.Storage.Path` at startup
  - [x] Service resolver (`RepoBundle` provider) returns the workspace store regardless of project ID
  - [x] `baseDir` for relative `storage.path` still resolves against the active config file's directory (unchanged contract, simpler implementation)

- [x] **Story: Remove cross-store fan-out from services**
  - [x] `TaskService.List`, `available`, `next` no longer fan out across stores — they run one query against the workspace store with project filters applied in SQL
  - [x] `tusk pop` selects the top-urgency candidate via a single query and claims it in the same store; optimistic-lock retry stays but cross-store retry logic is removed
  - [x] `RelationService` drops the same-store constraint and re-allows relations between tasks in different projects within the workspace
  - [x] `projectLister` closure in `cmd/tusk` is replaced by reading project IDs from the config

- [x] **Story: Migration guidance for existing per-project DBs**
  - [x] Documented in `docs/` as a manual export/import procedure (export each per-project DB with `tusk export --format json`, merge into the new workspace DB)
  - [x] No automatic migration shipped — per-project DBs predate v0.1 and had no production users
  - [x] Release notes flag it as a breaking change for any user who set `db_path` in their config

### Initiative: Explicit Config File Resolver

> Introduce the config resolution abstraction that walk-up discovery will later plug into. Adds the `--config` flag and `TUSK_CONFIG` env var as first-class ways to point tusk at any config file, plus `config path` and an active-file header on `config show`. No walk-up yet — the resolver's precedence chain is `--config` → `TUSK_CONFIG` → global → defaults. Delivered on top of the single-workspace model from the previous initiative.

- [x] **Story: Config resolver abstraction**
  - [x] Introduce `ResolveConfigFile(startDir, explicitFile, globalDir) (string, error)` in `config/` that returns the active config file path or `""` for "defaults only"
  - [x] Initial implementation: returns `explicitFile` if set, otherwise `globalDir/config.toml` if it exists, otherwise `""`. Walk-up step is reserved for the next initiative.
  - [x] `config.Load()` routes through the resolver; legacy `WithSearchPath` option is preserved for tests but documented as "sets globalDir"
  - [x] `config.Load()` returns the resolved file path alongside the `*Config` (e.g. via a `Sources` field or a second return value) so callers can render it

- [x] **Story: `--config` flag and `TUSK_CONFIG` env var**
  - [x] Add global `--config <path>` flag handled before Cobra parsing, parallel to the existing `--db` handling in `cmd/tusk/main.go`
  - [x] Add `TUSK_CONFIG` env var as a fallback for `--config`. `TUSK_CONFIG_DIR` remains valid and untouched (it sets `globalDir`).
  - [x] Missing `--config` / `TUSK_CONFIG` target file is a hard error at `Load()` time; missing global file falls through to defaults silently
  - [x] Precedence at this point: `TUSK_*` env values > `--config` / `TUSK_CONFIG` file > global file > embedded defaults

- [x] **Story: `config path` and active-file header**
  - [x] New `tusk config path` subcommand prints the resolved active file path, or the path `tusk config init` would create when none is active
  - [x] `tusk config show` prepends a header indicating which file is active (`# active: /path/to/config.toml` or `# active: defaults only`)
  - [x] `tusk config edit` opens the resolved active file (honoring `--config` / `TUSK_CONFIG`)
  - [x] `tusk config validate` validates the resolved file

### Initiative: Local Config Discovery

> Walk-up config resolution analogous to `package.json` in Node.js. Extends the resolver from the previous initiative with a walk-up step so tusk picks the nearest `tusk.toml` from the CWD upward, falling back to the global `config.toml` when the walk finds nothing. Single-file model — first match wins, no merging between user configs. Also lands the workspace-aware write commands (`config set`, `config init --local`).

- [x] **Story: Walk-up step in the resolver**
  - [x] Insert walk-up between `TUSK_CONFIG` and global in `ResolveConfigFile`: starting at CWD, check each ancestor directory for `tusk.toml` and return the first hit
  - [x] Walk stops at filesystem root; no symlink resolution
  - [x] Walk is skipped entirely when `--config` or `TUSK_CONFIG` is set (the bypass stays authoritative)
  - [x] Final precedence: `TUSK_*` env > `--config` > `TUSK_CONFIG` > walk-up `tusk.toml` > global `config.toml` > embedded defaults

- [x] **Story: Relative paths resolve to the config file's directory**
  - [x] `storage.path` and any other file-path field resolve relative to the directory that contains the active config file, not the caller's CWD
  - [x] `tusk` run from any subdirectory of a project with a `tusk.toml` at the root hits the same database as `tusk` run from the root itself
  - [x] Absolute paths and `~`-prefixed paths are untouched

- [x] **Story: Workspace-aware `config set`**
  - [x] `tusk config set <key> <value>` writes to the file `Load()` resolved — whichever `tusk.toml` or `config.toml` is active
  - [x] `--global` flag forces writes to `~/.config/tusk/config.toml` even when a walk-up `tusk.toml` is active
  - [x] With no active file and no `--global`, emit a clear error pointing at `tusk config init` or `tusk config init --local`

- [x] **Story: `config init --local`**
  - [x] `tusk config init --local` creates `./tusk.toml` containing a full dump of the current effective config
  - [x] Errors if `./tusk.toml` already exists
  - [x] `tusk config init` (no flag) still writes global defaults as today

- [x] **Story: Conditional global auto-create**
  - [x] `config.Load()` auto-creates `~/.config/tusk/config.toml` on first run only when walk-up finds no `tusk.toml` and no `--config` / `TUSK_CONFIG` override is set
  - [x] Running tusk inside a project with a local config never spawns a global file
  - [x] Existing behavior preserved for fresh installs operating outside any tusk project

- [x] **Story: `config show` / `config path` report walk-up hits**
  - [x] Active-file header on `config show` correctly reflects walk-up discoveries (e.g. `# active: /repo/tusk.toml`)
  - [x] `config path` prints the walk-up hit when one is active, the global path otherwise
  - [x] E2E coverage: subdirectory walk-up, ancestor walk-up, `--config` override, `TUSK_CONFIG` override, no-config fallthrough

### Initiative: MCP Config Tools

> Expose configuration management to AI agents via MCP tools.

- [x] **Story: Config MCP tools**
  - [x] `tusk_config_show` — read effective configuration
  - [x] `tusk_config_set` — set a config value
  - [x] `tusk_workflow_create` / `tusk_workflow_modify` / `tusk_workflow_delete` — workflow management
  - [x] `tusk_project_create` / `tusk_project_modify` / `tusk_project_delete` — project management

---

## v0.10 — Datastore-Backed Projects & Workflows

**Goal:** Move projects and workflows out of the config file and into the workspace database. With config files now acting as workspace namespaces (walk-up discovery, local `tusk.toml`), projects and workflows are workspace data, not user configuration. Tasks already live in the DB — projects and workflows should too.

### Initiative: Project & Workflow Schema

> Persistent storage for projects and workflows in the workspace database, with optimistic locking like every other mutable entity.

- [x] **Story: Projects table**
  - [x] Define `domain.Project` entity (`id` UUID, `name`, `workflow_id` UUID, `settings` JSON, `version`, `created_at`, `updated_at`)
  - [x] `settings` JSON carries `auto_complete`, `auto_revert`, and `urgency` overrides — JSON chosen over dedicated columns because per-project overrides are read once per service call and written rarely; promote to columns only if profiling shows the JSON decode is hot
  - [x] Migration adding `projects` table with unique index on `name`
  - [x] Seed built-in `_default` project (UUID all zeros) as a regular row in the migration — no special-case code paths
  - [x] `ProjectRepository` interface (`Create`, `Get`, `GetByName`, `List`, `Update`, `Delete`) and SQLite implementation with version-checked updates returning `domain.ErrConflict`

- [x] **Story: Workflows table**
  - [x] Define `domain.Workflow` entity (`id` UUID, `name`, `statuses` JSON, `transitions` JSON, `version`, `created_at`, `updated_at`)
  - [x] Statuses keep the v0.9 role schema (`initial`, `start`, `terminal`, `done`, `delete`, `highlight`, `dim`) — serialized as JSON to avoid a second table just for status rows
  - [x] Migration adding `workflows` table with unique index on `name`
  - [x] Seed built-in default workflow (`pending`/`active`/`completed`/`deleted` with roles) as a regular row in the migration
  - [x] `WorkflowRepository` interface (`Create`, `Get`, `GetByName`, `List`, `Update`, `Delete`) and SQLite implementation with version-checked updates

- [x] **Story: Foreign key from tasks to projects**
  - [x] Migration converts `tasks.project_id` to a real FK referencing `projects.id` (was previously just a UUID column with no DB-level integrity)
  - [x] `ON DELETE RESTRICT` so the existing "reject delete if tasks reference it" guard gets DB-level enforcement in addition to the service-level check
  - [x] Workflows are referenced via `projects.workflow_id` FK with `ON DELETE RESTRICT`

### Initiative: Service Layer Migration

> `ProjectService` and `WorkflowService` read and write the database instead of the config file. `inmem/` implementations are deleted — the in-memory path only existed because the source of truth was a TOML file held in memory after `config.Load()`.

- [x] **Story: ProjectService over repository**
  - [x] `ProjectService.Create`/`Modify`/`Delete`/`List`/`Get` call `ProjectRepository` directly
  - [x] Optimistic locking: callers fetch to get `version`, mutations pass it through, `ErrConflict` bubbles up like task mutations
  - [x] Drop `config.CreateProject`/`ModifyProject`/`DeleteProject` — their TOML-writing logic is removed and callers switch to the service
  - [x] Service-level delete guard (reject if tasks reference the project, reject deleting `_default`, `--force` bypass) stays in the service and runs before the DB delete

- [x] **Story: WorkflowService over repository**
  - [x] `WorkflowService.Create`/`Modify`/`Delete`/`List`/`Get` call `WorkflowRepository` directly
  - [x] Role-schema validation (exactly one `initial`, one `start`, ≥1 `terminal`, etc.) moves from config validation into the service
  - [x] Delete guard rejects workflows referenced by any project — implemented via a repository-level `CountProjectsByWorkflow` call, not a full project list scan
  - [x] Drop `config.CreateWorkflow`/`ModifyWorkflow`/`DeleteWorkflow`

- [x] **Story: Retire `inmem/` project and workflow stores**
  - [x] Delete `inmem/project.go` and `inmem/workflow.go`
  - [x] DI wiring in `cmd/tusk/` constructs SQLite repositories from the workspace store
  - [x] Tests that used `inmem` for project/workflow setup switch to the SQLite store via the existing test harness

### Initiative: Config Schema Trim

> Remove `[projects.*]` and `[workflows.*]` from the config file. Config keeps global settings only — `storage.*`, global `urgency.*`, global `auto_complete.*`, `mcp.*`, `filter.*`, etc. `config show` still renders projects and workflows, now sourced from the DB.

- [x] **Story: Remove project/workflow sections from the config schema**
  - [x] Delete `ProjectConfig` and `WorkflowConfig` from `config/` types
  - [x] Remove `[projects.<name>]` and `[workflows.<name>]` from `config/default.toml`
  - [x] Config loader emits a hard error if the resolved file still contains these sections, pointing at the migration command (see next initiative)
  - [x] Global `[urgency]` and `[auto_complete]` stay in config as defaults — project overrides live in the DB `projects.settings` JSON

- [x] **Story: `config show` reads projects and workflows from DB**
  - [x] `config show` output keeps rendering `[projects.*]` and `[workflows.*]` sections for continuity, hydrated from the DB at display time
  - [x] Sections are marked read-only in the rendered header (e.g. `# projects (from database, use 'tusk project' to modify)`)
  - [x] `config get projects.<name>.<field>` / `config get workflows.<name>.<field>` resolve against the DB
  - [x] `config set` rejects keys under `projects.*` and `workflows.*` with an error pointing at `tusk project modify` / `tusk workflow modify`

### Initiative: CLI & MCP Rewiring

> `tusk project` and `tusk workflow` subcommands (and their MCP counterparts) mutate the database through the services instead of the config file. External surface is nearly unchanged — same flags, same inline syntax — only the storage backend moves.

- [x] **Story: Project and workflow CLI over services**
  - [x] `tusk project create`/`modify`/`delete`/`list` call `ProjectService` directly
  - [x] `tusk workflow create`/`modify`/`delete`/`list` call `WorkflowService` directly
  - [x] Inline syntax (`workflow=kanban`, `urgency.blocking-weight=15`, `+urgency.blocking-weight=2`, etc.) is unchanged — the parser produces the same AST, only the write target moves
  - [x] Numeric delta resolution for urgency weights still reads the effective global weight from config and stores the resolved override in `projects.settings`

- [x] **Story: MCP project and workflow tools over services**
  - [x] `tusk_project_create`/`modify`/`delete` and `tusk_workflow_create`/`modify`/`delete` call the services
  - [x] Tools accept and return `version` for optimistic locking, matching `tusk_task_*` conventions
  - [x] The config mutex that previously serialized project/workflow writes (`eec8ec6`) is removed — DB-level optimistic locking replaces it

---

## v0.11 — CLI Command Grouping

**Goal:** Regroup the CLI under explicit subcommand namespaces so the surface scales cleanly as the system grows. Early-stage Tusk shipped flat commands (`tusk add`, `tusk start`, `tusk done`); with projects, workflows, players, tags, config, notes, and dashboard all competing for top-level slots, the flat layout is noisy and ambiguous. This milestone moves every task-scoped verb under `tusk task` and leaves only workspace-wide operations at the top level. Pre-release, so no backward-compat aliases — clean break.

Alongside the regrouping, v0.11 locks in a principle the CLI has been drifting toward since v0.9: **entity properties flow through the inline `key=value` lexer, not ad-hoc Cobra flags.** `priority=3`, `project=backend`, `due=today`, `+tag`, `parent=a3f8b2c1`, `uda.env=prod` — every property that describes *what the task is* goes through the shared syntax pipeline, so the lexer/AST owns every entity-shaped input and there is exactly one way to set a field on a task. Cobra flags stay reserved for invocation-level concerns that aren't entity properties: actor identity (`--player`), view toggles (`--all`, `--output`), config scoping (`--config`, `--db`, `--global`). This avoids Cobra-custom flag surfaces overlapping the lexer, keeps the CLI and MCP field sets aligned (MCP already accepts entity properties as structured JSON, never as flags), and means every new field added to a task is one entry in the field registry instead of a new flag on every consumer command. The two remaining flag-based task entity properties — `--description` and `--uda` — are eliminated in their own initiatives below.

### Initiative: `tusk task` Subcommand Group

> Move every task-scoped command under a single `tusk task` parent. Verbs, flags, inline syntax, and output are unchanged — only the invocation path moves. Pre-release, so no backward-compat aliases — removed commands stay removed.

- [x] **Story: Scope — which commands move and which stay flat**
  - [x] Moves under `tusk task`: every task-scoped verb (CRUD, lifecycle, claim/queue, relations)
  - [x] Stays flat — workspace-wide operations that don't belong to any single entity: `tusk undo` (reverts the last mutation regardless of entity type), `tusk export` (workspace-wide data dump), `tusk dashboard` (workspace-wide view), `tusk mcp serve` (server invocation, not an entity operation)
  - [x] Already grouped, no change: `tusk config`, `tusk project`, `tusk workflow`, `tusk player`, `tusk tag`, `tusk note`
  - [x] This story is a decision/scoping gate — the mapping table it locks in drives every downstream story in this milestone

- [x] **Story: `tusk task` parent command skeleton**
  - [x] Register the `tusk task` parent Cobra command with its long help listing all subcommands with one-line summaries
  - [x] Wire it into the root command so `tusk task` (no subcommand) prints usage and exits cleanly
  - [x] Establishes the parent so each subsequent move story is a drop-in `AddCommand` call rather than a restructure

- [x] **Story: Task CRUD and lifecycle under `tusk task`**
  - [x] `tusk add` → `tusk task create`
  - [x] `tusk list` → `tusk task list`
  - [x] `tusk info` → `tusk task get`
  - [x] `tusk modify` → `tusk task modify`
  - [x] `tusk start` → `tusk task start`
  - [x] `tusk done` → `tusk task done`
  - [x] `tusk delete` → `tusk task delete`
  - [x] `tusk tree` → `tusk task tree`
  - [x] `tusk next` → `tusk task next`
  - [x] `tusk annotate` → `tusk task annotate`

- [x] **Story: Claim and queue under `tusk task`**
  - [x] `tusk available` → `tusk task available`
  - [x] `tusk pop` → `tusk task pop`
  - [x] `tusk claim` → `tusk task claim`
  - [x] `tusk release` → `tusk task release`

- [x] **Story: Relations under `tusk task`**
  - [x] `tusk link` → `tusk task link`
  - [x] `tusk unlink` → `tusk task unlink`
  - [x] MCP tools rename to match: `tusk_relation_add` → `tusk_task_link`, `tusk_relation_remove` → `tusk_task_unlink`. MCP and CLI surfaces stay in lockstep so agents and humans share the same mental model.

- [x] **Story: Removal and suggestions for moved commands**
  - [x] Old flat commands are deleted from the root — Cobra emits its standard "unknown command" error for each
  - [x] A custom `SuggestFor` / unknown-command handler prints a targeted hint for moved verbs so `tusk add foo` prints "unknown command 'add'; did you mean 'tusk task create'?" for every entry in the scope story's mapping table
  - [x] Runs last in this initiative so the hint table reflects the final set of moved commands

### Initiative: `tusk completion` Subcommand

> Add a `tusk completion` subcommand that emits shell completion scripts for bash, zsh, fish, and PowerShell. Without it, surface reorganizations like the `tusk task` grouping leave users with stale completions and no in-repo way to refresh them — the roadmap item "regenerate completions after the new command tree" has no mechanism to tick against. Cobra already ships a built-in completion generator; this initiative wires it into the root command tree and documents the install flow, so every future CLI surface change (v0.11 string-field unification, v0.11 UDA flag elimination, v0.12 notes window, and onward) can point users at a single refresh command instead of bespoke per-release completion scripts.

- [x] **Story: Wire Cobra's completion generator into the root command**
  - [x] Call `rootCmd.AddCommand(...)` with the `cobra.Command` returned by Cobra's built-in completion generator, using the standard four shells (`bash`, `zsh`, `fish`, `powershell`)
  - [x] `tusk completion bash`, `tusk completion zsh`, `tusk completion fish`, `tusk completion powershell` each emit a completion script to stdout for the current command tree
  - [x] The subcommand is visible in `tusk --help` alongside `tusk version`, `tusk mcp`, and the grouped entity commands
  - [x] No persistent flag parsing side effects — the completion command runs without touching the workspace database or config resolution

- [x] **Story: Document the install flow**
  - [x] Add a "Shell completion" section to `PRODUCT.md` under the CLI interface that shows the generate-and-install commands for each supported shell
  - [x] Add a matching section to `docs/configuration.md` (or a new `docs/shell-completion.md` if it grows past a few paragraphs) with per-shell install paths (`~/.bash_completion.d/`, `~/.zsh/completions/`, `~/.config/fish/completions/`, PowerShell profile)
  - [x] Call out in the section that completion scripts are generated on demand — there are no pre-baked completion artifacts in the repo or release tarballs, so users regenerate after every tusk upgrade

- [x] **Story: Completion tests**
  - [x] Add an e2e test that invokes `tusk completion bash`, `tusk completion zsh`, `tusk completion fish`, and `tusk completion powershell` and asserts non-empty stdout and exit code 0 — smoke-level, not script parsing
  - [x] Add a regression check that `tusk completion bash` output mentions every top-level subcommand currently registered on the root (`task`, `project`, `workflow`, `tag`, `player`, `config`, `mcp`, `completion`, `version`, and the workspace-wide verbs) — if a future refactor drops a command from the root, the test fails loudly

### Initiative: String Field Input Unification

> Executes the milestone-wide inline-field principle for free-form string fields. The `description` field lives outside the lexer today — it's a Cobra flag (`--description`/`-d`) with bespoke `@file` / `@-` expansion, while `title` and every other field already flow through inline `key=value` syntax. This initiative moves `description` onto the inline surface alongside `title`, annotation bodies, and any future string field, and it adds an inline `@` reference expander that substitutes file content (or stdin) directly into the decoded string value at the consumer layer. Runs after the `tusk task` grouping initiative so it acts on the already-renamed commands once, not twice, and before the UDA flag elimination initiative so both flag-removal stories share the same rewired command surface.

> **Scope note:** The original story set included `syntax.ValueModifier` AST changes and a value-prefix modifier registry. These were dropped in design — `@` is inline text substitution, not a prefix marker, so the mid-string case (`"text @file.txt"`) cannot be represented as a stripped AST marker. The shipped implementation is a pure consumer-layer text pass with no lexer or AST changes. See `docs/plans/v0.11-string-field-input-unification/design.md` for the full reasoning.

- [x] **Story: Word-boundary `@` reference expansion**
  - [x] Add a CLI-layer expander `internal/tui.expandRefs(raw, stdin, maxSize)` that scans a string for word-boundary `@` references and substitutes file content (or stdin for `@-`) inline
  - [x] Word boundary means start-of-string or preceded by ASCII whitespace — `foo@bar.com` and `user@host` are never expanded
  - [x] Bare path scans until next whitespace; quoted path `@"./name with space.txt"` scans a quoted span for paths containing spaces
  - [x] `@@` at a word boundary escapes to a literal `@`
  - [x] `@-` reads stdin; stdin may only be referenced once per invocation (enforced across multiple `expandRefsWithState` calls in one command via a shared state struct)
  - [x] Substituted content is **not** re-scanned for nested references — expansion is one level deep
  - [x] No AST or lexer changes — the expander runs on the final decoded string value from the v0.9 lexer, after quotes have already collapsed. Quoted lexer values are **not** opaque to `@` expansion; lexer quoting escapes shell/lexer syntax, `@@` escapes the expander.

- [x] **Story: Expander file-read and stdin semantics**
  - [x] File paths resolve via `os.ReadFile` against the caller's CWD; `~/` prefix expands via `os.UserHomeDir`; absolute paths pass through
  - [x] Missing file → `@<path>: no such file` error
  - [x] Binary detection via NUL-byte scan on the first 8 KB of content (git's approach); binary files rejected with an error pointing at future attachment support
  - [x] Per-reference size cap configured via `inline.max_expansion_size` (default 1 MB); over-cap files rejected with actual size and limit in the error message
  - [x] Stdin TTY guard preserved from the old `readDescription` helper
  - [x] Replaces `internal/tui/description.go` entirely — the old helper and its tests are deleted

- [x] **Story: Drop `--description` flag, use inline field**
  - [x] Remove `--description` / `-d` from `tusk task create` and `tusk task modify`
  - [x] Commands read the `description=` field value from `FilterSet.GetField` and pass it through the expander
  - [x] `description=` with an empty value clears the field on modify (matches the old `--description ""` behavior, feeds into the double-pointer `**string` update path)
  - [x] Same pattern applied to `title=` so `title=@./title.txt` works on create and modify

- [x] **Story: Positional bodies gain `@` expansion**
  - [x] `tusk task annotate <id> "body"` stays positional — annotation commands are single-value and the positional form is idiomatic
  - [x] The positional body runs through the expander, so `tusk task annotate a3f8b2c1 @./notes.md` and `tusk task annotate a3f8b2c1 @-` work with the same semantics as `description=@...`
  - [x] Literal `@` at the start of a positional body is escaped at the expander level with `@@` (or quoted at the shell level as a fallback); shell quoting remains the user's responsibility
  - [x] `tusk note add "body"` (v0.12) inherits the same convention from day one — documented in the v0.12 note CLI story rather than patched in later

- [x] **Story: MCP field parity check**
  - [x] MCP tools receive description, title, and body as structured JSON fields, so no `@file` expansion is needed on that surface — agents already pass the content directly
  - [x] Tool schemas stay unchanged; only the CLI surface moves
  - [x] Verification pass confirmed no MCP tool accidentally grew a `@` interpretation while the CLI was being rewired: `grep -rn "expandRefs\|expandRefsWithState\|expandState" internal/mcp/` → no hits; `grep -rn "readDescription" internal/mcp/` → no hits; `grep -rn "@\"" internal/mcp/` → no hits; `grep -rn "@-" internal/mcp/` → no hits. `tusk_task_create`, `tusk_task_modify`, and `tusk_task_annotate` schemas list `title`, `description`, and `body` as plain `string` parameters, and the handlers in `internal/mcp/tools.go` call `TaskService.Create`/`Update`/`Annotate` directly with the raw JSON-supplied strings.

### Initiative: UDA Flag Elimination

> User-defined attributes are currently set via `--uda key=value` (repeatable) on `tusk task create` and `tusk task modify`, while every other entity property on those same commands is inline. This initiative drops `--uda` in favor of dotted inline fields (`uda.key=value`) so UDAs obey the milestone-wide principle and match the filter syntax already documented in `PRODUCT.md` (`uda.env=prod` works identically in filters, create, and modify). No lexer change is required — dotted keys already flow through the v0.9 key tokenizer — so this initiative is pure consumer rewiring on top of the String Field Input Unification work.

- [x] **Story: Dotted UDA field recognition in task commands**
  - [x] `runAdd` and `runModify` iterate the parsed field list and treat every field whose key has a `uda.` prefix as a UDA entry, with the tail after the prefix as the UDA key
  - [x] `uda.key=value` sets the attribute; `uda.key=` (empty value) clears it on modify, matching the double-pointer `**string` update path already used for nullable fields
  - [x] Repetition works naturally — the parser already allows multiple fields, so `uda.env=prod uda.region=eu` sets two attributes in one invocation without any array-of-flags plumbing
  - [x] Dotted keys coexist with the reserved top-level keys (`title`, `priority`, `project`, `parent`, `due`, `status`, `description`, `tree`) — a `uda.` prefix is the only disambiguator, and a bare `env=prod` is still rejected as an unknown top-level field so typos surface loudly instead of silently becoming UDAs

- [x] **Story: Drop `--uda` / `-u` flag**
  - [x] Remove `--uda` / `-u` from `tusk task create` and `tusk task modify` in `internal/tui/commands.go`
  - [x] Delete the `parseUDAFlags` helper and its tests once every caller has moved to the inline path
  - [x] Cobra emits its standard "unknown flag" error for stale `--uda` invocations — no targeted suggestion shim, since the inline syntax is documented in both help text and the dotted-field error path
  - [x] Runs second so the inline recognizer is proven before the old flag disappears

- [x] **Story: MCP parity check for UDAs**
  - [x] MCP task tools already accept UDAs as a structured `uda` object in the tool schema — no dotted-key translation needed on that surface
  - [x] Tool schemas stay unchanged; verification pass confirms no MCP handler accidentally grew a `uda.`-prefix parser while the CLI was being rewired
  - [x] Runs last as a symmetric verification to the String Field Input Unification initiative's MCP check

### Initiative: Documentation and Test Rewrite

> Every doc example, help string, and E2E scenario references the old flat commands. All need to move in lockstep with the CLI change, or the release ships with broken examples. Runs last in the milestone — the command surface and field conventions must be final before the surrounding material is rewritten.

- [x] **Story: Help text and command descriptions**
  - [x] Every moved subcommand's long help is reviewed and updated to remove self-references to the old flat path and to document the new inline field conventions (`description=`, `title=`, `@file`, `@-`)
  - [x] The `tusk task` parent command skeleton from the grouping initiative gets its full listing finalized here once every child command is in place
  - [x] Runs first in this initiative because help text is the source of truth that the documentation sweep quotes from

- [x] **Story: Documentation sweep**
  - [x] `README.md`, `PRODUCT.md`, `docs/configuration.md`, `docs/programmatic-usage.md`, and every file under `docs/releases/` and `docs/status/` updated to the new command syntax
  - [x] Historical release notes (v0.1 through v0.10) are left untouched — they describe what shipped at the time and should not be rewritten
  - [x] v0.11 release notes call out the full mapping table as a breaking change, the inline-field principle, the `--description` and `--uda` flag removals, the `@file` / `@-` convention on inline string fields, and the dotted `uda.key=value` convention on create/modify
  - [x] PRODUCT.md's "Inline Syntax" and CLI sections explicitly state the principle so agents and humans reading the product description see the one-way-to-set-a-field rule alongside the lexer description

- [x] **Story: E2E test rewrite**
  - [x] Every scenario in `tests/e2e/` updated to the new invocation paths and inline field conventions
  - [x] Harness step builders (if any hardcode command names) updated
  - [x] New scenarios covering the "unknown command" suggestion path for each removed flat verb, to lock in the hint table
  - [x] New scenarios covering `description=@file`, `description=@-`, and `title=@file` to lock in the file-loading helper behavior
  - [x] New scenarios covering `uda.key=value` on create, repeated `uda.*` fields in a single invocation, `uda.key=` clearing on modify, and the unknown-top-level-field rejection path for bare `key=value` that isn't a registered field
  - [x] Runs last in the milestone — a green test suite on the new surface is the exit gate for v0.11

---

## v0.12 — Trailing Window Notes

**Goal:** A persistent notebook system where players record learnings, context, and decisions — scoped by project and player, with a configurable trailing window that shows only the most recent entries to avoid context overload.

### Initiative: Note Entity & Storage

> Domain type, repository interface, and SQLite implementation for notes.

- [ ] **Story: Note domain and storage**
  - [ ] Define `Note` entity (`id` UUID, `project_id`, `player_id`, `task_id` nullable, `body`, `metadata` JSON, `archived_at` nullable, `created_at`)
  - [ ] `NoteRepository` interface (`Create`, `Archive`, `GetByID`, `List`)
  - [ ] Migration adding `notes` table with composite index on `(project_id, player_id, created_at DESC)` and partial index on `task_id`
  - [ ] SQLite `NoteRepository` implementation with window-aware `List` query (`LIMIT` in SQL, not post-fetch)

### Initiative: Note Service

> Business logic for note creation, listing with trailing window, and archiving.

- [ ] **Story: NoteService**
  - [ ] `Create` — validate player exists, project exists, optional task exists and belongs to project, body non-empty
  - [ ] `List` — resolve effective window size (CLI flag → player DB setting → project config → global config → default 20), apply `--since` filter, default to caller's notes only
  - [ ] `Archive` — set `archived_at`, validate caller is author

- [ ] **Story: Window size resolution**
  - [ ] Add `note_window_size` nullable column to `players` table (migration)
  - [ ] Add `[notes].window_size` to global config schema
  - [ ] Add `[projects.<name>.notes].window_size` to project config schema
  - [ ] Resolution chain: CLI flag → player DB → project config → global config → hardcoded default (20)

### Initiative: Note CLI

> `tusk note` subcommand for writing, reading, and archiving notes.

- [ ] **Story: Note write commands**
  - [ ] `tusk note add "<body>" [project=<name>] [--task <short_id>] [key=value...]` — create a note with optional task scope and metadata
  - [ ] `tusk note archive <note_id>` — archive a note

- [ ] **Story: Note read commands**
  - [ ] `tusk note list` — list own notes in current/default project, trailing window applied
  - [ ] `tusk note list project=<name>` — specific project
  - [ ] `tusk note list --all-players` — all players' notes
  - [ ] `tusk note list --player <id>` — specific player's notes
  - [ ] `tusk note list --task <short_id>` — task-scoped notes
  - [ ] `tusk note list --window <N>` — override window size
  - [ ] `tusk note list --since <duration>` — time-bounded filter (e.g., `7d`, `24h`)
  - [ ] `tusk note list --archived` — include archived notes
  - [ ] Markdown rendering via glamour in CLI output

- [ ] **Story: Player window size preference**
  - [ ] `tusk player modify <id> note-window-size=<N>` — set per-player window size
  - [ ] Display `note_window_size` in player info output

### Initiative: Note MCP Tools

> Expose note operations to AI agents via MCP.

- [ ] **Story: Note MCP tools**
  - [ ] `tusk_note_add` — create note (project, player, optional task, body, metadata)
  - [ ] `tusk_note_list` — list with window/since/player/task/archived filters
  - [ ] `tusk_note_archive` — archive a note

### Initiative: MCP Field Restrictions

> Configurable field-level write restrictions for MCP tools — prevent agents from modifying sensitive player or system settings.

- [ ] **Story: MCP blocked fields**
  - [ ] Define `[mcp.blocked_fields]` config section mapping tool names to lists of restricted fields
  - [ ] Enforce restrictions at the MCP layer before service calls
  - [ ] Default blocked fields for player modification (e.g., `note_window_size`)

---

## v0.13 — Live Dashboard

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

## v0.14 — Advanced Features

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

> Revert the last mutation using the event log from v0.13.

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
  - [ ] Fire webhooks on task state changes (powered by event log from v0.13)

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
