# Tusk Roadmap
> Deliver a concurrent-safe, single-binary task management tool that combines CLI speed with structured hierarchy and workflow flexibility, accessible to both humans (TUI) and AI agents (MCP server).
>

## v0.1 — Foundation level=milestone order=1
> Core task management with CLI, SQLite persistence, and basic workflow.

### Initiative: Core Domain & Storage level=initiative order=1
> Establish domain types, repository interfaces, and SQLite backend.

- [x] Story: Domain model level=story order=1
  - [x] Define core types (Task, Relation, Project, Workflow, Tag, Annotation) level=task order=1
  - [x] Define repository interfaces (TaskRepository, RelationRepository, ProjectRepository, TagRepository, WorkflowRepository, AnnotationRepository) level=task order=2
  - [x] Define sentinel errors (ErrNotFound, ErrConflict, ErrCyclicBlock, ErrInvalidTransition, ErrDuplicateRelation) level=task order=3
- [x] Story: SQLite storage level=story order=2
  - [x] Implement SQLite store with WAL mode, busy_timeout, foreign keys level=task order=1
  - [x] Write initial migration (001_initial.up.sql / down.sql) level=task order=2
  - [x] Implement TaskRepository for SQLite level=task order=3
  - [x] Implement TagRepository for SQLite level=task order=4
  - [x] Implement AnnotationRepository for SQLite level=task order=5
  - [x] Implement WorkflowRepository for SQLite level=task order=6
  - [x] Implement RelationRepository for SQLite level=task order=7
### Initiative: Task Service & Workflow level=initiative order=2
> Business logic for task CRUD with workflow validation and optimistic locking.

- [x] Story: TaskService with CRUD level=story order=1
  - [x] Create tasks with UUID + 8-char short ID generation level=task order=1
  - [x] Read tasks by ID and short ID level=task order=2
  - [x] Update tasks with optimistic locking (version field) level=task order=3
  - [x] Soft-delete tasks via workflow transition level=task order=4
- [x] Story: Workflow validation level=story order=2
  - [x] Enforce status transitions per project workflow level=task order=1
  - [x] Seed default workflow (pending, active, completed, deleted) level=task order=2
  - [x] Reject invalid transitions with ErrInvalidTransition level=task order=3
### Initiative: CLI Interface level=initiative order=3
> Basic TUI commands for task management.

- [x] Story: Core CLI commands level=story order=1
  - [x] `tusk add` — create tasks with inline project, tags, priority level=task order=1
  - [x] `tusk list` — list tasks sorted by urgency level=task order=2
  - [x] `tusk info` — full detail view of a single task level=task order=3
  - [x] `tusk modify` — update task fields level=task order=4
  - [x] `tusk done` — shorthand for active → completed level=task order=5
  - [x] `tusk delete` — shorthand for → deleted level=task order=6
  - [x] `tusk annotate` — add annotation to a task level=task order=7
- [x] Story: Tag support level=story order=2
  - [x] TagService implementation level=task order=1
  - [x] Wire tags into `add` / `modify` / `list` commands level=task order=2
  - [x] `+tag` / `-tag` syntax in CLI level=task order=3
- [x] Story: Filter syntax level=story order=3
  - [x] Lexer for filter tokens level=task order=1
  - [x] Parser for filter expressions level=task order=2
  - [x] Resolver to map filters to repository queries level=task order=3
  - [x] Support: `status:`, `priority:`, `project:`, `+tag`, `-tag`, `due:`, ranges (`..`) level=task order=4
### Initiative: Testing level=initiative order=4
> Automated test coverage for CLI workflows.

- [x] Story: E2E test harness level=story order=1
  - [x] Build harness with multi-mode execution (DB config modes x output formats) level=task order=1
  - [x] Step reference system (`$0.short_id`) level=task order=2
  - [x] Cover core CLI commands end-to-end level=task order=3
## v0.2 — Relations & Hierarchy level=milestone order=2
> Typed relations between tasks, parent-child hierarchy, and tag management.

### Initiative: Relations level=initiative order=1
> First-class typed edges between tasks with cycle detection.

- [x] Story: RelationService level=story order=1
  - [x] Create relations (blocks, relates_to, duplicates) level=task order=1
  - [x] Cycle detection via DFS on `blocks` edges level=task order=2
  - [x] Prevent duplicate relations (ErrDuplicateRelation) level=task order=3
  - [x] Derive inverse relations at query time level=task order=4
- [x] Story: Relation CLI commands level=story order=2
  - [x] `tusk link <source> <type> <target>` — create relation level=task order=1
  - [x] `tusk unlink <source> <type> <target>` — remove relation level=task order=2
  - [x] Display relations in `tusk info` output level=task order=3
### Initiative: Parent-Child Hierarchy level=initiative order=2
> Optional task nesting to arbitrary depth.

- [x] Story: Parent-child task creation level=story order=1
  - [x] `parent:<short_id>` on `tusk add` to create subtasks level=task order=1
  - [x] `parent:<short_id>` filter for listing direct children level=task order=2
  - [x] `tree:<short_id>` filter for listing all descendants level=task order=3
- [x] Story: Tree CLI command level=story order=2
  - [x] `tusk tree` — hierarchical indented view of all tasks level=task order=1
  - [x] E2E tests for tree display level=task order=2
- [x] Story: Completion propagation level=story order=3
  - [x] Add JSON `settings` column to projects table (migration) — _settings moved to config in v0.4_ level=task order=1
  - [x] `ProjectSettings` with `AutoCompleteConfig` and `AutoRevertConfig` (configurable trigger/target statuses) level=task order=2
  - [x] `TaskTxProvider` for atomic propagation within same transaction level=task order=3
  - [x] Auto-transition parent when all non-deleted children reach trigger status level=task order=4
  - [x] Auto-revert parent when a child moves away from trigger status level=task order=5
  - [x] Recursive propagation up ancestor chain level=task order=6
  - [x] Workflow validation respected — propagation silently stops if transition invalid level=task order=7
  - [x] Disabled by default, opt-in per project via settings level=task order=8
  - [x] E2E tests for propagation scenarios level=task order=9
### Initiative: Project Management level=initiative order=3
> CLI commands for project listing. Projects are config-driven (see v0.4 Config-based Projects); `create` and `modify` were removed in v0.4.

- [x] Story: `tusk project` subcommand level=story order=1
  - [x] `tusk project list` — list all projects level=task order=1
### Initiative: Tag Management level=initiative order=4
> Dedicated tag subcommand for CRUD operations.

- [x] Story: `tusk tag` subcommand level=story order=1
  - [x] `tusk tag create <name>` — create a tag level=task order=1
  - [x] `tusk tag list` — list all tags level=task order=2
  - [x] `tusk tag delete <name>` — delete a tag level=task order=3
  - [x] `tusk tag rename <old> <new>` — rename a tag level=task order=4
## v0.3 — MCP Server level=milestone order=3
> Expose all capabilities via MCP protocol for AI agent integration.

### Initiative: MCP Server Core level=initiative order=1
> stdio-transport MCP server mapping tools to service methods.

- [x] Story: MCP server with stdio transport level=story order=1
  - [x] Server setup and lifecycle management level=task order=1
  - [x] stdio transport implementation level=task order=2
  - [x] Tool registration framework level=task order=3
- [x] Story: Task tools level=story order=2
  - [x] `tusk_task_create` — TaskService.Create level=task order=1
  - [x] `tusk_task_list` — TaskService.List level=task order=2
  - [x] `tusk_task_get` — TaskService.GetByShortID level=task order=3
  - [x] `tusk_task_modify` — TaskService.Update level=task order=4
  - [x] `tusk_task_start` — TaskService.Start level=task order=5
  - [x] `tusk_task_done` — TaskService.Complete level=task order=6
  - [x] `tusk_task_delete` — TaskService.Delete level=task order=7
  - [x] `tusk_task_annotate` — TaskService.Annotate level=task order=8
  - [x] `tusk_task_tree` — TaskService.Tree level=task order=9
- [x] Story: Relation & project tools level=story order=3
  - [x] `tusk_relation_add` — RelationService.Add level=task order=1
  - [x] `tusk_relation_remove` — RelationService.Remove level=task order=2
  - [x] `tusk_project_list` — ProjectService.List level=task order=3
### Initiative: MCP Resources level=initiative order=2
> Expose tasks, projects, and workflows as readable resources.

- [x] Story: MCP resource definitions level=story order=1
  - [x] `tusk://tasks/{short_id}` resource level=task order=1
  - [x] `tusk://projects/{id}` resource level=task order=2
  - [x] `tusk://projects/{id}/workflow` resource level=task order=3
### Initiative: MCP Concurrency level=initiative order=3
> End-to-end optimistic locking through MCP tool I/O.

- [x] Story: Version passing level=story order=1
  - [x] Include `version` in all task tool responses level=task order=1
  - [x] Accept `version` in modify/start/done/delete tool inputs level=task order=2
  - [x] Return ErrConflict on version mismatch level=task order=3
## v0.4 — Configuration & Customization level=milestone order=4
> Viper-based configuration system, config-driven projects and workflows, enabling custom statuses, transitions, and per-project workflow assignment — all without runtime DB tables for workflows.

### Initiative: Configuration System level=initiative order=1
> Viper-based config loading as foundation for all runtime settings.

- [x] Story: Viper config loader level=story order=1
  - [x] Add Viper dependency level=task order=1
  - [x] Load config from `~/.config/tusk/config.toml` with fallback to hardcoded defaults level=task order=2
  - [x] Support `TUSK_` environment variable prefix for all config keys level=task order=3
  - [x] Wire config into `cmd/tusk/main.go` DI setup level=task order=4
  - [x] Define `Config` struct covering `[urgency]`, `[tui]`, `[storage]`, `[workflows]`, and `[mcp]` sections level=task order=5
- [x] Story: MCP visibility config schema level=story order=2
  - [x] `[mcp.disabled_tool_groups]` — hide tools by group (e.g., `["workflow", "relation"]`) level=task order=1
  - [x] `[mcp.disabled_tools]` — hide individual tools (e.g., `["tusk_workflow_list"]`) level=task order=2
  - [x] `[mcp.disabled_resource_groups]` — hide resources by group level=task order=3
  - [x] `[mcp.disabled_resources]` — hide individual resource templates level=task order=4
### Initiative: Config-based Projects level=initiative order=2
> Projects become purely config-driven in-memory entities, same as workflows. Drop the `projects` table entirely. Project IDs become human-readable strings (e.g., `"default"`, `"backend"`). Tasks store `project_id` as a plain string column validated at the service layer against config — no FK constraint. A builtin `default` project exists when no config is present.

- [x] Story: Drop projects table and migrate task references level=story order=1
  - [x] Migration to drop `projects` table and remove FK constraint from `tasks.project_id` level=task order=1
  - [x] Migrate existing `tasks.project_id` UUID values to human-readable project IDs level=task order=2
  - [x] Update `tasks.project_id` column to plain TEXT (no FK) level=task order=3
  - [x] Drop `workflows.project_id` FK reference (handled by Declarative Workflows initiative) level=task order=4
- [x] Story: Project config schema level=story order=2
  - [x] Define `[projects.<id>]` TOML section with `workflow` and `settings` keys level=task order=1
  - [x] Add `ProjectsConfig` to `Config` struct level=task order=2
  - [x] Builtin `default` project with `kanban` workflow when no config is present level=task order=3
  - [x] Validate project config on load (referenced workflow must exist in config) level=task order=4
- [x] Story: In-memory project repository and service level=story order=3
  - [x] Rewrite `domain.Project` as config struct — `ID` (string), `Workflow` (string), `Settings` (ProjectSettings) level=task order=1
  - [x] Simplify `ProjectRepository` interface to read-only (`GetByID`, `List`) level=task order=2
  - [x] Implement in-memory `ProjectRepository` backed by config level=task order=3
  - [x] Remove SQLite `ProjectRepository` implementation level=task order=4
  - [x] Update `ProjectService` and `TaskService` for new interface level=task order=5
  - [x] Update CLI commands (`tusk project list`, remove `tusk project create`/`modify`) level=task order=6
  - [x] Update MCP tools (remove `tusk_project_create`, make project tools read-only) level=task order=7
### Initiative: Declarative Workflows level=initiative order=3
> Workflows become purely config-driven in-memory entities. Drop workflow DB tables entirely. A builtin `kanban` workflow provides the default. Projects reference a workflow by name, resolved from config at runtime.

- [x] Story: Workflow config schema level=story order=1
  - [x] Define `[workflows.<name>]` TOML schema with `statuses` and `transitions` keys level=task order=1
  - [x] Add `WorkflowsConfig` map to `Config` struct level=task order=2
  - [x] Builtin `kanban` workflow as default (pending, active, completed, deleted) when no config is present level=task order=3
  - [x] Validate workflow config on load (statuses referenced in transitions must exist, no orphan transitions) level=task order=4
- [x] Story: Drop workflow DB tables level=story order=2
  - [x] Migration to drop `workflow_transitions` and `workflows` tables level=task order=1
  - [x] Remove SQLite `WorkflowRepository` implementation level=task order=2
  - [x] Remove workflow seed data from migrations level=task order=3
- [x] Story: In-memory workflow repository and service level=story order=3
  - [x] Simplify `WorkflowRepository` interface (`GetByName`, `GetTransitions`, `List`) level=task order=1
  - [x] Implement in-memory `WorkflowRepository` backed by config level=task order=2
  - [x] Update `WorkflowService` for new interface level=task order=3
  - [x] Wire into DI in `cmd/tusk/main.go` level=task order=4
  - [x] Update `TaskService` if interface changes level=task order=5
- [x] Story: Workflow CLI commands level=story order=4
  - [x] `tusk workflow list` — list all workflows from config with their statuses and transitions level=task order=1
  - [x] `tusk workflow info <name>` — detailed view of a single workflow level=task order=2
- [x] Story: MCP workflow tools level=story order=5
  - [x] `tusk_workflow_list` — list all workflows from config level=task order=1
  - [x] Expose workflow name in project resource responses level=task order=2
### Initiative: MCP Configurability level=initiative order=4
> Config-driven control over which MCP tools and resources are exposed to agents.

- [x] Story: MCP visibility wiring level=story order=1
  - [x] Expose tool/resource groups as a convention in MCP registration (tag or prefix-based) level=task order=1
  - [x] Filter tool and resource registration at server startup based on config level=task order=2
  - [x] Validate config values against known tools/resources on startup (error on unknown entries) level=task order=3
## v0.5 — Rich Content level=milestone order=5
> Enable rich task descriptions and structured metadata for agent orchestration.

### Initiative: Rich Descriptions level=initiative order=1
> Full markdown descriptions with file-based input for detailed task specs.

- [x] Story: File-based description input level=story order=1
  - [x] `--description @file.md` syntax on `tusk add` and `tusk modify` to read content from a file level=task order=1
  - [x] `tusk_task_create` and `tusk_task_modify` MCP tools accept full markdown descriptions level=task order=2
  - [x] `tusk info` renders description in full (no truncation) level=task order=3
### Initiative: User-Defined Attributes level=initiative order=2
> Expose the existing `uda` JSON column via CLI and MCP. Note: `Task.UDA` field and `tasks.uda` JSON column already exist in the domain and schema.

- [x] Story: UDA CLI surface level=story order=1
  - [x] `--uda key=value` on `tusk add` and `tusk modify` level=task order=1
  - [x] Display UDAs in `tusk info` level=task order=2
  - [x] `tusk_task_create` and `tusk_task_modify` MCP tools accept UDA fields level=task order=3
- [x] Story: UDA filter support level=story order=2
  - [x] `uda.key:value` filter syntax level=task order=1
  - [x] Expose in both CLI and MCP task list level=task order=2
## v0.6 — Urgency & UX level=milestone order=6
> Smart task prioritization and polished terminal experience.

### Initiative: Advanced Filters level=initiative order=1
> Richer filter expressions for complex queries.

- [x] Story: Quoted string support in filters level=story order=1
  - [x] Enable `title:"some text"` and `description:"some text"` fields level=task order=1
  - [x] `description:` filter field for CLI and MCP task list level=task order=2
- [x] Story: Boolean operators in filters level=story order=2
  - [x] `AND` / `OR` / `NOT` operators level=task order=1
  - [x] Parenthesized grouping level=task order=2
### Initiative: TUI Polish level=initiative order=2
> Color, formatting, and quality-of-life improvements.

- [x] Story: Color-coded output level=story order=1
  - [x] Color by priority level level=task order=1
  - [x] Color by status level=task order=2
  - [x] Respect `NO_COLOR` / `--no-color` flag level=task order=3
- [x] Story: Tag colors level=story order=2
  - [x] CLI support for setting tag color (`tusk tag modify <name> --color <hex>`) level=task order=1
  - [x] Display colored tags in list/info/tree output level=task order=2
  - [x] Read default color settings from `[tui]` config section level=task order=3
- [x] Story: Markdown description rendering level=story order=3
  - [x] Terminal-rendered markdown in `tusk info` using glamour (charmbracelet) level=task order=1
  - [x] Respect `NO_COLOR` / `--no-color` flag for plain text fallback level=task order=2
### Initiative: Urgency Scoring level=initiative order=3
> Weighted multi-factor urgency algorithm for task ranking.

- [x] Story: Urgency engine level=story order=1
  - [x] Implement scoring with default weights (priority, due, age, status, blocking, blocked, tags, project, annotations, waiting) level=task order=1
  - [x] Sigmoid curve for due-date urgency level=task order=2
  - [x] Integrate urgency into default list sort level=task order=3
- [x] Story: Configurable urgency weights level=story order=2
  - [x] Read weights from config system (global defaults) level=task order=1
  - [x] `tusk next` — display highest-urgency actionable task (can ship with engine story using hardcoded defaults if needed earlier) level=task order=2
- [x] Story: Per-project urgency overrides level=story order=3
  - [x] Extend `ProjectSettings` with urgency weight overrides in config level=task order=1
  - [x] Merge project-level weights on top of global config at scoring time level=task order=2
  - [x] Expose overrides via `[projects.<id>.settings]` config section level=task order=3
## v0.7 — Player Management level=milestone order=7
> Track which player (human or agent) is working on which task, preventing overlapping work and enabling atomic task queue operations.

### Initiative: Player Entity & Registration level=initiative order=1
> Self-registering player model persisted to DB.

- [x] Story: Player domain and storage level=story order=1
  - [x] Define Player entity (`id` string PK, `type`, `registered_at`, `last_seen_at`) level=task order=1
  - [x] PlayerRepository interface and SQLite implementation level=task order=2
  - [x] Migration adding `players` table and `claimed_by`/`claimed_at` columns to `tasks` level=task order=3
  - [x] PlayerService with Register and UpdateLastSeen methods level=task order=4
  - [x] `ErrTaskClaimed` sentinel error level=task order=5
- [x] Story: Player CLI level=story order=2
  - [x] `tusk player register <id> --type human|agent` — explicit registration level=task order=1
  - [x] `--player <id>` global flag for CLI (auto-registers on first use) level=task order=2
- [x] Story: MCP player registration level=story order=3
  - [x] `tusk_player_register` tool level=task order=1
  - [x] `player_id` parameter on MCP tool calls (auto-registers on first use) level=task order=2
  - [x] Update `last_seen_at` on every player action level=task order=3
### Initiative: Task Claiming level=initiative order=2
> Claim mechanics to prevent overlapping work between players.

- [x] Story: Claim and release level=story order=1
  - [x] TaskService.Claim — set `claimed_by`/`claimed_at`, reject if already claimed (`ErrTaskClaimed`) level=task order=1
  - [x] TaskService.Release — clear claim, validate caller is the claimant level=task order=2
  - [x] Auto-claim on `tusk start` if unclaimed, reject if claimed by another level=task order=3
  - [x] Claims preserved after `done` and `delete` (historical attribution — design decision, replaces auto-release) level=task order=4
  - [x] `tusk claim <id>` / `tusk release <id>` CLI commands level=task order=5
  - [x] `tusk_task_claim` / `tusk_task_release` MCP tools level=task order=6
- [x] Story: Player visibility level=story order=2
  - [x] Include `claimed_by` and `claimed_at` in all task responses (CLI + MCP) level=task order=1
  - [x] Filter support: `claimed_by:<player_id>`, `unclaimed:true` level=task order=2
### Initiative: Task Queue level=initiative order=3
> Atomic pop operation for efficient agent orchestration. Depends on urgency scoring (v0.6) to rank tasks.

- [x] Story: Available tasks level=story order=1
  - [x] `tusk available` — convenience: unclaimed + actionable status + not blocked level=task order=1
  - [x] `tusk_task_available` MCP tool level=task order=2
- [x] Story: `tusk pop` level=story order=2
  - [x] TaskService.Pop — atomically find highest-urgency unclaimed unblocked task, claim for player, return it level=task order=1
  - [x] `tusk pop --player <id>` CLI command level=task order=2
  - [x] `tusk_task_pop` MCP tool with `player_id` input level=task order=3
  - [x] Respect filters (optional: `tusk pop project:backend`) level=task order=4
## v0.8 — Programmatic Access level=milestone order=8
> Expose tusk's core APIs as importable Go packages so other programs can embed tusk as a library.

### Initiative: Package Restructure level=initiative order=1
> Move core packages out of `internal/` to top-level, making them importable by external Go programs.

- [x] Story: Move foundational packages (domain, config) level=story order=1
  - [x] Move `internal/domain` → `domain` level=task order=1
  - [x] Move `internal/config` → `config` level=task order=2
  - [x] Update all import paths across the codebase level=task order=3
- [x] Story: Move interface and filter packages (repository, filter) level=story order=2
  - [x] Move `internal/repository` → `repository` level=task order=1
  - [x] Move `internal/filter` → `filter` level=task order=2
  - [x] Update all import paths across the codebase level=task order=3
- [x] Story: Move service and storage packages (service, sqlite, inmem) level=story order=3
  - [x] Move `internal/service` → `service` level=task order=1
  - [x] Move `internal/sqlite` → `sqlite` level=task order=2
  - [x] Move `internal/inmem` → `inmem` level=task order=3
  - [x] Update all import paths across the codebase level=task order=4
  - [x] Verify `internal/tui` and `internal/mcp` remain in `internal/` level=task order=5
### Initiative: High-level Client level=initiative order=2
> Convenience `Client` type in the root package that wires up config, DB, and services for consumers.

- [x] Story: Client type and constructor level=story order=1
  - [x] Define `Config` struct (DBPath, Workflows, Projects, Urgency) level=task order=1
  - [x] Implement `NewClient(Config) (*Client, error)` — open DB, run migrations, wire services level=task order=2
  - [x] Implement `Close() error` for cleanup level=task order=3
  - [x] Expose services as public fields (Tasks, Tags, Relations, Projects, Workflows, Players) level=task order=4
  - [x] Default to builtin kanban workflow and default project when config fields are zero-valued level=task order=5
- [x] Story: Client tests level=story order=2
  - [x] Test NewClient opens DB and creates task successfully level=task order=1
  - [x] Test NewClient with zero-valued config uses defaults level=task order=2
  - [x] Test NewClient with empty DBPath returns error level=task order=3
  - [x] Test Close releases DB connection level=task order=4
## v0.9 — Configuration Management level=milestone order=9
> CLI commands for managing configuration without manual TOML editing — create, modify, and inspect workflows, projects, and settings from the terminal.

### Initiative: Config CLI level=initiative order=1
> Read, write, and validate configuration from the command line.

- [x] Story: Config inspection level=story order=1
  - [x] `tusk config show` — display current effective configuration (merged defaults + file + env) level=task order=1
  - [x] `tusk config get <key>` — get a specific value using dot notation (e.g., `urgency.due_weight`) level=task order=2
  - [x] `tusk config path` — print resolved config file path level=task order=3
- [x] Story: Config mutation level=story order=2
  - [x] `tusk config set <key> <value>` — set a config value and write to file level=task order=1
  - [x] `tusk config edit` — open config file in `$EDITOR` level=task order=2
  - [x] `tusk config init` — create config file with defaults if none exists (no-op if file present) level=task order=3
- [x] Story: Config validation level=story order=3
  - [x] `tusk config validate` — parse and validate config, report errors (unknown keys, invalid references, type mismatches) level=task order=1
  - [x] Run validation on `config set` before writing level=task order=2
### Initiative: Inline Syntax Migration level=initiative order=2
> Extract shared parsing infrastructure from the filter package and migrate all CLI inline syntax from `key:value` to `key=value`, freeing `:` for use within values (e.g., workflow transitions `pending:active`). Prerequisite for Workflow and Project Management CLI initiatives.

- [x] Story: Extract shared lexer and AST level=story order=1
  - [x] Extract generic lexer (tokenization, quoted strings, `key=value` fields) from `filter/` into a shared parsing package level=task order=1
  - [x] First-class modifier support in the lexer — primitives: `+` (additive), `-` (subtractive), `,` (unordered set, deduplicated), `:` (ordered sequence, no dedup), `..` (range), `()` (group); extensible to future modifiers level=task order=2
  - [x] Composable modifiers — modifiers nest within groups: `status=pending(initial,highlight)` contains a `,` set inside a `()` group; the lexer's modifier system is recursive level=task order=3
  - [x] Position-based `()` disambiguation — `(` immediately after a value (no whitespace) is a group modifier on that value; `(` preceded by whitespace is a boolean grouping operator; no per-application configuration needed level=task order=4
  - [x] Quoted strings are opaque — `"value"` is a literal string, no modifier tokenization inside; `title="pending(initial)"` yields the plain string `pending(initial)` level=task order=5
  - [x] Extract AST types (`FieldFilter`, `TagFilter`, free text) into the shared package level=task order=6
  - [x] Filter package and future consumers define domain-specific token lists and field validators on top of the shared foundation level=task order=7
- [x] Story: Migrate `key:value` to `key=value` across CLI level=story order=2
  - [x] Update lexer field detection from `:` to `=` separator level=task order=1
  - [x] Update all CLI commands (`add`, `modify`, `list`, `available`, `pop`, etc.) level=task order=2
  - [x] Covers all `key:value` patterns across the codebase: filter fields from v0.1 (`status:`, `priority:`, `project:`, `due:`), quoted strings from v0.6 (`title:`, `description:`), claim filters from v0.7 (`claimed_by:`, `unclaimed:`), UDA filters from v0.5 (`uda.key:`), and inline syntax on `add`/`modify` level=task order=3
  - [x] Update filter syntax documentation and help text level=task order=4
  - [x] Update E2E tests for new syntax level=task order=5
### Initiative: Workflow Management CLI level=initiative order=3
> Create, modify, and remove workflows via CLI commands that write to the config file. Mutation logic lives in the `config` package for reuse by MCP tools.

- [x] Story: Workflow CRUD commands level=story order=1
  - [x] `tusk workflow create <name> [fields...]` — all-inline syntax: `status=pending(initial) status=active(start) status=completed(terminal,done) status=deleted(terminal,delete) transition=pending:active,active:completed,active:deleted` level=task order=1
  - [x] `tusk workflow modify <name> [fields...]` — replace: `status=active(start,highlight)`; additive: `+status=review +transition=active:review`; subtractive: `-status=review -transition=active:review` level=task order=2
  - [x] `tusk workflow delete <name>` — remove workflow from config (reject if referenced by a project) level=task order=3
- [x] Story: Status roles schema level=story order=2
  - [x] Change `WorkflowConfig.Statuses` from `[]string` to `map[string]StatusConfig` — each status carries a `roles` list level=task order=1
  - [x] Built-in roles: `initial` (default for new tasks), `start` (target for `tusk start`/`pop`), `terminal` (task is finished), `done` (target for `tusk done`), `delete` (target for `tusk delete`), `highlight` (emphasized in output), `dim` (deemphasized in output) level=task order=2
  - [x] Remove top-level `highlight_statuses` and `dim_statuses` fields — replaced by `highlight` and `dim` roles on individual statuses level=task order=3
  - [x] Migration of existing `WorkflowConfig`: map `highlight_statuses`/`dim_statuses` lists to roles on the corresponding status entries level=task order=4
  - [x] Replace hardcoded `"pending"` fallback in `TaskService.Create` — look up the status with `initial` role level=task order=5
  - [x] Replace hardcoded `"active"` in `TaskService.Start`/`Pop` — look up the status with `start` role level=task order=6
  - [x] Replace hardcoded `"completed"` in `TaskService.Complete` — look up the status with `done` role level=task order=7
  - [x] Replace hardcoded `"deleted"` in `TaskService.Delete` — look up the status with `delete` role level=task order=8
  - [x] Replace hardcoded `"pending","active"` in `TaskService.Available`/`Pop` — derive actionable statuses as those without the `terminal` role level=task order=9
  - [x] Validation: exactly one `initial` status; exactly one `start` status with valid transition from `initial`; at least one `terminal` status; `done` and `delete` roles must be on statuses that also have `terminal`; at most one status per `initial`, `start`, `done`, `delete` role level=task order=10
- [x] Story: Config package workflow mutations level=story order=3
  - [x] `config.CreateWorkflow(name, WorkflowConfig)` — add workflow to config, validate, write level=task order=1
  - [x] `config.ModifyWorkflow(name, WorkflowMutation)` — apply field changes (replace/add/remove), validate, write level=task order=2
  - [x] `config.DeleteWorkflow(name)` — validate no project references, remove, write level=task order=3
### Initiative: Inline Syntax Modifier AST level=initiative order=4
> Promote the `+`/`-` token prefix to a first-class AST property with both list-op and arithmetic-op variants. Prerequisite for Project Management CLI urgency weight mutations; lets commands stop hand-rolling prefix parsing.

- [x] Story: Field modifier AST level=story order=1
  - [x] Extend `syntax.FieldFilter` with a `Modifier` field carrying the raw prefix rune only (empty = bare). No domain semantics attached at the AST level. level=task order=1
  - [x] Treat the set of recognized modifier prefixes as an open, extensible registry in the syntax package — initially `+` and `-`, designed so new prefixes (e.g. `?`, `*`) can be added without changing the `FieldFilter` shape or the consumer-facing API. Adding a new prefix is a one-line registry change plus consumer opt-in. level=task order=2
  - [x] Lexer consults the registry when scanning a token's first character: if the char is a registered modifier and is followed by a field/tag body, strip it into the AST marker; otherwise treat it as part of the value level=task order=3
  - [x] `FieldFilter.Key`/`FieldFilter.Value` always expose the bare form; modifier carried separately so consumers pattern-match on it without re-parsing strings level=task order=4
  - [x] The syntax package is explicit that it does not interpret modifiers — whether `+` means "append to a list", "add arithmetically", "include", or something else is entirely the consumer command's decision. The same neutral AST shape serves all of them, and the same applies to any future modifier. level=task order=5
  - [x] Migrate `internal/tui/workflow_parse.go` to read `FieldFilter.Modifier` instead of inspecting string prefixes — the workflow command interprets `+`/`-` as list add/remove on `status`/`transition` level=task order=6
  - [x] Migrate filter and task add/modify parsers to use the same field; list/tag semantics they assign are unchanged externally level=task order=7
  - [x] Unit tests cover lexing each registered modifier into the AST with no semantic interpretation, plus a "register a new modifier" test that proves the extensibility path works without touching consumer code. Consumer-level tests live in their respective packages and cover the interpretation layer. level=task order=8
### Initiative: Project Management CLI ✅ level=initiative order=5
> Create, modify, and remove projects via CLI commands that write to the config file.

- [x] Story: Project CRUD commands level=story order=1
  - [x] `tusk project create <name> [fields...]` — inline syntax: `workflow=kanban db-path=/data/b.db auto-complete.trigger=completed urgency.blocking-weight=15` level=task order=1
  - [x] `tusk project modify <name> [fields...]` — inline syntax: bare assignment replaces (`workflow=sprint`, `urgency.blocking-weight=10`); `+key=value`/`-key=value` apply arithmetic deltas on numeric urgency weights (`+urgency.blocking-weight=2` adds 2, `-urgency.blocking-weight=1` subtracts 1) level=task order=2
  - [x] Numeric delta resolution: when the project override is unset, the delta applies relative to the effective global urgency weight and the result is written as a new project override level=task order=3
  - [x] Accepted fields: `workflow`, `db-path`, `auto-complete.trigger`, `auto-complete.target`, `auto-revert.trigger`, `auto-revert.target`, and every `urgency.<weight>` key level=task order=4
  - [x] `tusk project delete <name>` — removes project from config; rejects if any tasks reference it; rejects deleting the built-in `default` project; `--force` bypasses both guards and emits a warning with the task count level=task order=5
  - [x] Config package mutations: `config.CreateProject`, `config.ModifyProject`, `config.DeleteProject` — mirror the workflow mutation helpers, reusable by MCP tools level=task order=6
  - [x] Task-reference check passes a callback into `config.DeleteProject` so the config package stays free of service/repository imports level=task order=7
- [x] Story: Per-project database path level=story order=2
  - [x] `[projects.<name>].db_path` config key — optional SQLite file path per project; `db-path=...` in inline syntax writes it, `db-path=` clears it level=task order=1
  - [x] Paths resolve relative to the config file's directory (absolute paths used as-is, `~` expanded); projects without `db_path` use the global `storage.path` level=task order=2
  - [x] Store registry: lazily open and migrate per-project databases on first access, reuse the connection for subsequent operations, close all on shutdown level=task order=3
  - [x] Task, annotation, relation, and tag repositories are bundled per store; services resolve the correct bundle via the registry using the task's project ID level=task order=4
  - [x] Cross-project reads (unfiltered `tusk list`, `available`, `next`) fan out across all known stores and merge results, re-sorting by urgency in memory before applying limit/offset level=task order=5
  - [x] `tusk pop` picks the highest-urgency candidate across stores, then claims it in its own store with retry on optimistic-lock conflict level=task order=6
  - [x] Relations must link tasks within the same store — `RelationService.Create` rejects cross-store links to preserve referential integrity; documented as a per-project-DB constraint level=task order=7
### Initiative: Workspace Scope Collapse level=initiative order=6
> Internal refactor that makes the config file's directory the one workspace namespace. Removes per-project `db_path`, collapses `StoreRegistry` to a single store, and drops cross-store fan-out. No user-visible config resolution changes yet — tusk still reads the global `config.toml` only. Ships first so the rest of the config work has a simple "one config, one DB" invariant to build on.

- [x] Story: Remove per-project db_path from the config schema level=story order=1
  - [x] Delete `[projects.<name>].db_path` from the config types and TOML schema level=task order=1
  - [x] Remove `db-path=` from `tusk project create` / `tusk project modify` inline syntax, and remove it from the accepted-fields list level=task order=2
  - [x] Update `config/default.toml` comments and any docs-embedded examples level=task order=3
  - [x] Explicitly supersedes the v0.8 "Per-Project Databases" stories, which remain in the roadmap as historical record level=task order=4
- [x] Story: Collapse StoreRegistry to a single workspace store level=story order=2
  - [x] Replace `StoreRegistry` with a single `Store` opened from `cfg.Storage.Path` at startup level=task order=1
  - [x] Service resolver (`RepoBundle` provider) returns the workspace store regardless of project ID level=task order=2
  - [x] `baseDir` for relative `storage.path` still resolves against the active config file's directory (unchanged contract, simpler implementation) level=task order=3
- [x] Story: Remove cross-store fan-out from services level=story order=3
  - [x] `TaskService.List`, `available`, `next` no longer fan out across stores — they run one query against the workspace store with project filters applied in SQL level=task order=1
  - [x] `tusk pop` selects the top-urgency candidate via a single query and claims it in the same store; optimistic-lock retry stays but cross-store retry logic is removed level=task order=2
  - [x] `RelationService` drops the same-store constraint and re-allows relations between tasks in different projects within the workspace level=task order=3
  - [x] `projectLister` closure in `cmd/tusk` is replaced by reading project IDs from the config level=task order=4
- [x] Story: Migration guidance for existing per-project DBs level=story order=4
  - [x] Documented in `docs/` as a manual export/import procedure (export each per-project DB with `tusk export --format json`, merge into the new workspace DB) level=task order=1
  - [x] No automatic migration shipped — per-project DBs predate v0.1 and had no production users level=task order=2
  - [x] Release notes flag it as a breaking change for any user who set `db_path` in their config level=task order=3
### Initiative: Explicit Config File Resolver level=initiative order=7
> Introduce the config resolution abstraction that walk-up discovery will later plug into. Adds the `--config` flag and `TUSK_CONFIG` env var as first-class ways to point tusk at any config file, plus `config path` and an active-file header on `config show`. No walk-up yet — the resolver's precedence chain is `--config` → `TUSK_CONFIG` → global → defaults. Delivered on top of the single-workspace model from the previous initiative.

- [x] Story: Config resolver abstraction level=story order=1
  - [x] Introduce `ResolveConfigFile(startDir, explicitFile, globalDir) (string, error)` in `config/` that returns the active config file path or `""` for "defaults only" level=task order=1
  - [x] Initial implementation: returns `explicitFile` if set, otherwise `globalDir/config.toml` if it exists, otherwise `""`. Walk-up step is reserved for the next initiative. level=task order=2
  - [x] `config.Load()` routes through the resolver; legacy `WithSearchPath` option is preserved for tests but documented as "sets globalDir" level=task order=3
  - [x] `config.Load()` returns the resolved file path alongside the `*Config` (e.g. via a `Sources` field or a second return value) so callers can render it level=task order=4
- [x] Story: `--config` flag and `TUSK_CONFIG` env var level=story order=2
  - [x] Add global `--config <path>` flag handled before Cobra parsing, parallel to the existing `--db` handling in `cmd/tusk/main.go` level=task order=1
  - [x] Add `TUSK_CONFIG` env var as a fallback for `--config`. `TUSK_CONFIG_DIR` remains valid and untouched (it sets `globalDir`). level=task order=2
  - [x] Missing `--config` / `TUSK_CONFIG` target file is a hard error at `Load()` time; missing global file falls through to defaults silently level=task order=3
  - [x] Precedence at this point: `TUSK_*` env values > `--config` / `TUSK_CONFIG` file > global file > embedded defaults level=task order=4
- [x] Story: `config path` and active-file header level=story order=3
  - [x] New `tusk config path` subcommand prints the resolved active file path, or the path `tusk config init` would create when none is active level=task order=1
  - [x] `tusk config show` prepends a header indicating which file is active (`# active: /path/to/config.toml` or `# active: defaults only`) level=task order=2
  - [x] `tusk config edit` opens the resolved active file (honoring `--config` / `TUSK_CONFIG`) level=task order=3
  - [x] `tusk config validate` validates the resolved file level=task order=4
### Initiative: Local Config Discovery level=initiative order=8
> Walk-up config resolution analogous to `package.json` in Node.js. Extends the resolver from the previous initiative with a walk-up step so tusk picks the nearest `tusk.toml` from the CWD upward, falling back to the global `config.toml` when the walk finds nothing. Single-file model — first match wins, no merging between user configs. Also lands the workspace-aware write commands (`config set`, `config init --local`).

- [x] Story: Walk-up step in the resolver level=story order=1
  - [x] Insert walk-up between `TUSK_CONFIG` and global in `ResolveConfigFile`: starting at CWD, check each ancestor directory for `tusk.toml` and return the first hit level=task order=1
  - [x] Walk stops at filesystem root; no symlink resolution level=task order=2
  - [x] Walk is skipped entirely when `--config` or `TUSK_CONFIG` is set (the bypass stays authoritative) level=task order=3
  - [x] Final precedence: `TUSK_*` env > `--config` > `TUSK_CONFIG` > walk-up `tusk.toml` > global `config.toml` > embedded defaults level=task order=4
- [x] Story: Relative paths resolve to the config file's directory level=story order=2
  - [x] `storage.path` and any other file-path field resolve relative to the directory that contains the active config file, not the caller's CWD level=task order=1
  - [x] `tusk` run from any subdirectory of a project with a `tusk.toml` at the root hits the same database as `tusk` run from the root itself level=task order=2
  - [x] Absolute paths and `~`-prefixed paths are untouched level=task order=3
- [x] Story: Workspace-aware `config set` level=story order=3
  - [x] `tusk config set <key> <value>` writes to the file `Load()` resolved — whichever `tusk.toml` or `config.toml` is active level=task order=1
  - [x] `--global` flag forces writes to `~/.config/tusk/config.toml` even when a walk-up `tusk.toml` is active level=task order=2
  - [x] With no active file and no `--global`, emit a clear error pointing at `tusk config init` or `tusk config init --local` level=task order=3
- [x] Story: `config init --local` level=story order=4
  - [x] `tusk config init --local` creates `./tusk.toml` containing a full dump of the current effective config level=task order=1
  - [x] Errors if `./tusk.toml` already exists level=task order=2
  - [x] `tusk config init` (no flag) still writes global defaults as today level=task order=3
- [x] Story: Conditional global auto-create level=story order=5
  - [x] `config.Load()` auto-creates `~/.config/tusk/config.toml` on first run only when walk-up finds no `tusk.toml` and no `--config` / `TUSK_CONFIG` override is set level=task order=1
  - [x] Running tusk inside a project with a local config never spawns a global file level=task order=2
  - [x] Existing behavior preserved for fresh installs operating outside any tusk project level=task order=3
- [x] Story: `config show` / `config path` report walk-up hits level=story order=6
  - [x] Active-file header on `config show` correctly reflects walk-up discoveries (e.g. `# active: /repo/tusk.toml`) level=task order=1
  - [x] `config path` prints the walk-up hit when one is active, the global path otherwise level=task order=2
  - [x] E2E coverage: subdirectory walk-up, ancestor walk-up, `--config` override, `TUSK_CONFIG` override, no-config fallthrough level=task order=3
### Initiative: MCP Config Tools level=initiative order=9
> Expose configuration management to AI agents via MCP tools.

- [x] Story: Config MCP tools level=story order=1
  - [x] `tusk_config_show` — read effective configuration level=task order=1
  - [x] `tusk_config_set` — set a config value level=task order=2
  - [x] `tusk_workflow_create` / `tusk_workflow_modify` / `tusk_workflow_delete` — workflow management level=task order=3
  - [x] `tusk_project_create` / `tusk_project_modify` / `tusk_project_delete` — project management level=task order=4
## v0.10 — Datastore-Backed Projects & Workflows level=milestone order=10
> Move projects and workflows out of the config file and into the workspace database. With config files now acting as workspace namespaces (walk-up discovery, local `tusk.toml`), projects and workflows are workspace data, not user configuration. Tasks already live in the DB — projects and workflows should too.

### Initiative: Project & Workflow Schema level=initiative order=1
> Persistent storage for projects and workflows in the workspace database, with optimistic locking like every other mutable entity.

- [x] Story: Projects table level=story order=1
  - [x] Define `domain.Project` entity (`id` UUID, `name`, `workflow_id` UUID, `settings` JSON, `version`, `created_at`, `updated_at`) level=task order=1
  - [x] `settings` JSON carries `auto_complete`, `auto_revert`, and `urgency` overrides — JSON chosen over dedicated columns because per-project overrides are read once per service call and written rarely; promote to columns only if profiling shows the JSON decode is hot level=task order=2
  - [x] Migration adding `projects` table with unique index on `name` level=task order=3
  - [x] Seed built-in `_default` project (UUID all zeros) as a regular row in the migration — no special-case code paths level=task order=4
  - [x] `ProjectRepository` interface (`Create`, `Get`, `GetByName`, `List`, `Update`, `Delete`) and SQLite implementation with version-checked updates returning `domain.ErrConflict` level=task order=5
- [x] Story: Workflows table level=story order=2
  - [x] Define `domain.Workflow` entity (`id` UUID, `name`, `statuses` JSON, `transitions` JSON, `version`, `created_at`, `updated_at`) level=task order=1
  - [x] Statuses keep the v0.9 role schema (`initial`, `start`, `terminal`, `done`, `delete`, `highlight`, `dim`) — serialized as JSON to avoid a second table just for status rows level=task order=2
  - [x] Migration adding `workflows` table with unique index on `name` level=task order=3
  - [x] Seed built-in default workflow (`pending`/`active`/`completed`/`deleted` with roles) as a regular row in the migration level=task order=4
  - [x] `WorkflowRepository` interface (`Create`, `Get`, `GetByName`, `List`, `Update`, `Delete`) and SQLite implementation with version-checked updates level=task order=5
- [x] Story: Foreign key from tasks to projects level=story order=3
  - [x] Migration converts `tasks.project_id` to a real FK referencing `projects.id` (was previously just a UUID column with no DB-level integrity) level=task order=1
  - [x] `ON DELETE RESTRICT` so the existing "reject delete if tasks reference it" guard gets DB-level enforcement in addition to the service-level check level=task order=2
  - [x] Workflows are referenced via `projects.workflow_id` FK with `ON DELETE RESTRICT` level=task order=3
### Initiative: Service Layer Migration level=initiative order=2
> `ProjectService` and `WorkflowService` read and write the database instead of the config file. `inmem/` implementations are deleted — the in-memory path only existed because the source of truth was a TOML file held in memory after `config.Load()`.

- [x] Story: ProjectService over repository level=story order=1
  - [x] `ProjectService.Create`/`Modify`/`Delete`/`List`/`Get` call `ProjectRepository` directly level=task order=1
  - [x] Optimistic locking: callers fetch to get `version`, mutations pass it through, `ErrConflict` bubbles up like task mutations level=task order=2
  - [x] Drop `config.CreateProject`/`ModifyProject`/`DeleteProject` — their TOML-writing logic is removed and callers switch to the service level=task order=3
  - [x] Service-level delete guard (reject if tasks reference the project, reject deleting `_default`, `--force` bypass) stays in the service and runs before the DB delete level=task order=4
- [x] Story: WorkflowService over repository level=story order=2
  - [x] `WorkflowService.Create`/`Modify`/`Delete`/`List`/`Get` call `WorkflowRepository` directly level=task order=1
  - [x] Role-schema validation (exactly one `initial`, one `start`, ≥1 `terminal`, etc.) moves from config validation into the service level=task order=2
  - [x] Delete guard rejects workflows referenced by any project — implemented via a repository-level `CountProjectsByWorkflow` call, not a full project list scan level=task order=3
  - [x] Drop `config.CreateWorkflow`/`ModifyWorkflow`/`DeleteWorkflow` level=task order=4
- [x] Story: Retire `inmem/` project and workflow stores level=story order=3
  - [x] Delete `inmem/project.go` and `inmem/workflow.go` level=task order=1
  - [x] DI wiring in `cmd/tusk/` constructs SQLite repositories from the workspace store level=task order=2
  - [x] Tests that used `inmem` for project/workflow setup switch to the SQLite store via the existing test harness level=task order=3
### Initiative: Config Schema Trim level=initiative order=3
> Remove `[projects.*]` and `[workflows.*]` from the config file. Config keeps global settings only — `storage.*`, global `urgency.*`, global `auto_complete.*`, `mcp.*`, `filter.*`, etc. `config show` still renders projects and workflows, now sourced from the DB.

- [x] Story: Remove project/workflow sections from the config schema level=story order=1
  - [x] Delete `ProjectConfig` and `WorkflowConfig` from `config/` types level=task order=1
  - [x] Remove `[projects.<name>]` and `[workflows.<name>]` from `config/default.toml` level=task order=2
  - [x] Config loader emits a hard error if the resolved file still contains these sections, pointing at the migration command (see next initiative) level=task order=3
  - [x] Global `[urgency]` and `[auto_complete]` stay in config as defaults — project overrides live in the DB `projects.settings` JSON level=task order=4
- [x] Story: `config show` reads projects and workflows from DB level=story order=2
  - [x] `config show` output keeps rendering `[projects.*]` and `[workflows.*]` sections for continuity, hydrated from the DB at display time level=task order=1
  - [x] Sections are marked read-only in the rendered header (e.g. `# projects (from database, use 'tusk project' to modify)`) level=task order=2
  - [x] `config get projects.<name>.<field>` / `config get workflows.<name>.<field>` resolve against the DB level=task order=3
  - [x] `config set` rejects keys under `projects.*` and `workflows.*` with an error pointing at `tusk project modify` / `tusk workflow modify` level=task order=4
### Initiative: CLI & MCP Rewiring level=initiative order=4
> `tusk project` and `tusk workflow` subcommands (and their MCP counterparts) mutate the database through the services instead of the config file. External surface is nearly unchanged — same flags, same inline syntax — only the storage backend moves.

- [x] Story: Project and workflow CLI over services level=story order=1
  - [x] `tusk project create`/`modify`/`delete`/`list` call `ProjectService` directly level=task order=1
  - [x] `tusk workflow create`/`modify`/`delete`/`list` call `WorkflowService` directly level=task order=2
  - [x] Inline syntax (`workflow=kanban`, `urgency.blocking-weight=15`, `+urgency.blocking-weight=2`, etc.) is unchanged — the parser produces the same AST, only the write target moves level=task order=3
  - [x] Numeric delta resolution for urgency weights still reads the effective global weight from config and stores the resolved override in `projects.settings` level=task order=4
- [x] Story: MCP project and workflow tools over services level=story order=2
  - [x] `tusk_project_create`/`modify`/`delete` and `tusk_workflow_create`/`modify`/`delete` call the services level=task order=1
  - [x] Tools accept and return `version` for optimistic locking, matching `tusk_task_*` conventions level=task order=2
  - [x] The config mutex that previously serialized project/workflow writes (`eec8ec6`) is removed — DB-level optimistic locking replaces it level=task order=3
## v0.11 — CLI Command Grouping level=milestone order=11
> Regroup the CLI under explicit subcommand namespaces so the surface scales cleanly as the system grows. Early-stage Tusk shipped flat commands (`tusk add`, `tusk start`, `tusk done`); with projects, workflows, players, tags, config, notes, and dashboard all competing for top-level slots, the flat layout is noisy and ambiguous. This milestone moves every task-scoped verb under `tusk task` and leaves only workspace-wide operations at the top level. Pre-release, so no backward-compat aliases — clean break.
>
> Alongside the regrouping, v0.11 locks in a principle the CLI has been drifting toward since v0.9: **entity properties flow through the inline `key=value` lexer, not ad-hoc Cobra flags.** `priority=3`, `project=backend`, `due=today`, `+tag`, `parent=a3f8b2c1`, `uda.env=prod` — every property that describes *what the task is* goes through the shared syntax pipeline, so the lexer/AST owns every entity-shaped input and there is exactly one way to set a field on a task. Cobra flags stay reserved for invocation-level concerns that aren't entity properties: actor identity (`--player`), view toggles (`--all`, `--output`), config scoping (`--config`, `--db`, `--global`). This avoids Cobra-custom flag surfaces overlapping the lexer, keeps the CLI and MCP field sets aligned (MCP already accepts entity properties as structured JSON, never as flags), and means every new field added to a task is one entry in the field registry instead of a new flag on every consumer command. The two remaining flag-based task entity properties — `--description` and `--uda` — are eliminated in their own initiatives below.
>

### Initiative: `tusk task` Subcommand Group level=initiative order=1
> Move every task-scoped command under a single `tusk task` parent. Verbs, flags, inline syntax, and output are unchanged — only the invocation path moves. Pre-release, so no backward-compat aliases — removed commands stay removed.

- [x] Story: Scope — which commands move and which stay flat level=story order=1
  - [x] Moves under `tusk task`: every task-scoped verb (CRUD, lifecycle, claim/queue, relations) level=task order=1
  - [x] Stays flat — workspace-wide operations that don't belong to any single entity: `tusk undo` (reverts the last mutation regardless of entity type), `tusk export` (workspace-wide data dump), `tusk dashboard` (workspace-wide view), `tusk mcp serve` (server invocation, not an entity operation) level=task order=2
  - [x] Already grouped, no change: `tusk config`, `tusk project`, `tusk workflow`, `tusk player`, `tusk tag`, `tusk note` level=task order=3
  - [x] This story is a decision/scoping gate — the mapping table it locks in drives every downstream story in this milestone level=task order=4
- [x] Story: `tusk task` parent command skeleton level=story order=2
  - [x] Register the `tusk task` parent Cobra command with its long help listing all subcommands with one-line summaries level=task order=1
  - [x] Wire it into the root command so `tusk task` (no subcommand) prints usage and exits cleanly level=task order=2
  - [x] Establishes the parent so each subsequent move story is a drop-in `AddCommand` call rather than a restructure level=task order=3
- [x] Story: Task CRUD and lifecycle under `tusk task` level=story order=3
  - [x] `tusk add` → `tusk task create` level=task order=1
  - [x] `tusk list` → `tusk task list` level=task order=2
  - [x] `tusk info` → `tusk task get` level=task order=3
  - [x] `tusk modify` → `tusk task modify` level=task order=4
  - [x] `tusk start` → `tusk task start` level=task order=5
  - [x] `tusk done` → `tusk task done` level=task order=6
  - [x] `tusk delete` → `tusk task delete` level=task order=7
  - [x] `tusk tree` → `tusk task tree` level=task order=8
  - [x] `tusk next` → `tusk task next` level=task order=9
  - [x] `tusk annotate` → `tusk task annotate` level=task order=10
- [x] Story: Claim and queue under `tusk task` level=story order=4
  - [x] `tusk available` → `tusk task available` level=task order=1
  - [x] `tusk pop` → `tusk task pop` level=task order=2
  - [x] `tusk claim` → `tusk task claim` level=task order=3
  - [x] `tusk release` → `tusk task release` level=task order=4
- [x] Story: Relations under `tusk task` level=story order=5
  - [x] `tusk link` → `tusk task link` level=task order=1
  - [x] `tusk unlink` → `tusk task unlink` level=task order=2
  - [x] MCP tools rename to match: `tusk_relation_add` → `tusk_task_link`, `tusk_relation_remove` → `tusk_task_unlink`. MCP and CLI surfaces stay in lockstep so agents and humans share the same mental model. level=task order=3
- [x] Story: Removal and suggestions for moved commands level=story order=6
  - [x] Old flat commands are deleted from the root — Cobra emits its standard "unknown command" error for each level=task order=1
  - [x] A custom `SuggestFor` / unknown-command handler prints a targeted hint for moved verbs so `tusk add foo` prints "unknown command 'add'; did you mean 'tusk task create'?" for every entry in the scope story's mapping table level=task order=2
  - [x] Runs last in this initiative so the hint table reflects the final set of moved commands level=task order=3
### Initiative: `tusk completion` Subcommand level=initiative order=2
> Add a `tusk completion` subcommand that emits shell completion scripts for bash, zsh, fish, and PowerShell. Without it, surface reorganizations like the `tusk task` grouping leave users with stale completions and no in-repo way to refresh them — the roadmap item "regenerate completions after the new command tree" has no mechanism to tick against. Cobra already ships a built-in completion generator; this initiative wires it into the root command tree and documents the install flow, so every future CLI surface change (v0.11 string-field unification, v0.11 UDA flag elimination, v0.12 notes window, and onward) can point users at a single refresh command instead of bespoke per-release completion scripts.

- [x] Story: Wire Cobra's completion generator into the root command level=story order=1
  - [x] Call `rootCmd.AddCommand(...)` with the `cobra.Command` returned by Cobra's built-in completion generator, using the standard four shells (`bash`, `zsh`, `fish`, `powershell`) level=task order=1
  - [x] `tusk completion bash`, `tusk completion zsh`, `tusk completion fish`, `tusk completion powershell` each emit a completion script to stdout for the current command tree level=task order=2
  - [x] The subcommand is visible in `tusk --help` alongside `tusk version`, `tusk mcp`, and the grouped entity commands level=task order=3
  - [x] No persistent flag parsing side effects — the completion command runs without touching the workspace database or config resolution level=task order=4
- [x] Story: Document the install flow level=story order=2
  - [x] Add a "Shell completion" section to `PRODUCT.md` under the CLI interface that shows the generate-and-install commands for each supported shell level=task order=1
  - [x] Add a matching section to `docs/configuration.md` (or a new `docs/shell-completion.md` if it grows past a few paragraphs) with per-shell install paths (`~/.bash_completion.d/`, `~/.zsh/completions/`, `~/.config/fish/completions/`, PowerShell profile) level=task order=2
  - [x] Call out in the section that completion scripts are generated on demand — there are no pre-baked completion artifacts in the repo or release tarballs, so users regenerate after every tusk upgrade level=task order=3
- [x] Story: Completion tests level=story order=3
  - [x] Add an e2e test that invokes `tusk completion bash`, `tusk completion zsh`, `tusk completion fish`, and `tusk completion powershell` and asserts non-empty stdout and exit code 0 — smoke-level, not script parsing level=task order=1
  - [x] Add a regression check that `tusk completion bash` output mentions every top-level subcommand currently registered on the root (`task`, `project`, `workflow`, `tag`, `player`, `config`, `mcp`, `completion`, `version`, and the workspace-wide verbs) — if a future refactor drops a command from the root, the test fails loudly level=task order=2
### Initiative: String Field Input Unification level=initiative order=3
> Executes the milestone-wide inline-field principle for free-form string fields. The `description` field lives outside the lexer today — it's a Cobra flag (`--description`/`-d`) with bespoke `@file` / `@-` expansion, while `title` and every other field already flow through inline `key=value` syntax. This initiative moves `description` onto the inline surface alongside `title`, annotation bodies, and any future string field, and it adds an inline `@` reference expander that substitutes file content (or stdin) directly into the decoded string value at the consumer layer. Runs after the `tusk task` grouping initiative so it acts on the already-renamed commands once, not twice, and before the UDA flag elimination initiative so both flag-removal stories share the same rewired command surface.
>
> **Scope note:** The original story set included `syntax.ValueModifier` AST changes and a value-prefix modifier registry. These were dropped in design — `@` is inline text substitution, not a prefix marker, so the mid-string case (`"text @file.txt"`) cannot be represented as a stripped AST marker. The shipped implementation is a pure consumer-layer text pass with no lexer or AST changes. See `docs/plans/v0.11-string-field-input-unification/design.md` for the full reasoning.

- [x] Story: Word-boundary `@` reference expansion level=story order=1
  - [x] Add a CLI-layer expander `internal/tui.expandRefs(raw, stdin, maxSize)` that scans a string for word-boundary `@` references and substitutes file content (or stdin for `@-`) inline level=task order=1
  - [x] Word boundary means start-of-string or preceded by ASCII whitespace — `foo@bar.com` and `user@host` are never expanded level=task order=2
  - [x] Bare path scans until next whitespace; quoted path `@"./name with space.txt"` scans a quoted span for paths containing spaces level=task order=3
  - [x] `@@` at a word boundary escapes to a literal `@` level=task order=4
  - [x] `@-` reads stdin; stdin may only be referenced once per invocation (enforced across multiple `expandRefsWithState` calls in one command via a shared state struct) level=task order=5
  - [x] Substituted content is **not** re-scanned for nested references — expansion is one level deep level=task order=6
  - [x] No AST or lexer changes — the expander runs on the final decoded string value from the v0.9 lexer, after quotes have already collapsed. Quoted lexer values are **not** opaque to `@` expansion; lexer quoting escapes shell/lexer syntax, `@@` escapes the expander. level=task order=7
- [x] Story: Expander file-read and stdin semantics level=story order=2
  - [x] File paths resolve via `os.ReadFile` against the caller's CWD; `~/` prefix expands via `os.UserHomeDir`; absolute paths pass through level=task order=1
  - [x] Missing file → `@<path>: no such file` error level=task order=2
  - [x] Binary detection via NUL-byte scan on the first 8 KB of content (git's approach); binary files rejected with an error pointing at future attachment support level=task order=3
  - [x] Per-reference size cap configured via `inline.max_expansion_size` (default 1 MB); over-cap files rejected with actual size and limit in the error message level=task order=4
  - [x] Stdin TTY guard preserved from the old `readDescription` helper level=task order=5
  - [x] Replaces `internal/tui/description.go` entirely — the old helper and its tests are deleted level=task order=6
- [x] Story: Drop `--description` flag, use inline field level=story order=3
  - [x] Remove `--description` / `-d` from `tusk task create` and `tusk task modify` level=task order=1
  - [x] Commands read the `description=` field value from `FilterSet.GetField` and pass it through the expander level=task order=2
  - [x] `description=` with an empty value clears the field on modify (matches the old `--description ""` behavior, feeds into the double-pointer `**string` update path) level=task order=3
  - [x] Same pattern applied to `title=` so `title=@./title.txt` works on create and modify level=task order=4
- [x] Story: Positional bodies gain `@` expansion level=story order=4
  - [x] `tusk task annotate <id> "body"` stays positional — annotation commands are single-value and the positional form is idiomatic level=task order=1
  - [x] The positional body runs through the expander, so `tusk task annotate a3f8b2c1 @./notes.md` and `tusk task annotate a3f8b2c1 @-` work with the same semantics as `description=@...` level=task order=2
  - [x] Literal `@` at the start of a positional body is escaped at the expander level with `@@` (or quoted at the shell level as a fallback); shell quoting remains the user's responsibility level=task order=3
  - [x] `tusk note add "body"` (v0.12) inherits the same convention from day one — documented in the v0.12 note CLI story rather than patched in later level=task order=4
- [x] Story: MCP field parity check level=story order=5
  - [x] MCP tools receive description, title, and body as structured JSON fields, so no `@file` expansion is needed on that surface — agents already pass the content directly level=task order=1
  - [x] Tool schemas stay unchanged; only the CLI surface moves level=task order=2
  - [x] Verification pass confirmed no MCP tool accidentally grew a `@` interpretation while the CLI was being rewired: `grep -rn "expandRefs\|expandRefsWithState\|expandState" internal/mcp/` → no hits; `grep -rn "readDescription" internal/mcp/` → no hits; `grep -rn "@\"" internal/mcp/` → no hits; `grep -rn "@-" internal/mcp/` → no hits. `tusk_task_create`, `tusk_task_modify`, and `tusk_task_annotate` schemas list `title`, `description`, and `body` as plain `string` parameters, and the handlers in `internal/mcp/tools.go` call `TaskService.Create`/`Update`/`Annotate` directly with the raw JSON-supplied strings. level=task order=3
### Initiative: UDA Flag Elimination level=initiative order=4
> User-defined attributes are currently set via `--uda key=value` (repeatable) on `tusk task create` and `tusk task modify`, while every other entity property on those same commands is inline. This initiative drops `--uda` in favor of dotted inline fields (`uda.key=value`) so UDAs obey the milestone-wide principle and match the filter syntax already documented in `PRODUCT.md` (`uda.env=prod` works identically in filters, create, and modify). No lexer change is required — dotted keys already flow through the v0.9 key tokenizer — so this initiative is pure consumer rewiring on top of the String Field Input Unification work.

- [x] Story: Dotted UDA field recognition in task commands level=story order=1
  - [x] `runAdd` and `runModify` iterate the parsed field list and treat every field whose key has a `uda.` prefix as a UDA entry, with the tail after the prefix as the UDA key level=task order=1
  - [x] `uda.key=value` sets the attribute; `uda.key=` (empty value) clears it on modify, matching the double-pointer `**string` update path already used for nullable fields level=task order=2
  - [x] Repetition works naturally — the parser already allows multiple fields, so `uda.env=prod uda.region=eu` sets two attributes in one invocation without any array-of-flags plumbing level=task order=3
  - [x] Dotted keys coexist with the reserved top-level keys (`title`, `priority`, `project`, `parent`, `due`, `status`, `description`, `tree`) — a `uda.` prefix is the only disambiguator, and a bare `env=prod` is still rejected as an unknown top-level field so typos surface loudly instead of silently becoming UDAs level=task order=4
- [x] Story: Drop `--uda` / `-u` flag level=story order=2
  - [x] Remove `--uda` / `-u` from `tusk task create` and `tusk task modify` in `internal/tui/commands.go` level=task order=1
  - [x] Delete the `parseUDAFlags` helper and its tests once every caller has moved to the inline path level=task order=2
  - [x] Cobra emits its standard "unknown flag" error for stale `--uda` invocations — no targeted suggestion shim, since the inline syntax is documented in both help text and the dotted-field error path level=task order=3
  - [x] Runs second so the inline recognizer is proven before the old flag disappears level=task order=4
- [x] Story: MCP parity check for UDAs level=story order=3
  - [x] MCP task tools already accept UDAs as a structured `uda` object in the tool schema — no dotted-key translation needed on that surface level=task order=1
  - [x] Tool schemas stay unchanged; verification pass confirms no MCP handler accidentally grew a `uda.`-prefix parser while the CLI was being rewired level=task order=2
  - [x] Runs last as a symmetric verification to the String Field Input Unification initiative's MCP check level=task order=3
### Initiative: Documentation and Test Rewrite level=initiative order=5
> Every doc example, help string, and E2E scenario references the old flat commands. All need to move in lockstep with the CLI change, or the release ships with broken examples. Runs last in the milestone — the command surface and field conventions must be final before the surrounding material is rewritten.

- [x] Story: Help text and command descriptions level=story order=1
  - [x] Every moved subcommand's long help is reviewed and updated to remove self-references to the old flat path and to document the new inline field conventions (`description=`, `title=`, `@file`, `@-`) level=task order=1
  - [x] The `tusk task` parent command skeleton from the grouping initiative gets its full listing finalized here once every child command is in place level=task order=2
  - [x] Runs first in this initiative because help text is the source of truth that the documentation sweep quotes from level=task order=3
- [x] Story: Documentation sweep level=story order=2
  - [x] `README.md`, `PRODUCT.md`, `docs/configuration.md`, `docs/programmatic-usage.md`, and every file under `docs/releases/` and `docs/status/` updated to the new command syntax level=task order=1
  - [x] Historical release notes (v0.1 through v0.10) are left untouched — they describe what shipped at the time and should not be rewritten level=task order=2
  - [x] v0.11 release notes call out the full mapping table as a breaking change, the inline-field principle, the `--description` and `--uda` flag removals, the `@file` / `@-` convention on inline string fields, and the dotted `uda.key=value` convention on create/modify level=task order=3
  - [x] PRODUCT.md's "Inline Syntax" and CLI sections explicitly state the principle so agents and humans reading the product description see the one-way-to-set-a-field rule alongside the lexer description level=task order=4
- [x] Story: E2E test rewrite level=story order=3
  - [x] Every scenario in `tests/e2e/` updated to the new invocation paths and inline field conventions level=task order=1
  - [x] Harness step builders (if any hardcode command names) updated level=task order=2
  - [x] New scenarios covering the "unknown command" suggestion path for each removed flat verb, to lock in the hint table level=task order=3
  - [x] New scenarios covering `description=@file`, `description=@-`, and `title=@file` to lock in the file-loading helper behavior level=task order=4
  - [x] New scenarios covering `uda.key=value` on create, repeated `uda.*` fields in a single invocation, `uda.key=` clearing on modify, and the unknown-top-level-field rejection path for bare `key=value` that isn't a registered field level=task order=5
  - [x] Runs last in the milestone — a green test suite on the new surface is the exit gate for v0.11 level=task order=6
## v0.12 — Trailing Window Notes level=milestone order=12
> A persistent notebook system where players record learnings, context, and decisions — scoped by project and player, with a configurable trailing window that shows only the most recent entries to avoid context overload.

### Initiative: Note Entity & Storage level=initiative order=1
> Domain type, repository interface, and SQLite implementation for notes.

- [x] Story: Note domain and storage level=story order=1
  - [x] Define `Note` entity (`id` UUID, `project_id`, `player_id`, `task_id` nullable, `body`, `metadata` JSON, `archived_at` nullable, `created_at`) level=task order=1
  - [x] `NoteRepository` interface (`Create`, `Archive`, `GetByID`, `List`) level=task order=2
  - [x] Migration adding `notes` table with composite index on `(project_id, player_id, created_at DESC)` and partial index on `task_id` level=task order=3
  - [x] SQLite `NoteRepository` implementation with window-aware `List` query (`LIMIT` in SQL, not post-fetch) level=task order=4
### Initiative: Note Service level=initiative order=2
> Business logic for note creation, listing with trailing window, and archiving.

- [x] Story: NoteService level=story order=1
  - [x] `Create` — validate player exists, project exists, optional task exists and belongs to project, body non-empty level=task order=1
  - [x] `List` — resolve effective window size (CLI flag → player DB setting → project config → global config → default 20), apply `--since` filter, default to caller's notes only level=task order=2
  - [x] `Archive` — set `archived_at`, validate caller is author level=task order=3
- [x] Story: Window size resolution level=story order=2
  - [x] Add `note_window_size` nullable column to `players` table (migration) level=task order=1
  - [x] Add `[notes].window_size` to global config schema level=task order=2
  - [x] Add `note_window_size` to `ProjectSettings` JSON (projects moved to DB in v0.10 — per-project override lives on the DB row, not in config) level=task order=3
  - [x] Resolution chain: CLI flag → player DB → project settings → global config → hardcoded default (20) level=task order=4
### Initiative: Note CLI level=initiative order=3
> `tusk note` subcommand for writing, reading, and archiving notes.

- [x] Story: Note write commands level=story order=1
  - [x] `tusk note add "<body>" [project=<name>] [--task <short_id>] [meta.key=value...]` — create a note with optional task scope and metadata (metadata keys namespaced under `meta.`, symmetric with task `uda.`) level=task order=1
  - [x] `tusk note archive <note_id>` — archive a note level=task order=2
- [x] Story: Note read commands level=story order=2
  - [x] `tusk note list` — list own notes in current/default project, trailing window applied level=task order=1
  - [x] `tusk note list project=<name>` — specific project level=task order=2
  - [x] `tusk note list --all-players` — all players' notes level=task order=3
  - [x] `tusk note list --player <id>` — specific player's notes level=task order=4
  - [x] `tusk note list --task <short_id>` — task-scoped notes level=task order=5
  - [x] `tusk note list --window <N>` — override window size level=task order=6
  - [x] `tusk note list --since <duration>` — time-bounded filter (e.g., `7d`, `24h`) level=task order=7
  - [x] `tusk note list --archived` — include archived notes level=task order=8
  - [x] Markdown rendering via glamour in CLI output level=task order=9
- [x] Story: Player window size preference level=story order=3
  - [x] `tusk player modify <id> note-window-size=<N>` — set per-player window size level=task order=1
  - [x] Display `note_window_size` in player info output level=task order=2
### Initiative: Note MCP Tools level=initiative order=4
> Expose note operations to AI agents via MCP.

- [x] Story: Note MCP tools level=story order=1
  - [x] `tusk_note_add` — create note (project, player, optional task, body, metadata) level=task order=1
  - [x] `tusk_note_list` — list with window/since/player/task/archived filters level=task order=2
  - [x] `tusk_note_archive` — archive a note level=task order=3
### Initiative: MCP Field Restrictions level=initiative order=5
> Configurable field-level write restrictions for MCP tools — prevent agents from modifying sensitive player or system settings.

- [x] Story: MCP blocked fields level=story order=1
  - [x] Define `[mcp.blocked_fields]` config section mapping tool names to lists of restricted fields level=task order=1
  - [x] Enforce restrictions at the MCP layer before service calls level=task order=2
  - [x] Default blocked fields for player modification (e.g., `note_window_size`) level=task order=3
## v0.13 — Roadmap Self-Host level=milestone order=13
> Make tusk usable as the source of truth for its own roadmap. Replace the hand-edited `ROADMAP.md` with a tusk project, regenerate a human-readable markdown view from tusk state, and give agents the observability and schema tools they need to plan against it.
>
> The milestone combines the foundational capabilities the self-host use case depends on — the Event Log, Task Level Taxonomy, and bidirectional JSON Data Portability — with three capabilities that fall out of managing a roadmap inside tusk: sibling ordering, subtree urgency overrides, and a static progress rollup view. The human-readable markdown renderer used to regenerate `ROADMAP.md` lives under the ROADMAP.md Migration initiative as an export-only `tusk task tree --format markdown`. The milestone closes with a one-shot migration from the existing `ROADMAP.md` so it can be dogfooded before release.
>
> **Exit criteria:** `ROADMAP.md` is regenerated from tusk state (never hand-edited) and every status update flows through `tusk task done` or equivalent.
>

### Initiative: Event Log level=initiative order=1
> Append-only event table recording all mutations. Foundation for data portability (import/export need accurate event history), the live dashboard in v0.15, and undo in v0.16.

- [x] Story: Event log infrastructure level=story order=1
  - [x] Define event types (task_created, task_modified, status_changed, task_started, task_claimed, task_released, task_completed, task_deleted, task_popped, relation_added, relation_removed) level=task order=1
  - [x] Migration adding `events` table (id, event_type, entity_id, entity_kind, player_id, payload JSON, created_at) level=task order=2
  - [x] EventRepository interface and SQLite implementation level=task order=3
  - [x] Emit events from TaskService, RelationService on every mutation level=task order=4
  - [x] Bounded retention (configurable max events, prune on write) level=task order=5
### Initiative: Task Level Taxonomy level=initiative order=2
> First-class `level` field on every task plus a rank-ordered taxonomy declared at workspace scope with per-project override. Enforces the milestone → initiative → story → task/spike modeling used by the roadmap self-host and the Claude Code plugin skills. Replaces the earlier per-UDA-key schema plan with a narrower, purpose-built primitive; UDAs stay free-form key-value metadata.

- [x] Story: Domain model and resolution level=story order=1
  - [x] Add `level TEXT` nullable column to `tasks` via migration; update `domain.Task.Level *string` and the SQLite scan/write paths; existing rows default to `NULL` level=task order=1
  - [x] `domain.TaskUpdate.Level` uses `**string` so callers can distinguish "no change" from "clear" on modify level=task order=2
  - [x] `domain.Taxonomy` = ordered slice of rank groups (`[][]string`); rank index 0 is the top rank and the only root-eligible rank level=task order=3
  - [x] `domain.ProjectSettings.Taxonomy *domain.Taxonomy` carries the per-project override with three observable states: `nil` = inherit the workspace default, `&empty` = explicit opt-out (disable levels for this project even when a workspace default exists), `&populated` = full replace (no per-rank merge) level=task order=4
  - [x] `config.TaxonomyConfig` section in `tusk.toml` for the workspace default; embedded default ships empty level=task order=5
  - [x] Resolution chain: project override (non-nil, including explicit empty) → workspace default → empty; any empty effective taxonomy disables level validation for that project level=task order=6
  - [x] Taxonomy helpers on the domain type — `RankOf(level) (int, bool)`, `IsTopRank(level) bool`, `IsEmpty() bool`, `Contains(level) bool` level=task order=7
- [x] Story: Validator and enforcement level=story order=2
  - [x] `TaxonomyValidator.Check(ctx ValidationContext, task *domain.Task) error` — single entry point invoked from the task service on create and on any modify touching `Level`, `ParentID`, or `ProjectID` level=task order=1
  - [x] `ValidationContext` carries the parent task's resolved level (pre-loaded in the service layer) so the validator never touches the repository level=task order=2
  - [x] Rules: empty effective taxonomy accepts any state; otherwise `task.Level` must be declared in the taxonomy; tasks with no parent require top-rank (`rank == 0`); tasks with a parent require parent's rank strictly less than the task's rank level=task order=3
  - [x] Rejections return `domain.ErrTaxonomyViolation` wrapping a typed `TaxonomyError{Level, ParentLevel, Reason}` so CLI and MCP surfaces render structured messages level=task order=4
  - [x] Prospective semantics — taxonomy edits do not retroactively re-validate existing tasks; a later `tusk task level-check` surfaces violations without rejecting them level=task order=5
  - [x] Project reassignment re-runs validation against the destination project's effective taxonomy level=task order=6
- [x] Story: CRUD — CLI inline syntax level=story order=3
  - [x] `tusk task create` / `tusk task modify` accept `level=<name>`; `level=` (empty value on modify) clears the field level=task order=1
  - [x] `tusk project modify` accepts `taxonomy.levels=milestone:initiative:story:(task,spike)` — `:` separates ranks top-to-bottom, a parenthesized comma list marks peer levels sharing a rank level=task order=2
  - [x] `taxonomy.levels=` (empty value) clears the project override and falls back to the workspace default level=task order=3
  - [x] `taxonomy.disable=true` writes an explicit-empty override so the project opts out of the workspace default; `taxonomy.disable=false` clears it (equivalent to `taxonomy.levels=`). `disable=true` is mutually exclusive with `taxonomy.levels=...` in the same call level=task order=4
  - [x] `taxonomy=@./taxonomy.json` replaces the project taxonomy via the `@`-reference expander level=task order=5
  - [x] `tusk config set taxonomy.levels ...` writes the workspace default to the active `tusk.toml` level=task order=6
  - [x] `tusk project show` renders the effective taxonomy with a provenance marker (`source: workspace default` / `source: project override`) level=task order=7
  - [x] `tusk config show` renders the workspace default under `[taxonomy]` and each project's override read-only under its projects section level=task order=8
  - [x] Filter grammar: `level=<name>` and `level=a,b` become first-class filter fields; `uda.level` is no longer a reserved convention level=task order=9
- [x] Story: CRUD — MCP tool level=story order=4
  - [x] `tusk_task_create` / `tusk_task_modify` accept a `level` string parameter; empty string on modify clears the field level=task order=1
  - [x] Every task response (`tusk_task_get`, create/modify returns, list, tree) includes `level` level=task order=2
  - [x] `tusk_project_modify` accepts a structured `taxonomy` object mirroring the domain shape (`{"ranks": [["milestone"], ["initiative"], ["story"], ["task", "spike"]]}`); omitted = no change, `null` = clear the project override (inherit workspace default), `{"ranks": []}` = explicit-empty opt-out level=task order=3
  - [x] Version-based optimistic locking on project writes is unchanged; the v0.12 blocked-fields mechanism applies level=task order=4
### Initiative: Sibling Ordering level=initiative order=3
> Fractional `order` field for positioning tasks among siblings. Gives hierarchical views a meaningful document-position sort without coupling to urgency.

- [x] Story: Order field and sort policy level=story order=1
  - [x] Add `order` DOUBLE column to `tasks` (nullable) via migration level=task order=1
  - [x] `tusk task create` accepts `order=<float>` inline; default is `max(sibling.order) + 1` or `1.0` for the first child level=task order=2
  - [x] `tusk task modify <id> order=<float>` sets an absolute value through the inline field path level=task order=3
  - [x] Tree views (`tusk task tree`, `task list parent=...`, `task list tree=...`, children in `task get`) sort by `order ASC, created_at ASC` level=task order=4
  - [x] Flat views (`task list`, `next`, `available`, `pop`) continue to sort by urgency level=task order=5
  - [x] `--sort=order|urgency|created|priority|due` override available on list/tree level=task order=6
- [x] Story: `tusk task move` command level=story order=2
  - [x] `tusk task move <id> --before <target>` / `--after <target>` / `--first` / `--last` level=task order=1
  - [x] Computes a midpoint between neighbors (fractional index) and writes it level=task order=2
  - [x] Re-parents the task when `target` has a different parent (single atomic operation) level=task order=3
  - [x] `--resequence <parent>` rewrites a sibling group to evenly spaced integers when midpoints exhaust `float64` precision level=task order=4
  - [x] MCP tool `tusk_task_move` with the same semantics level=task order=5
### Initiative: Subtree Urgency Overrides level=initiative order=4
> Urgency weight overrides attachable to any task, inherited by descendants with key-level merge. Lets a single workspace host multiple priority zones — e.g., per-milestone boosts on a self-hosted roadmap — without requiring a project split.

- [x] Story: Override field and resolution level=story order=1
  - [x] Add `urgency_overrides` JSON column to `tasks` (nullable) level=task order=1
  - [x] Urgency engine resolution chain: global config → project settings → ancestor task overrides (root → self, merged) → self overrides level=task order=2
  - [x] Merge is per-key — unspecified keys inherit from the outer scope level=task order=3
  - [x] Re-parenting re-walks the ancestor chain on next compute; overrides on the moved task travel with it level=task order=4
- [x] Story: Override CLI and MCP surface level=story order=2
  - [x] `tusk task modify <id> urgency.<weight>=<float>` sets an override key level=task order=1
  - [x] `tusk task modify <id> urgency.<weight>=` (empty value) clears that key level=task order=2
  - [x] `tusk task modify <id> +urgency.<weight>=<delta>` / `-urgency.<weight>=<delta>` apply arithmetic deltas; when no task-level value exists, the delta applies relative to the resolved effective weight at that position in the chain level=task order=3
  - [x] `tusk task modify <id> urgency.clear=true` drops every task-level override in one call level=task order=4
  - [x] `tusk_task_modify` MCP tool accepts a structured `urgency_overrides` object; the v0.12 blocked-fields mechanism applies unchanged level=task order=5
- [x] Story: Visibility level=story order=3
  - [x] `tusk task get` renders `urgency_overrides` (self) and an `effective_urgency_weights` block (resolved chain) level=task order=1
  - [x] `tusk config show` unchanged — task-level overrides are task data, not config level=task order=2
### Initiative: Progress Rollup level=initiative order=5
> Static CLI summary views for per-subtree completion tracking. Live dashboard rollup is deferred to v0.15, where the event log can drive real-time updates without re-querying.

- [x] Story: Rollup on tree view level=story order=1
  - [x] `tusk task tree --rollup` — branch nodes render with `[done/total done, %]` and `(status: count, ...)` breakdown; leaf nodes unchanged level=task order=1
  - [x] Counters include all descendants at any depth — no WBS vocabulary baked in level=task order=2
  - [x] `%done` = `count(descendants with status having done role) / count(descendants with status not having delete role)` — leverages the status roles shipped in v0.9 so custom workflows work without extra configuration level=task order=3
- [x] Story: `tusk task summary` command level=story order=2
  - [x] `tusk task summary <id>` — single-subtree block: title, status, `%done`, counts by status level=task order=1
  - [x] `tusk task summary` (no id) — workspace-wide rollup: one block per root task plus a totals line level=task order=2
  - [x] Accepts the same filter grammar as `task list` so scoped rollups (`tusk task summary level=story`) work without the feature itself knowing what `level` means level=task order=3
  - [x] `--output json` variant for agent consumption level=task order=4
  - [x] MCP tool `tusk_task_summary` mirrors the CLI level=task order=5
### Initiative: Data Portability level=initiative order=6
> Bidirectional JSON import and export. Covers backup, migration, and the v0.13 ROADMAP self-host. Markdown rendering lives under the ROADMAP.md Migration initiative; CSV is deferred (see blockquote below).

- [x] Story: JSON export and import level=story order=1
  - [x] `tusk export [--output <path>]` writes a full workspace dump (workflows, projects, players, tags, tasks, relations, annotations, notes, events) to stdout by default; `--output <path>` writes atomically via `<path>.tmp` + rename level=task order=1
  - [x] `tusk import --input <path>` rehydrates the workspace; `--input -` reads from stdin (TTY-guarded) level=task order=2
  - [x] `--replace` overwrites collisions row-by-row; default is fail-on-collision level=task order=3
  - [x] `--replace --truncate` wipes every entity table before applying the dump (wipe-and-restore mode); `--truncate` requires `--replace` level=task order=4
  - [x] `--dry-run` runs the validation pass and reports counts without writing level=task order=5
  - [x] Faithful semantics: IDs, timestamps, and version numbers preserved exactly; per-entity events are not emitted — one `workspace_imported` envelope event records the import level=task order=6
  - [x] Pre-validation pass collects every issue (schema, FK, taxonomy, blocks-cycle, workflow well-formedness, collision) before any write so callers see the full picture in one round-trip level=task order=7
  - [x] Apply pass runs in a single SQLite transaction so a failed import leaves no partial state level=task order=8
  - [x] Envelope carries `schema_version: 1` + `tusk_version`; unknown `schema_version` is rejected with a structured error naming both the dump's value and the supported value level=task order=9
  - [x] `domain.TaxonomyValidator` runs on every task; level violations reject the offending row with a `TaxonomyError`, matching the CLI and MCP enforcement paths level=task order=10
  - [x] Sibling `order` serializes as a JSON number or `null`; import preserves exact values, treats `null` / missing key as "no opinion" (service auto-assigns) level=task order=11
  - [x] **No `--format` flag** — JSON is the only format level=task order=12
- [x] Story: PortabilityService and codec package level=story order=2
  - [x] `internal/portability/` package owns the JSON codec over a neutral `PortableWorkspace` value with no service-layer dependencies level=task order=1
  - [x] `service.PortabilityService` orchestrates Export and Import; `Export` reads through the existing per-entity services and `Import` applies a dump inside a single `WriteTx` level=task order=2
  - [x] `service.WriteTx` extended to surface every entity-kind accessor (`Workflows`, `Projects`, `Players`, `Tags`, `Tasks`, `Relations`, `Annotations`, `Notes`, `Events`) plus `TruncateAll` level=task order=3
  - [x] `Client.Portability` exposes the same API to library consumers as the CLI uses level=task order=4
  - [x] `domain.EventWorkspaceImported` event type + `domain.EntityWorkspace` entity-kind constant land in `domain/`; the existing event-log retention prunes them like any other event level=task order=5
### Initiative: ROADMAP.md Migration level=initiative order=7
> One-shot bootstrap that moves the existing `ROADMAP.md` into a tusk workspace, so the milestone can be dogfooded before close. Script is throwaway — it lives in-repo only for the duration of v0.13.

- [x] Story: Markdown rendering (export-only) level=story order=1
  - [x] `tusk task tree --format markdown` extends the existing tree renderer; no top-level `tusk render` command and no markdown import path level=task order=1
  - [x] Dialect: H1 per project, H2 per root task, nested bullets for descendants, `[x]` for done-role statuses, inline `level=`, `priority=`, `due=`, `order=`, `uda.*=` tokens, trailing `+tag` level=task order=2
  - [x] `status=<name>` token emitted only for non-binary states (anything other than the initial pending and the done role); binary statuses use the `[x]` / ` ` checkbox alone level=task order=3
  - [x] Annotations and notes render as labeled child lists under their parent task level=task order=4
  - [x] `urgency_overrides`, `recurrence_rule`, `claimed_by` / `claimed_at`, and any future attachment fields are silently dropped — round-trip lives exclusively under the JSON portability codec level=task order=5
- [x] Story: Cutover level=story order=3
  - [x] `ROADMAP.md` is regenerated from `tusk task tree --format markdown` and replaces the hand-edited file level=task order=1
  - [x] Contributor docs updated to point at `tusk task` commands for roadmap edits instead of direct markdown edits level=task order=2
  - [x] Migration script removed from the repo once cutover is stable level=task order=3
### Initiative: Repo-Root Tusk Workspace level=initiative order=8
> Make the tusk repo root resolve as its own tusk workspace via a committed `tusk.toml`, so any `tusk` subcommand run from anywhere inside the checkout (and CI) automatically uses the committed `.data/tusk.db` — no `TUSK_DB` env, no `--config` flag. Blocked by an e2e harness gap: tests run with CWD inside the repo and walk-up config discovery (v0.9) finds the workspace `tusk.toml` as their active file. Tests that exercise `tusk config init --local` / `tusk config set` then write into the committed file, polluting it across the rest of the suite. Captured during the v0.13 ROADMAP.md cutover (see `docs/retrospectives/v0.13-roadmap-migration.md`) — the workspace config was prepared, broke e2e, and reverted on the cutover PR.

- [x] Story: Hermetic e2e harness against walk-up config level=story order=1
  - [x] `tests/e2e/harness.go`: `Env.Run` sets `TUSK_CONFIG` to a per-test temp file (or chdirs to a temp dir before `exec.Command`) so tusk's walk-up resolver never reaches the repo-root `tusk.toml`. Match the existing `TUSK_CONFIG_DIR` injection pattern. level=task order=1
  - [x] Regression test: create a `tusk.toml` in a parent dir during a test, run a tusk subcommand, assert the test's expected DB/config wins (proves walk-up isolation). level=task order=2
  - [x] Re-run the full e2e suite from a CWD inside the repo with a sentinel `tusk.toml` at the repo root containing a `[taxonomy]` section; assert no tests fail. level=task order=3
  - [x] Document the isolation pattern in the harness file's package comment so future tests don't reintroduce walk-up coupling. level=task order=4
- [x] Story: Commit repo-root tusk.toml and drop TUSK_DB plumbing level=story order=2
  - [x] Add `tusk.toml` at repo root with `[storage] path = ".data/tusk.db"` and a comment pointing readers at v0.9 walk-up discovery. level=task order=1
  - [x] Drop `TUSK_DB: ${{ github.workspace }}/.data/tusk.db` from `.github/workflows/ci.yml`'s `roadmap-drift` step — walk-up handles it. level=task order=2
  - [x] Drop the `export TUSK_DB="$(pwd)/.data/tusk.db"` step from the contributor workflow in `CONTRIBUTING.md`; from inside the repo, `tusk task ...` and `make roadmap` work with no env setup. level=task order=3
  - [x] Resolve the v0.13 retrospective's "Follow-up: e2e harness is not hermetic against walk-up config" section by linking to the merged PR. level=task order=4
  - [x] Verify drift check still passes end-to-end: locally and on CI, `make roadmap && git diff --exit-code ROADMAP.md` succeeds with no `TUSK_DB` set. level=task order=5
## v0.14 — Tusk Claude Code Plugin level=milestone order=14
> Ship an official Claude Code plugin that accelerates the human-agent loop for roadmap work and day-to-day task triage on top of tusk. Vanilla `tusk` remains fully supported — the plugin is an optional layer for users who want an agentic loop.

### Initiative: Plugin Scaffolding level=initiative order=1
> Repo layout, marketplace manifest, plugin manifest, CI validation.

- [ ] Story: Repo layout level=story order=1
  - [ ] `plugin/` subtree with `plugin/.claude-plugin/plugin.json` manifest (plugin name `tusk`, version mirrors tusk minor) level=task order=1
  - [ ] Top-level `.claude-plugin/marketplace.json` with a single plugin entry pointing at `./plugin` level=task order=2
  - [ ] `plugin/.mcp.json` declaring the tusk MCP server; command targets the launcher, no `TUSK_DB` set so tusk's default applies level=task order=3
- [ ] Story: CI validation level=story order=2
  - [ ] GitHub Actions job runs `claude plugin validate plugin/` on every PR that touches `plugin/` level=task order=1
  - [ ] Plugin release tag gated on manifest validity level=task order=2
### Initiative: Binary Launcher level=initiative order=2
> Portable launcher that downloads the pinned tusk binary from GitHub Releases on first use, verifies via SHA256, and caches in `${CLAUDE_PLUGIN_DATA}`. No bundled binaries — plugin package stays small.

- [ ] Story: Platform-aware launcher scripts level=story order=1
  - [ ] `plugin/bin/tusk-launcher` (POSIX) and `plugin/bin/tusk-launcher.cmd` (Windows) with parallel logic level=task order=1
  - [ ] Platform detection via `uname` / `%PROCESSOR_ARCHITECTURE%` level=task order=2
  - [ ] Exec cached binary if present; otherwise download level=task order=3
- [ ] Story: Download and verification level=story order=2
  - [ ] Fetch from `https://github.com/<org>/tusk/releases/download/v<version>/tusk-<os>-<arch>` level=task order=1
  - [ ] SHA256 check against `plugin/bin/checksums.json` (regenerated per release) level=task order=2
  - [ ] Install to `${CLAUDE_PLUGIN_DATA}/bin/tusk-<version>-<os>-<arch>`, mark executable level=task order=3
  - [ ] Actionable error messages on network or checksum failure level=task order=4
- [ ] Story: Escape hatch and version check level=story order=3
  - [ ] `TUSK_MCP_BINARY` env var skips download and execs the provided path (for dev and corporate mirrors) level=task order=1
  - [ ] Launcher warns (never blocks) if `tusk version` disagrees with the plugin's pinned version level=task order=2
- [ ] Story: Launcher tests level=story order=4
  - [ ] Shell test harness (bats-style) against a local HTTP fixture level=task order=1
  - [ ] Coverage: platform detection, successful install, checksum rejection, override path level=task order=2
### Initiative: MCP Wiring and Install Flow level=initiative order=3
> End-to-end install: plugin loads, MCP server spawns, tasks created through a skill land in the shared tusk DB.

- [ ] Story: Shared-DB default level=story order=1
  - [ ] Plugin `.mcp.json` leaves `TUSK_DB` unset so tusk falls through to its default `~/.local/share/tusk/tusk.db` level=task order=1
  - [ ] Project-level `.mcp.json` opt-out pattern documented in the plugin README level=task order=2
- [ ] Story: Integration smoke test level=story order=2
  - [ ] Pre-release checklist: `claude --plugin-dir ./plugin` → `tusk:init` → `tusk:plan` → verify tasks land in a fresh DB level=task order=1
  - [ ] Documented in `RELEASE.md` level=task order=2
### Initiative: Tier A — Tusk-Native Skills level=initiative order=4
> One-time setup plus the roadmap/task-shape workflow skills. All skills use only documented v0.13 tusk MCP tools.

- [ ] Story: `tusk:init` level=story order=1
  - [ ] Detect CLAUDE.md / AGENTS.md / GEMINI.md at repo root; ask which file(s) to update or offer to create CLAUDE.md level=task order=1
  - [ ] Ask for the alignment doc path; accept paths that don't exist yet level=task order=2
  - [ ] Write the `## Tusk alignment` block idempotently — update in place if present, never duplicate level=task order=3
  - [ ] Offer to bootstrap the level taxonomy (milestone/initiative/story/task/spike) on the active tusk project level=task order=4
- [ ] Story: `tusk:plan` level=story order=2
  - [ ] Read the alignment doc via the CLAUDE.md convention; prompt for intent if absent level=task order=1
  - [ ] Guided brainstorm → WBS draft → user review → `tusk import --format json` for atomic bulk creation level=task order=2
  - [ ] Produces one milestone subtree per invocation level=task order=3
- [ ] Story: `tusk:decompose` level=story order=3
  - [ ] Input: task short_id level=task order=1
  - [ ] Walks the user through splitting the task; creates children with level-correct UDA values respecting v0.13 parent-level pairing level=task order=2
- [ ] Story: `tusk:pick-next` level=story order=4
  - [ ] Reads urgency, sibling order, blocker state, rollup health level=task order=1
  - [ ] Recommends one task with explicit reasoning; user accepts or overrides level=task order=2
  - [ ] Advisory only — never mutates level=task order=3
- [ ] Story: `tusk:report` level=story order=5
  - [ ] Logs progress as a note on the active task level=task order=1
  - [ ] Transitions status on confirmation; shows the impact on parent rollup level=task order=2
- [ ] Story: `tusk:review` level=story order=6
  - [ ] Reads the full roadmap rollup level=task order=1
  - [ ] Surfaces at-risk subtrees (low velocity, urgency escalation, stale in-progress) level=task order=2
  - [ ] Suggests reprioritizations; never mutates without confirmation level=task order=3
### Initiative: Tier B — Engineering Discipline Skills level=initiative order=5
> Opinionated workflow skills that counter common agentic-coding failure modes — missing clarifications, overcomplicated designs, skipped tests. Artifacts land as tusk notes and child tasks rather than loose markdown files.

- [ ] Story: `tusk:brainstorm` level=story order=1
  - [ ] Clarifying questions one at a time level=task order=1
  - [ ] Propose 2-3 approaches with tradeoffs level=task order=2
  - [ ] Hard gate: refuses to produce a design until questions are answered level=task order=3
  - [ ] Output: note on the active task level=task order=4
- [ ] Story: `tusk:design` level=story order=2
  - [ ] Turn a brainstorm into a design with named components, interfaces, failure modes, testing strategy level=task order=1
  - [ ] Hard gate: refuses to move to implementation until the user approves level=task order=2
  - [ ] Output: a second note on the task, cross-referenced with the brainstorm level=task order=3
- [ ] Story: `tusk:plan-implementation` level=story order=3
  - [ ] Turn a design into a phased plan as a child-task subtree level=task order=1
  - [ ] Each phase is a child task with a "definition of done" in its description level=task order=2
  - [ ] Hard gate: refuses to write code until the subtree is approved level=task order=3
  - [ ] Rollup tracks plan progress automatically level=task order=4
- [ ] Story: `tusk:tdd` level=story order=4
  - [ ] Requires a failing test before implementation level=task order=1
  - [ ] Runs the suite iteratively, logs each iteration as a note on the active task level=task order=2
  - [ ] Hard gate: refuses to write implementation without a red test level=task order=3
### Initiative: Release Pipeline level=initiative order=6
> Extend tusk's release workflow to produce a matching plugin release with regenerated checksums.

- [ ] Story: Checksum regeneration job level=story order=1
  - [ ] CI step after tusk binary publish: download release SHA256s, write `plugin/bin/checksums.json` level=task order=1
  - [ ] Commits and pushes the checksum update level=task order=2
  - [ ] Plugin version bump handled in the same PR when needed level=task order=3
- [ ] Story: Version pin and compatibility matrix level=story order=2
  - [ ] Plugin minor mirrors tusk minor (v0.14.x plugin → v0.14.x tusk) level=task order=1
  - [ ] Plugin patches can ship independently for skill or launcher fixes level=task order=2
  - [ ] Compatibility matrix documented in the plugin README (plugin v0.14.x requires tusk ≥ v0.13.0) level=task order=3
### Initiative: Documentation level=initiative order=7
> Plugin install guide, skill catalog, troubleshooting.

- [ ] Story: Plugin README level=story order=1
  - [ ] Install via `/plugin marketplace add <org>/tusk` then `/plugin install tusk@tusk` level=task order=1
  - [ ] Skill catalog with one-paragraph summaries level=task order=2
  - [ ] Compatibility matrix level=task order=3
- [ ] Story: Troubleshooting section level=story order=2
  - [ ] Offline install via `TUSK_MCP_BINARY` level=task order=1
  - [ ] Corporate proxy / mirror setup level=task order=2
  - [ ] DB isolation pattern (project-level `.mcp.json` with `TUSK_DB`) level=task order=3
### Initiative: Event Log Hardening level=initiative order=8
> Quality follow-ups surfaced during the v0.13 Event Log post-implementation review. Non-blocking — the event log ships correctly in v0.13 — but worth resolving before downstream consumers (Data Portability, Live Dashboard, undo) grow to depend on the current shape.

- [ ] Story: Lifecycle emission coherence level=story order=1
  - [ ] Normalize idempotent-call behavior across `Start`, `Complete`, `Delete`: either all three emit their action event when called on a task already in the terminal status, or none do. Current state: `Start` always emits; `Complete`/`Delete` silently skip when `oldStatus == targetStatus`. level=task order=1
  - [ ] Pick the emitting direction (preferred) so downstream consumers can trust "one action call = one event row"; update `service/task.go` and the corresponding event tests. level=task order=2
- [ ] Story: `TaskCreatedPayload` completeness level=story order=2
  - [ ] Populate `Order` and `Tags` in `domain.NewTaskCreatedEvent`, or drop the fields from the payload struct. Current state: fields are declared but the constructor never sets them because `domain.Task` carries neither. level=task order=1
  - [ ] If populating: pull tags via `TagRepo.GetTaskTags` at emit time (inside the same `WriteTx` so the snapshot is consistent) and add `Order` once v0.13 Sibling Ordering lands the field on `Task`. level=task order=2
  - [ ] Update the Data Portability JSON export to round-trip whichever shape is chosen. level=task order=3
- [ ] Story: `EventPayload` seal level=story order=3
  - [ ] Rename `EventPayload.EventKind()` to unexported `eventKind()` per the original design-spec intent, or document the exported form as deliberate so external packages can register new payload kinds without forking `domain/`. level=task order=1
  - [ ] If tightening the seal: move the `UnknownPayload` fallback path (currently in `sqlite/event.go`'s `decodePayload`) to a registry hook so downstream consumers can still round-trip unknown kinds. level=task order=2
- [ ] Story: Cleanup level=story order=4
  - [ ] Remove the dead `_ = bundle` line in `service/task.go` (`Start`). level=task order=1
  - [ ] Audit other event-log touch points for similar refactor leftovers. level=task order=2
### Initiative: Project Note Window Size Wiring level=initiative order=9
> v0.12 added `NoteWindowSize` to `domain.ProjectSettings` and `NoteService.List` reads it in the resolution chain, but the field has no CLI or MCP write path — `ModifyProjectInput`, `internal/tui/project_parse.go`, and `internal/mcp/project_handlers.go` all omit it. Projects cannot currently override the note window; the fallback passes straight through to global config. Non-blocking orphan surfaced during v0.13 Task Level Taxonomy design review.

- [ ] Story: Project-level window size write path level=story order=1
  - [ ] Add `NoteWindowSize` to `ModifyProjectInput` in `service/project.go` and apply it in `ProjectService.Modify` level=task order=1
  - [ ] Add inline parser case in `internal/tui/project_parse.go` for `note-window-size=<N>` and `note-window-size=` (clear) level=task order=2
  - [ ] Add the parameter to `tusk_project_modify` in `internal/mcp/project_handlers.go` with version-based optimistic locking level=task order=3
  - [ ] Render the resolved value in `tusk project info` output level=task order=4
  - [ ] E2E coverage: set per-project override, list notes, verify the resolution chain picks up the project value over global config level=task order=5
### Initiative: Sibling Ordering Hardening level=initiative order=10
> Quality follow-ups surfaced during the v0.13 Sibling Ordering post-implementation review. Non-blocking — the feature ships correctly in v0.13 — but worth resolving before Data Portability's import path depends on null-order round-trip and before MCP `tusk_task_tree` grows a sort option.

- [ ] Story: Preserve explicit `order=null` in JSON output level=story order=1
  - [ ] Drop `,omitempty` from the `Order *float64` field in `taskJSON` (`internal/tui/render.go`) and `treeNodeJSON` (`internal/tui/tree.go`). Spec §3.1 requires `null` to be serialized as `"order": null`, not omitted, because a cleared order is distinct from an inherited default. level=task order=1
  - [ ] Current state: after `tusk task modify <id> order=` (clear), JSON output omits the key entirely. Any consumer distinguishing "absent" from "null" — including the upcoming Data Portability import path — will mishandle cleared rows. level=task order=2
  - [ ] Add a regression test in `internal/tui/render_test.go` / `tree_test.go` that a cleared task emits `"order": null`. level=task order=3
- [ ] Story: `ErrCyclicParent` message alignment level=story order=2
  - [ ] Cosmetic: `domain/errors.go` reads `"parent would create a cycle in task hierarchy"`; spec §3.2 specifies `"task move would create a parent cycle"`. The sentinel still matches via `errors.Is`, and the E2E substring-on-"cycle" check still passes, so this is spec-fidelity only. level=task order=1
### Initiative: Subtree Urgency Overrides Hardening level=initiative order=11
> Verification follow-ups surfaced during the v0.13 Subtree Urgency Overrides design review. Edge-case behaviors that the core spec decides but that need explicit regression coverage so future refactors don't silently change them, plus the MCP-side scoring parity gap that was originally misfiled under Sibling Ordering Hardening.

- [ ] Story: MCP `tusk_task_tree` subtree urgency parity level=story order=1
  - [ ] `internal/mcp/tools.go` `handleTaskTree` subtree branch still calls `taskSvc.GetDescendants` raw, returning tasks with zero `Urgency`. The CLI equivalent was fixed in commit `d2bae4f` to route subtree through `TaskService.List` with a `RootID` filter so `UrgencyEngine.ScoreAndSort` runs. level=task order=1
  - [ ] Apply the same fix to the MCP handler so subtree responses carry populated urgency scores. Doubly important once Subtree Urgency Overrides ships: subtree responses must reflect the resolved per-task weights, not zero. level=task order=2
- [ ] Story: Deleted/terminal ancestor override propagation level=story order=2
  - [ ] Add an E2E scenario that places `urgency_overrides` on a parent, transitions the parent to a terminal status (e.g., `completed` or `deleted`), and verifies the surviving children still resolve the parent's overrides into their effective weights. level=task order=1
  - [ ] Locks in the spec decision that ancestor walk does not filter by status — preventing a future "skip terminal ancestors" optimization from silently changing inheritance. level=task order=2
- [ ] Story: Override re-walk on `task move` level=story order=3
  - [ ] Add an E2E scenario that creates two subtrees with different ancestor overrides, moves a task between them, and verifies its effective weights flip to match the new chain on the next read with no explicit invalidation step. level=task order=1
  - [ ] Locks in the spec decision that overrides are stored on the task and re-resolved per read; prevents a future cache that wouldn't invalidate on `move`. level=task order=2
- [ ] Story: Cross-project ancestry assertion level=story order=4
  - [ ] Add a unit assertion in `service/task.go::buildEffectiveWeights` (or equivalent) that every visited ancestor shares the input task's `project_id`. If the invariant ever breaks (e.g., a future feature lets subtrees span projects), the resolution chain would silently mix project-level weights from two sources — fail loud instead. level=task order=1
  - [ ] Pair with an E2E or service-level test that confirms the assertion holds in normal operation and fires if the invariant is bypassed. level=task order=2
## v0.15 — Live Dashboard level=milestone order=15
> Real-time TUI dashboard for monitoring task state and player activity, powered by the event log shipped in v0.13.

### Initiative: TUI Dashboard level=initiative order=1
> Bubbletea-based live dashboard for orchestrator situational awareness.

- [ ] Story: Task board view level=story order=1
  - [ ] `tusk dashboard` — long-running TUI command level=task order=1
  - [ ] Tasks organized by status columns (kanban-style) level=task order=2
  - [ ] Live updates via event log polling (1-2 second interval) level=task order=3
  - [ ] Color-coded by priority and claim status (bubbletea owns its own styling, independent of v0.6 TUI Polish) level=task order=4
- [ ] Story: Player activity feed level=story order=2
  - [ ] Activity stream panel showing recent events ("agent-1 claimed X", "german completed Y") level=task order=1
  - [ ] Filter by player or event type level=task order=2
  - [ ] Highlight stuck/idle players (claimed but no activity for configurable duration) level=task order=3
- [ ] Story: Dashboard layout level=story order=3
  - [ ] Split view: task board + activity feed level=task order=1
  - [ ] Keyboard navigation between panels level=task order=2
  - [ ] Configurable via `[dashboard]` config section (refresh interval, layout, visible columns) level=task order=3
- [ ] Story: Live rollup panel level=story order=4
  - [ ] Extends the static progress rollup from v0.13 into a live dashboard panel driven by event log deltas (no per-tick re-query) level=task order=1
  - [ ] Per-root-task `%done` and status breakdown, refreshed as events arrive level=task order=2
## v0.16 — Advanced Features level=milestone order=16
> Recurrence, additional transports, and undo.

### Initiative: Recurrence level=initiative order=1
> Automatic task instance generation from RFC 5545 RRULE.

- [ ] Story: RRULE support level=story order=1
  - [ ] Parse RFC 5545 RRULE strings level=task order=1
  - [ ] Generate next instance on task completion level=task order=2
  - [ ] Handle recurrence edge cases (end date, count limit) level=task order=3
### Initiative: MCP Streamable HTTP Transport level=initiative order=2
> Network-accessible MCP server for multi-client scenarios. Targets Streamable HTTP (successor to deprecated SSE transport).

- [ ] Story: Streamable HTTP transport level=story order=1
  - [ ] Streamable HTTP transport implementation level=task order=1
  - [ ] `tusk mcp serve --transport http --port <port>` level=task order=2
### Initiative: Undo level=initiative order=3
> Revert the last mutation using the event log from v0.13.

- [ ] Story: Undo command level=story order=1
  - [ ] `tusk undo` — revert last mutation by reading event log and applying inverse level=task order=1
  - [ ] Support undo for task CRUD, status transitions, and claim operations level=task order=2
### Initiative: PostgreSQL Backend level=initiative order=4
- [ ] Story: PostgreSQL infrastructure level=story order=1
  - [ ] Connection pooling, migration support, and test harness level=task order=1
  - [ ] PostgreSQL migration files mirroring SQLite schema level=task order=2
- [ ] Story: Core PostgreSQL repositories level=story order=2
  - [ ] TaskRepository for PostgreSQL level=task order=1
- [ ] Story: Supporting PostgreSQL repositories level=story order=3
  - [ ] TagRepository for PostgreSQL level=task order=1
  - [ ] RelationRepository for PostgreSQL level=task order=2
  - [ ] AnnotationRepository for PostgreSQL level=task order=3
### Initiative: Interactive TUI level=initiative order=5
- [ ] Story: Interactive task management level=story order=1
  - [ ] Extend dashboard with inline task editing and status transitions level=task order=1
  - [ ] Task creation and modification without leaving the TUI level=task order=2
### Initiative: REST API level=initiative order=6
- [ ] Story: HTTP REST API level=story order=1
  - [ ] RESTful endpoints mirroring CLI/MCP capabilities level=task order=1
  - [ ] Authentication and authorization level=task order=2
### Initiative: Integrations & Extensions level=initiative order=7
- [ ] Story: Webhook notifications level=story order=1
  - [ ] Fire webhooks on task state changes (powered by event log from v0.13) level=task order=1
- [ ] Story: Time tracking level=story order=2
  - [ ] Start/stop timer on tasks level=task order=1
  - [ ] Report time spent level=task order=2
### Initiative: Binary Attachments level=initiative order=8
- [ ] Story: File attachments level=story order=1
  - [ ] Attach binary files to tasks (stored on filesystem, referenced in DB) level=task order=1
  - [ ] `tusk attach <id> <file>` / `tusk_task_attach` MCP tool level=task order=2
  - [ ] List and retrieve attachments via CLI and MCP level=task order=3
### Initiative: Bidirectional Sync level=initiative order=9
- [ ] Story: Sync protocol level=story order=1
  - [ ] Define sync format and conflict resolution strategy level=task order=1
  - [ ] `tusk sync export` / `tusk sync import` with merge semantics level=task order=2
### Initiative: Teams level=initiative order=10
> Introduce teams as a first-class entity so workflows, milestone assignments, and urgency scoping can vary by team within one workspace. Workflows are per-project by design — a single workspace sharing one workflow and one project-level urgency default cannot express teams with divergent practices. Teams provide that scope without forcing a project-per-team split.

- [ ] Story: Team entity and membership level=story order=1
  - [ ] Domain type, repository, migration level=task order=1
  - [ ] Players can belong to one or more teams level=task order=2
  - [ ] Tasks carry an optional team reference level=task order=3
- [ ] Story: Team-scoped workflows level=story order=2
  - [ ] A team can declare its own workflow independent of the project's default level=task order=1
  - [ ] Task status transitions validate against the team's workflow when a team is set level=task order=2
- [ ] Story: Team-scoped urgency level=story order=3
  - [ ] Per-team urgency weight overrides, slotting into the resolution chain between project and task-subtree overrides level=task order=1
  - [ ] Resolution: global → project → team → ancestor tasks → self level=task order=2
### Initiative: Cross-Team Alignment level=initiative order=11
> Coordinate parallel teams against a shared alignment source. Teams each keep their own workspace with independent workflows and urgency (per the Teams initiative), but share a higher-level product doc that defines common milestones and success criteria. This initiative adds tooling to verify conformance and surface cross-team rollups, turning tusk into a source of clarity across teams without coupling their day-to-day workflows.

- [ ] Story: Shared milestone identity level=story order=1
  - [ ] Stable cross-workspace keys for milestones so multiple teams can reference "the same milestone" independently of UUIDs level=task order=1
  - [ ] Import/export carries the alignment identity so teams importing the same milestone recognize it as shared level=task order=2
- [ ] Story: Alignment-doc conformance check level=story order=2
  - [ ] `tusk align check` compares a team workspace's milestones against a configured alignment source and reports missing, extra, or mismatched entries level=task order=1
  - [ ] Read-only — never mutates the workspace automatically; surfaces a diff for the user to act on level=task order=2
  - [ ] MCP tool exposure for agent-driven conformance checks level=task order=3
- [ ] Story: Cross-team rollup level=story order=3
  - [ ] Aggregate progress rollup across multiple team workspaces by shared milestone reference level=task order=1
  - [ ] `tusk align status <milestone>` lists which teams own which portions and their current rollup level=task order=2
### Initiative: Urgency Profiles level=initiative order=12
> Named bundles of urgency weight overrides, attachable to any task or team. Follow-up to the subtree urgency overrides shipped in v0.13: once projects start repeating the same override combinations ("ship-critical", "research", "maintenance"), profiles replace the copy-paste. Also covers customizable rollup formulas if that scope follows.

- [ ] Story: Profile entity level=story order=1
  - [ ] Named profile with a weight map, stored per workspace level=task order=1
  - [ ] CRUD via `tusk urgency-profile create/modify/delete/list` level=task order=2
- [ ] Story: Profile attachment level=story order=2
  - [ ] Tasks reference a profile by name; resolution slots the profile's weights into the existing chain at the task-subtree layer level=task order=1
  - [ ] Inline profile overrides still allowed; profile provides defaults, task-local overrides win per key level=task order=2
## Future level=milestone order=17
> Scale to multi-user, richer interfaces, and deeper integrations.

### Initiative: PostgreSQL Backend level=initiative order=1
- [ ] Story: PostgreSQL infrastructure level=story order=1
  - [ ] Connection pooling, migration support, and test harness level=task order=1
  - [ ] PostgreSQL migration files mirroring SQLite schema level=task order=2
- [ ] Story: Core PostgreSQL repositories level=story order=2
  - [ ] TaskRepository for PostgreSQL level=task order=1
- [ ] Story: Supporting PostgreSQL repositories level=story order=3
  - [ ] TagRepository for PostgreSQL level=task order=1
  - [ ] RelationRepository for PostgreSQL level=task order=2
  - [ ] AnnotationRepository for PostgreSQL level=task order=3
### Initiative: Interactive TUI level=initiative order=2
- [ ] Story: Interactive task management level=story order=1
  - [ ] Extend dashboard with inline task editing and status transitions level=task order=1
  - [ ] Task creation and modification without leaving the TUI level=task order=2
### Initiative: REST API level=initiative order=3
- [ ] Story: HTTP REST API level=story order=1
  - [ ] RESTful endpoints mirroring CLI/MCP capabilities level=task order=1
  - [ ] Authentication and authorization level=task order=2
### Initiative: Integrations & Extensions level=initiative order=4
- [ ] Story: Webhook notifications level=story order=1
  - [ ] Fire webhooks on task state changes (powered by event log from v0.13) level=task order=1
- [ ] Story: Time tracking level=story order=2
  - [ ] Start/stop timer on tasks level=task order=1
  - [ ] Report time spent level=task order=2
### Initiative: Binary Attachments level=initiative order=5
- [ ] Story: File attachments level=story order=1
  - [ ] Attach binary files to tasks (stored on filesystem, referenced in DB) level=task order=1
  - [ ] `tusk attach <id> <file>` / `tusk_task_attach` MCP tool level=task order=2
  - [ ] List and retrieve attachments via CLI and MCP level=task order=3
### Initiative: Bidirectional Sync level=initiative order=6
- [ ] Story: Sync protocol level=story order=1
  - [ ] Define sync format and conflict resolution strategy level=task order=1
  - [ ] `tusk sync export` / `tusk sync import` with merge semantics level=task order=2
### Initiative: Teams level=initiative order=7
> Introduce teams as a first-class entity so workflows, milestone assignments, and urgency scoping can vary by team within one workspace. Workflows are per-project by design — a single workspace sharing one workflow and one project-level urgency default cannot express teams with divergent practices. Teams provide that scope without forcing a project-per-team split.

- [ ] Story: Team entity and membership level=story order=1
  - [ ] Domain type, repository, migration level=task order=1
  - [ ] Players can belong to one or more teams level=task order=2
  - [ ] Tasks carry an optional team reference level=task order=3
- [ ] Story: Team-scoped workflows level=story order=2
  - [ ] A team can declare its own workflow independent of the project's default level=task order=1
  - [ ] Task status transitions validate against the team's workflow when a team is set level=task order=2
- [ ] Story: Team-scoped urgency level=story order=3
  - [ ] Per-team urgency weight overrides, slotting into the resolution chain between project and task-subtree overrides level=task order=1
  - [ ] Resolution: global → project → team → ancestor tasks → self level=task order=2
### Initiative: Cross-Team Alignment level=initiative order=8
> Coordinate parallel teams against a shared alignment source. Teams each keep their own workspace with independent workflows and urgency (per the Teams initiative), but share a higher-level product doc that defines common milestones and success criteria. This initiative adds tooling to verify conformance and surface cross-team rollups, turning tusk into a source of clarity across teams without coupling their day-to-day workflows.

- [ ] Story: Shared milestone identity level=story order=1
  - [ ] Stable cross-workspace keys for milestones so multiple teams can reference "the same milestone" independently of UUIDs level=task order=1
  - [ ] Import/export carries the alignment identity so teams importing the same milestone recognize it as shared level=task order=2
- [ ] Story: Alignment-doc conformance check level=story order=2
  - [ ] `tusk align check` compares a team workspace's milestones against a configured alignment source and reports missing, extra, or mismatched entries level=task order=1
  - [ ] Read-only — never mutates the workspace automatically; surfaces a diff for the user to act on level=task order=2
  - [ ] MCP tool exposure for agent-driven conformance checks level=task order=3
- [ ] Story: Cross-team rollup level=story order=3
  - [ ] Aggregate progress rollup across multiple team workspaces by shared milestone reference level=task order=1
  - [ ] `tusk align status <milestone>` lists which teams own which portions and their current rollup level=task order=2
### Initiative: Urgency Profiles level=initiative order=9
> Named bundles of urgency weight overrides, attachable to any task or team. Follow-up to the subtree urgency overrides shipped in v0.13: once projects start repeating the same override combinations ("ship-critical", "research", "maintenance"), profiles replace the copy-paste. Also covers customizable rollup formulas if that scope follows.

- [ ] Story: Profile entity level=story order=1
  - [ ] Named profile with a weight map, stored per workspace level=task order=1
  - [ ] CRUD via `tusk urgency-profile create/modify/delete/list` level=task order=2
- [ ] Story: Profile attachment level=story order=2
  - [ ] Tasks reference a profile by name; resolution slots the profile's weights into the existing chain at the task-subtree layer level=task order=1
  - [ ] Inline profile overrides still allowed; profile provides defaults, task-local overrides win per key level=task order=2
## v0.14 — Naming and Spacing Convention level=milestone order=18 +naming-convention +v0.14
> Establish project-wide naming and spacing convention for the Tusk codebase, enforce it mechanically with a linter, and clean every existing package to match. No behavior changes; pure readability and tooling.
>
> Eight phases:
> - P1: Convention doc + linter scaffold + rule 1
> - P2: Custom analyzers (rules 2, 3, 4)
> - P3: Sweep service/
> - P4: Sweep internal/tui/
> - P5: Sweep internal/mcp/ + internal/portability/
> - P6: Sweep filter/ + domain/ + syntax/
> - P7: Sweep repository/ + sqlite/ + cmd/ + tests/e2e/ + root
> - P8: Lock-in (regression guards)
>
> Spec: docs/superpowers/specs/2026-04-28-v0.14-naming-convention-design.md
> Plans: docs/superpowers/plans/v014-naming-convention/
> Tickets: docs/superpowers/plans/v014-naming-convention/tasks/

### [v0.14 P1] Convention doc + linter scaffold + rule 1 level=initiative order=1 +naming-convention +phase-1 +v0.14
> Land STYLE.md, the multichecker-shell linter binary (cmd/tusk-lint), Makefile integration, and varnamelen configured in .golangci.yml with twelve per-package path exclusions covering every existing directory. After this phase ships, make lint runs both golangci-lint and tusk-lint in CI and finds zero new violations (everything is excluded). The codebase compiles and tests pass with no behavior changes.
>
> Why: Phase 1 establishes the rule-1 enforcement surface and the analyzer framework that Phase 2 plugs into.
>
> Acceptance:
> - STYLE.md exists at repo root with all four rules and the style guide.
> - cmd/tusk-lint binary builds and runs.
> - make lint runs both lint-go and lint-tusk.
> - make build, make test, make lint all pass against the unmodified codebase.
>
> Plan: docs/superpowers/plans/v014-naming-convention/01-convention-and-scaffold.md
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P1.md
> Blocks: P2.

- [x] [v0.14 P1-T1] Write STYLE.md level=task order=1 +naming-convention +phase-1 +v0.14
> What: Author STYLE.md at the repo root with three sections: the four mechanical rules (with before/after examples and rationales), the advisory style guide (receivers, generic type parameters, loop indices), and an enforcement summary mapping each rule to the linter that enforces it.
>
> Why: STYLE.md is the canonical reference cited from PR templates, review checklists, and every sweep phase.
>
> Code references:
> - docs/superpowers/specs/2026-04-28-v0.14-naming-convention-design.md:42-191 — full convention text and examples.
> - service/task.go:64 — defaultProjectID block (rule 1 / rule 2 motivating example).
> - service/task.go:325-339 — listInBundle block (rule 3 motivating example).
>
> Acceptance:
> - STYLE.md exists at repo root.
> - All four rules documented with before/after examples and one-line rationales.
> - Style guide covers receivers, generic type parameters, and loop indices.
> - Enforcement summary names the linter for each rule.
> - Rule 3 explicitly states that on shadow, every err instance is renamed (including the first).
>
> Bridge code: None.
> Blocks: P1-T2 (link target).
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P1-T1.md

- [x] [v0.14 P1-T2] Cross-reference STYLE.md from CONTRIBUTING.md level=task order=2 +naming-convention +phase-1 +v0.14
> What: Add a one-line link to STYLE.md from CONTRIBUTING.md. Do not move existing CONTRIBUTING.md content.
>
> Why: Contributors land in CONTRIBUTING.md first; the link makes the convention discoverable without bloating CONTRIBUTING.md.
>
> Code references:
> - CONTRIBUTING.md — existing setup / commit-conventions content; locate a natural insertion point.
>
> Acceptance:
> - CONTRIBUTING.md contains a link to STYLE.md from at least one location.
> - No existing CONTRIBUTING.md content is moved or rewritten.
>
> Bridge code: None.
> Depends on: P1-T1.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P1-T2.md

- [x] [v0.14 P1-T3] Create cmd/tusk-lint multichecker shell level=task order=3 +naming-convention +phase-1 +v0.14
> What: Create cmd/tusk-lint/main.go that calls golang.org/x/tools/go/analysis/multichecker.Main() with an empty analyzer list. The binary must compile and exit zero on any input. Add golang.org/x/tools to go.mod if not already present.
>
> Why: Provides the binary entry point that Phase 2 populates with analyzers. Shipping the empty shell now lets make lint wire to a real binary in this phase rather than waiting for Phase 2.
>
> Code references:
> - cmd/ (existing dir) — add new tusk-lint/ subdirectory alongside tusk/.
> - go.mod:1 — module path is github.com/germanamz/tusk; add golang.org/x/tools.
>
> Acceptance:
> - cmd/tusk-lint/main.go exists and compiles.
> - go build ./cmd/tusk-lint produces a working binary that exits zero with no input.
> - A code comment tags the empty analyzer list as bridge code with the removal reference (Phase 2 task 5).
> - golang.org/x/tools is listed in go.mod.
>
> Bridge code: Introduces empty analyzer registry; removed by P2-T5.
> Blocks: P1-T4, P1-T6.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P1-T3.md

- [x] [v0.14 P1-T4] Wire lint-tusk into Makefile level=task order=4 +naming-convention +phase-1 +v0.14
> What: Add lint-tusk and lint-go targets to Makefile. Make the existing lint target depend on both. lint-go runs golangci-lint run ./...; lint-tusk runs the new cmd/tusk-lint binary over ./....
>
> Why: Single hook (make lint) runs both linters. CI already invokes make lint; no CI changes needed once the Makefile is updated.
>
> Code references:
> - Makefile — locate the existing lint target (currently runs golangci-lint run ./...) and refactor.
>
> Acceptance:
> - make lint-go runs golangci-lint run ./....
> - make lint-tusk runs cmd/tusk-lint over the codebase.
> - make lint depends on both lint-go and lint-tusk.
> - make lint exits zero against the unmodified codebase.
>
> Bridge code: None.
> Depends on: P1-T3.
> Blocks: P1-T6.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P1-T4.md

- [x] [v0.14 P1-T5] Configure varnamelen with per-package exclusions level=task order=5 +naming-convention +phase-1 +v0.14
> What: Enable varnamelen in .golangci.yml with strict settings (min-name-length: 2, check-receiver/check-return/check-type-param: true, empty ignore-names and ignore-decls). Add twelve per-package path exclusions covering every existing directory: service/, internal/tui/, internal/mcp/, internal/portability/, filter/, domain/, syntax/, repository/, sqlite/, cmd/, tests/e2e/, and client.go. Use the v2 schema (linters.exclusions.rules, linters.settings.varnamelen) consistent with the existing config.
>
> Why: Per-package rules are intentional: parallel sweep phases (3–7) each remove a different rule, so branches do not merge-conflict on a single shared alternation. The full-codebase exclusion preserves CI green at the moment Phase 1 lands.
>
> Code references:
> - .golangci.yml:1-9 — existing v2 schema (with errcheck exclusion rules); preserve those rules verbatim.
> - Anchored slashes (^service/ not ^service) prevent prefix-collision matches.
>
> Acceptance:
> - varnamelen enabled under linters.enable.
> - Strict settings under linters.settings.varnamelen.
> - Twelve linters: [varnamelen] exclusion rules under linters.exclusions.rules, one per package.
> - Existing errcheck rules preserved verbatim.
> - make lint exits zero against the unmodified codebase.
>
> Bridge code: Introduces twelve per-package varnamelen exclusion rules; removed by P3-T1, P4-T1, P5-T1, P6-T1, P7-T1; residuals removed by P8-T1.
> Blocks: P1-T6.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P1-T5.md

- [x] [v0.14 P1-T6] Verify Phase 1 CI green level=task order=6 +naming-convention +phase-1 +v0.14
> What: Run the full local validation suite to confirm the Phase 1 changes are CI-clean: make build, make test, make lint, and a representative pre-commit hook execution.
>
> Why: Phase 1 introduces no behavior changes but does enable a new linter and add a new lint target. This task closes the phase by proving all three gates stay green.
>
> Code references:
> - Makefile — build, test, lint targets.
> - lefthook.yml (or equivalent) — pre-commit hooks if any.
>
> Acceptance:
> - make build exits zero.
> - make test exits zero.
> - make lint exits zero (both lint-go and lint-tusk pass).
> - Pre-commit hooks pass when committing the phase's changes.
>
> Bridge code: None.
> Depends on: P1-T1, P1-T2, P1-T3, P1-T4, P1-T5.
> Blocks: P2.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P1-T6.md

### [v0.14 P2] Custom analyzers (rules 2, 3, 4) level=initiative order=2 +naming-convention +phase-2 +v0.14
> Implement the three custom go/analysis analyzers (blankline, namederr, testhandle), wire them into cmd/tusk-lint, and add a shared path-filter helper that all three honor. After this phase, all four rules are linter-enforced but every existing directory is still excluded — make lint continues to find zero violations against the unmodified codebase.
>
> Why: Phase 2 is the complete rule-enforcement surface. Every subsequent sweep phase consumes the analyzers built here.
>
> Acceptance:
> - All three analyzers in internal/lint/<analyzer>/ with analyzer.go, analyzer_test.go, testdata/src/a/a.go.
> - pathfilter.Excluded(pkgPath) honors twelve per-package entries.
> - cmd/tusk-lint/main.go registers all three analyzers via multichecker.Main(...).
> - make lint, make test, make build pass against the unmodified codebase.
>
> Plan: docs/superpowers/plans/v014-naming-convention/02-custom-analyzers.md
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P2.md
> Blocks: P3, P4, P5, P6, P7.

- [x] [v0.14 P2-T1] Implement blankline analyzer level=task order=1 +naming-convention +phase-2 +v0.14
> What: Build internal/lint/blankline/ with analyzer.go, analyzer_test.go, and testdata/src/a/a.go. The analyzer detects missing blank lines around if err != nil (and <noun>Err != nil) guards: between an error-producing assignment and its guard, and between the guard's closing brace and the next statement. Test files use test *testing.T from the start (rule 4 applies to analyzer test code).
>
> Why: Rule 2 enforcement. Dense unspaced blocks make short identifiers visually easy to lose; even with rule 1 in place, spacing matters.
>
> Code references:
> - Spec example: docs/superpowers/specs/2026-04-28-v0.14-naming-convention-design.md:82-98.
> - Motivating site: service/task.go:64 (defaultProjectID block).
> - golang.org/x/tools/go/analysis/analysistest — test harness.
>
> Acceptance:
> - internal/lint/blankline/analyzer.go exports var Analyzer = &analysis.Analyzer{...}.
> - Diagnostic message references rule 2.
> - analyzer_test.go runs analysistest.Run(test, testdata, blankline.Analyzer, "a") with test *testing.T.
> - testdata/src/a/a.go contains both passing and failing patterns.
> - go test ./internal/lint/blankline/... passes.
>
> Bridge code: None.
> Blocks: P2-T5, P2-T6.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P2-T1.md

- [x] [v0.14 P2-T2] Implement namederr analyzer level=task order=2 +naming-convention +phase-2 +v0.14
> What: Build internal/lint/namederr/ with analyzer.go, analyzer_test.go, and testdata/src/a/a.go. The analyzer counts *ast.AssignStmt statements that declare err via := within each *ast.BlockStmt. When the count is ≥ 2, it emits a diagnostic on every such assignment (including the first), instructing the implementer to use typed names (<noun>Err). Test files use test *testing.T.
>
> Why: Rule 3 enforcement. Sequential err := shadows hide the failure mode at the variable; named errors document it. Renaming all instances rather than leaving the first as err keeps the block visually uniform.
>
> Code references:
> - Spec example: docs/superpowers/specs/2026-04-28-v0.14-naming-convention-design.md:111-144.
> - Canonical motivating site: service/task.go:325-339 (listInBundle with three sequential err := shadows).
>
> Acceptance:
> - internal/lint/namederr/analyzer.go exports var Analyzer = &analysis.Analyzer{...}.
> - Diagnostic fires on every err := in a scope with ≥ 2 such declarations, including the first.
> - Diagnostic message format: "namederr: 'err' is shadowed N times in this scope; rename all instances to typed names (e.g. fooErr, barErr)".
> - A function with exactly one err does NOT fire the rule.
> - go test ./internal/lint/namederr/... passes against testdata.
>
> Bridge code: None.
> Blocks: P2-T5, P2-T6.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P2-T2.md

- [x] [v0.14 P2-T3] Implement testhandle analyzer level=task order=3 +naming-convention +phase-2 +v0.14
> What: Build internal/lint/testhandle/ with analyzer.go, analyzer_test.go, and testdata/src/a/a.go. The analyzer walks every *ast.FuncDecl and *ast.FuncType. For each parameter, it resolves the type and consults a hardcoded table: *testing.T → test, *testing.B → bench, testing.TB → harness. If the parameter's name does not match, it emits a diagnostic. Test files use test *testing.T.
>
> Why: Rule 4 enforcement. Standardized test-handle names eliminate the last universally-tolerated single-character identifier in Go.
>
> Code references:
> - Spec example: docs/superpowers/specs/2026-04-28-v0.14-naming-convention-design.md:165-171.
> - Type table: docs/superpowers/specs/2026-04-28-v0.14-naming-convention-design.md:156-160.
>
> Acceptance:
> - internal/lint/testhandle/analyzer.go exports var Analyzer = &analysis.Analyzer{...}.
> - Hardcoded table covers *testing.T, *testing.B, testing.TB.
> - Diagnostic message format: "testhandle: parameter of type %s must be named %q, got %q".
> - go test ./internal/lint/testhandle/... passes against testdata exercising all three types.
>
> Bridge code: None.
> Blocks: P2-T5, P2-T6.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P2-T3.md

- [x] [v0.14 P2-T4] Add pathfilter helper level=task order=4 +naming-convention +phase-2 +v0.14
> What: Create internal/lint/pathfilter/pathfilter.go exporting func Excluded(pkgPath string) bool. The helper consults a compiled-in slice of twelve per-package regexes that mirror the per-package exclusion rules in .golangci.yml. Each analyzer's Run function consults this helper and short-circuits with no diagnostics when the package is excluded.
>
> Twelve packages: service, internal/tui, internal/mcp, internal/portability, filter, domain, syntax, repository, sqlite, cmd, tests/e2e, plus the module root (client.go).
>
> Why: Per-package entries are intentional: parallel sweep phases each remove a different entry, so branches do not merge-conflict on a single shared alternation. Verify the module path from go.mod (github.com/germanamz/tusk) before hardcoding.
>
> Code references:
> - go.mod:1 — module path github.com/germanamz/tusk.
> - .golangci.yml (after P1-T5) — twelve per-package varnamelen exclusion rules.
> - Module-root pattern: ^github\.com/germanamz/tusk$ (matches client.go's root package).
>
> Acceptance:
> - internal/lint/pathfilter/pathfilter.go exists and exports Excluded(pkgPath string) bool.
> - The excluded slice contains exactly twelve regex entries, each anchored with (/|$) for sub-package boundaries (except the module-root entry).
> - Each entry has a comment noting its removal target phase.
>
> Bridge code: Introduces twelve per-package exclusion entries; removed by P3-T2, P4-T2, P5-T2, P6-T2, P7-T2; residuals removed by P8-T2.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P2-T4.md

- [x] [v0.14 P2-T5] Register analyzers in cmd/tusk-lint level=task order=5 +naming-convention +phase-2 +v0.14
> What: Replace the empty analyzer list in cmd/tusk-lint/main.go (bridge code from P1-T3) with multichecker.Main(blankline.Analyzer, namederr.Analyzer, testhandle.Analyzer). Remove the bridge-code comment.
>
> Why: Activates all three rules in the linter binary. After this task ships, tusk-lint reports the rule violations defined in P2-T1 through P2-T3 wherever the package is not excluded by the pathfilter.
>
> Code references:
> - cmd/tusk-lint/main.go — currently calls multichecker.Main() with no analyzers (bridge code from P1-T3).
> - internal/lint/blankline, internal/lint/namederr, internal/lint/testhandle — packages from P2-T1, P2-T2, P2-T3.
>
> Acceptance:
> - cmd/tusk-lint/main.go imports the three analyzer packages.
> - multichecker.Main(...) is called with all three analyzers.
> - Phase-1 bridge-code comment is gone.
> - tusk-lint -blankline ./..., -namederr ./..., -testhandle ./... each run independently.
> - make lint continues to exit zero against the unmodified codebase.
>
> Bridge code: Removes empty analyzer registry (introduced by P1-T3).
> Depends on: P2-T1, P2-T2, P2-T3, P2-T4.
> Blocks: P2-T6.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P2-T5.md

- [x] [v0.14 P2-T6] Verify Phase 2 CI green level=task order=6 +naming-convention +phase-2 +v0.14
> What: Run go test ./internal/lint/... to verify all analyzer testdata fires the documented diagnostics. Run make lint against the unmodified codebase — must report zero violations because every existing package is excluded by the path filter. Run tusk-lint -blankline ./internal/lint/blankline/testdata/... manually to confirm the per-analyzer flag works on a single analyzer over a directory it knows.
>
> Why: Phase 2 closes by proving each analyzer fires correctly on testdata, the per-analyzer CLI flags work, and the unmodified codebase remains clean.
>
> Code references:
> - internal/lint/blankline/testdata/, internal/lint/namederr/testdata/, internal/lint/testhandle/testdata/ — fixture trees from P2-T1, P2-T2, P2-T3.
>
> Acceptance:
> - go test ./internal/lint/... exits zero.
> - make lint exits zero against the unmodified codebase.
> - tusk-lint -blankline ./internal/lint/blankline/testdata/... reports the documented violations.
> - make build, make test exit zero.
>
> Bridge code: None.
> Depends on: P2-T1, P2-T2, P2-T3, P2-T4, P2-T5.
> Blocks: P3, P4, P5, P6, P7.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P2-T6.md

### [v0.14 P3] Sweep service/ level=initiative order=3 +naming-convention +phase-3 +v0.14
> Bring every file in service/ (production and tests) into compliance with STYLE.md's four rules. Remove the service/ exclusion entries from both linters. Mechanical sweep; no behavior changes.
>
> Why: service/ is the highest-traffic package and the original motivating example for the convention. Sweeping it first proves the convention against real call sites before parallel sweeps roll it out further.
>
> Acceptance:
> - make lint passes against service/ with no exclusions.
> - make test passes — every service-layer behavior unchanged.
> - All MCP tools and CLI commands backed by services in this package continue to work identically.
>
> Plan: docs/superpowers/plans/v014-naming-convention/03-sweep-service.md
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P3.md
> Depends on: P2. Parallelizable with P4, P5, P6, P7. Blocks P8.

- [ ] [v0.14 P3-T1] Drop service/ varnamelen exclusion level=task order=1 +naming-convention +phase-3 +v0.14
> What: Delete the linters: [varnamelen] exclusion rule whose path is ^service/ from .golangci.yml. Other per-package rules stay untouched.
>
> Why: Removing the exclusion is the trigger that brings rule 1 into scope for service/.
>
> Code references:
> - .golangci.yml linters.exclusions.rules — locate the entry with path: ^service/.
>
> Acceptance:
> - .golangci.yml no longer contains a linters: [varnamelen] rule with path: ^service/.
> - All other linters: [varnamelen] exclusion rules and the existing errcheck rules remain.
>
> Bridge code: Removes service/ varnamelen exclusion rule (introduced by P1-T5).
> Blocks: P3-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P3-T1.md

- [ ] [v0.14 P3-T2] Drop service/ pathfilter entry level=task order=2 +naming-convention +phase-3 +v0.14
> What: Delete the regex line for ^github\.com/germanamz/tusk/service(/|$) from the excluded slice in internal/lint/pathfilter/pathfilter.go.
>
> Why: Removing this entry brings rules 2, 3, 4 into scope for service/. Pairs with P3-T1.
>
> Code references:
> - internal/lint/pathfilter/pathfilter.go — locate the entry matching service.
>
> Acceptance:
> - The excluded slice no longer contains the service entry.
> - All other entries remain.
>
> Bridge code: Removes service/ regex entry (introduced by P2-T4).
> Blocks: P3-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P3-T2.md

- [ ] [v0.14 P3-T3] Apply STYLE.md fixes across service/ level=task order=3 +naming-convention +phase-3 +v0.14
> What: Run make lint to enumerate every violation in service/, then apply mechanical fixes per STYLE.md across all production and test files. No behavior changes. If the diff exceeds review-friendly size, split per-file (task.go, task_test.go, project.go, …).
>
> The service/ package contains ~44 .go files including task.go (2174 LoC) and task_test.go (1754 LoC). Expected violation classes per STYLE.md rules 1–4: single-character locals and receivers, missing blank lines around if err != nil guards, sequential err := shadows, t *testing.T parameters.
>
> Why: Work-bearing task of the phase. Mechanical sweep that proves the convention against the highest-traffic package.
>
> Code references:
> - service/task.go:64 — defaultProjectID (rule 1 + rule 2 motivating example).
> - service/task.go:325-339 — listInBundle shadowed-err block (rule 3 canonical site).
> - service/task.go:361, 393, 437, 474, 503, 705, 774, 1431 and ~9 more — for _, t := range tasks sites.
> - All service/*_test.go files — t *testing.T parameters to rename.
>
> Acceptance:
> - Every file in service/ complies with STYLE.md rules 1–4.
> - No behavior changes (verified by P3-T4).
> - The diff is mechanical: identifier renames, blank-line insertions, named-error shadow renames, test-handle parameter renames.
>
> Bridge code: None.
> Depends on: P3-T1, P3-T2.
> Blocks: P3-T4, P3-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P3-T3.md

- [ ] [v0.14 P3-T4] Verify service/ tests pass level=task order=4 +naming-convention +phase-3 +v0.14
> What: Run make test to verify behavior is preserved after the sweep. All existing service-layer tests must pass. Failures indicate an accidental semantic change during the sweep — investigate and fix.
>
> Why: Mechanical renames can accidentally change semantics (e.g., shadowing a different identifier, mis-typed := vs =). Test suite is the regression net.
>
> Code references:
> - service/*_test.go — full test suite for the package.
>
> Acceptance:
> - make test exits zero.
> - No accidental semantic changes.
> - If failures surface, root-cause and fix as part of this task before P3-T5.
>
> Bridge code: None.
> Depends on: P3-T3.
> Blocks: P3-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P3-T4.md

- [ ] [v0.14 P3-T5] Verify service/ lint clean level=task order=5 +naming-convention +phase-3 +v0.14
> What: Run make lint to confirm zero violations across service/ with all four rules now active (no exclusions). Closes the phase.
>
> Why: Acceptance gate. The phase is "done" when both linters report zero violations against service/.
>
> Code references:
> - service/ — all production and test files.
>
> Acceptance:
> - make lint exits zero.
> - Both lint-go (varnamelen across service/) and lint-tusk (blankline + namederr + testhandle across service/) report no diagnostics.
> - All MCP tools and CLI commands continue to work identically.
>
> Bridge code: None.
> Depends on: P3-T4.
> Blocks: P8.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P3-T5.md

### [v0.14 P4] Sweep internal/tui/ level=initiative order=4 +naming-convention +phase-4 +v0.14
> Bring every file in internal/tui/ (production and tests) into compliance with STYLE.md's four rules. Remove the internal/tui/ exclusion entries from both linters. CLI command output remains byte-identical to pre-sweep (verified by existing e2e snapshot tests).
>
> Why: internal/tui/ is the second-largest package and the CLI surface. Sweeping it exercises the convention against cobra command builders and renderer code.
>
> Acceptance:
> - make lint passes against internal/tui/ with no exclusions.
> - make test passes — every CLI command produces byte-identical output.
> - Cobra help text remains unchanged.
>
> Plan: docs/superpowers/plans/v014-naming-convention/04-sweep-tui.md
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P4.md
> Depends on: P2. Parallelizable with P3, P5, P6, P7. Blocks P8.

- [ ] [v0.14 P4-T1] Drop internal/tui/ varnamelen exclusion level=task order=1 +naming-convention +phase-4 +v0.14
> What: Delete the linters: [varnamelen] rule whose path is ^internal/tui/ from .golangci.yml. Other per-package rules stay untouched.
>
> Why: Brings rule 1 into scope for internal/tui/.
>
> Code references:
> - .golangci.yml linters.exclusions.rules — locate the entry with path: ^internal/tui/.
>
> Acceptance:
> - .golangci.yml no longer contains a linters: [varnamelen] rule with path: ^internal/tui/.
> - All other rules remain.
>
> Bridge code: Removes internal/tui/ varnamelen exclusion rule (introduced by P1-T5).
> Blocks: P4-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P4-T1.md

- [ ] [v0.14 P4-T2] Drop internal/tui/ pathfilter entry level=task order=2 +naming-convention +phase-4 +v0.14
> What: Delete the regex line for ^github\.com/germanamz/tusk/internal/tui(/|$) from the excluded slice in internal/lint/pathfilter/pathfilter.go.
>
> Why: Brings rules 2, 3, 4 into scope for internal/tui/.
>
> Code references:
> - internal/lint/pathfilter/pathfilter.go — locate the entry matching internal/tui.
>
> Acceptance:
> - The excluded slice no longer contains the internal/tui entry.
> - All other entries remain.
>
> Bridge code: Removes internal/tui/ regex entry (introduced by P2-T4).
> Blocks: P4-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P4-T2.md

- [ ] [v0.14 P4-T3] Apply STYLE.md fixes across internal/tui/ level=task order=3 +naming-convention +phase-4 +v0.14
> What: Run make lint to enumerate every violation in internal/tui/, then apply mechanical fixes per STYLE.md across all production and test files. No behavior changes. Split per-file if the diff is too large for one PR — commands.go and render.go are the largest candidates.
>
> The package contains ~50 .go files. Expected violation classes: short receivers in cobra command builders (a *App → app *App), short range vars on tasks/projects/notes, runX handlers with multiple service calls missing blank lines around guards, sequential err := shadows, and t *testing.T parameters.
>
> Why: Work-bearing task of the phase.
>
> Code references:
> - internal/tui/commands.go (1450 LoC) — cobra command construction.
> - internal/tui/render.go (1231 LoC) — rendering helpers.
> - internal/tui/tree_markdown_test.go (959 LoC).
> - internal/tui/config.go (823 LoC).
> - internal/tui/commands_test.go (1491 LoC).
>
> Acceptance:
> - Every file in internal/tui/ complies with STYLE.md rules 1–4.
> - No behavior changes (verified by P4-T4).
>
> Bridge code: None.
> Depends on: P4-T1, P4-T2.
> Blocks: P4-T4, P4-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P4-T3.md

- [ ] [v0.14 P4-T4] Verify internal/tui/ tests pass level=task order=4 +naming-convention +phase-4 +v0.14
> What: Run make test to verify behavior is preserved after the sweep. All internal/tui/ tests, including the e2e tests that exercise CLI command output, must pass. CLI output remains byte-identical to pre-sweep.
>
> Why: CLI output is user-facing and verified by snapshot tests; any accidental change would surface here.
>
> Code references:
> - internal/tui/*_test.go and tests/e2e/ — exercise CLI surface.
>
> Acceptance:
> - make test exits zero.
> - tusk task create, list, get, modify, tree, next, etc. produce byte-identical output to pre-sweep.
>
> Bridge code: None.
> Depends on: P4-T3.
> Blocks: P4-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P4-T4.md

- [ ] [v0.14 P4-T5] Verify internal/tui/ lint clean level=task order=5 +naming-convention +phase-4 +v0.14
> What: Run make lint to confirm zero violations across internal/tui/ with all four rules now active (no exclusions). Closes the phase.
>
> Why: Acceptance gate.
>
> Code references:
> - internal/tui/ — all production and test files.
>
> Acceptance:
> - make lint exits zero.
> - Cobra help text for every command remains unchanged.
>
> Bridge code: None.
> Depends on: P4-T4.
> Blocks: P8.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P4-T5.md

### [v0.14 P5] Sweep internal/mcp/ + internal/portability/ level=initiative order=5 +naming-convention +phase-5 +v0.14
> Bring every file in internal/mcp/ and internal/portability/ (production and tests) into compliance with STYLE.md's four rules. Remove all four exclusion entries (two packages × two linter configs) from both linters. MCP tool responses and workspace export/import JSON output remain byte-identical to pre-sweep.
>
> Why: internal/mcp/ is the third-largest package and the MCP protocol surface. internal/portability/ is folded in here because no other phase covers internal/-rooted code beyond tui and mcp.
>
> Acceptance:
> - make lint passes against both packages with no exclusions.
> - make test passes — every MCP tool and portability encode/decode round-trip behavior unchanged.
> - The [mcp.blocked_fields] enforcement layer (v0.12) continues to block configured tool/field combinations.
> - The MCP server's stdio and SSE transports continue to function.
>
> Plan: docs/superpowers/plans/v014-naming-convention/05-sweep-mcp.md
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P5.md
> Depends on: P2. Parallelizable with P3, P4, P6, P7. Blocks P8.

- [ ] [v0.14 P5-T1] Drop internal/mcp/ + internal/portability/ varnamelen exclusions level=task order=1 +naming-convention +phase-5 +v0.14
> What: Delete the two linters: [varnamelen] rules whose path is ^internal/mcp/ and ^internal/portability/ from .golangci.yml. Other per-package rules stay untouched.
>
> Why: Brings rule 1 into scope for both packages.
>
> Code references:
> - .golangci.yml linters.exclusions.rules — locate entries with path: ^internal/mcp/ and path: ^internal/portability/.
>
> Acceptance:
> - Neither rule exists in .golangci.yml after the change.
> - All other rules remain.
>
> Bridge code: Removes two varnamelen exclusion rules (introduced by P1-T5).
> Blocks: P5-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P5-T1.md

- [ ] [v0.14 P5-T2] Drop internal/mcp/ + internal/portability/ pathfilter entries level=task order=2 +naming-convention +phase-5 +v0.14
> What: Delete the two regex lines for ^github\.com/germanamz/tusk/internal/mcp(/|$) and ^github\.com/germanamz/tusk/internal/portability(/|$) from the excluded slice in internal/lint/pathfilter/pathfilter.go.
>
> Why: Brings rules 2, 3, 4 into scope for both packages.
>
> Code references:
> - internal/lint/pathfilter/pathfilter.go — locate entries matching internal/mcp and internal/portability.
>
> Acceptance:
> - Neither entry exists in the excluded slice after the change.
> - All other entries remain.
>
> Bridge code: Removes two regex entries (introduced by P2-T4).
> Blocks: P5-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P5-T2.md

- [ ] [v0.14 P5-T3] Apply STYLE.md fixes across internal/mcp/ + internal/portability/ level=task order=3 +naming-convention +phase-5 +v0.14
> What: Run make lint to enumerate every violation in internal/mcp/ and internal/portability/, then apply mechanical fixes per STYLE.md across all production and test files. No behavior changes. Per-handler PRs are a reasonable split if tools.go produces an oversized diff.
>
> internal/mcp/ contains ~22 .go files. internal/portability/ contains 5. Expected violation classes: short locals during JSON marshaling (taskResponse, urgencyWeightsJSON, projectNameCache), short receivers (*ImportError), short dec/enc locals around json.NewDecoder/json.NewEncoder, short range vars (for _, b := range blocks), missing blank lines around guards in per-tool handlers, sequential err := shadows, and t *testing.T parameters.
>
> Why: Work-bearing task. Per-tool handlers each do multiple service calls — a high-density site for rules 2 and 3.
>
> Code references:
> - internal/mcp/tools.go:666, 734, 750 — for _, b := range blocks sites.
> - internal/mcp/tools.go (1729 LoC) — per-tool handlers.
> - internal/mcp/server.go (1043 LoC).
> - internal/mcp/project_handlers_test.go (619 LoC).
> - internal/mcp/handlers_test.go (578 LoC).
> - internal/portability/decode.go:29 — func (e *ImportError) Error() receiver rename.
> - internal/portability/decode.go:48 — dec := json.NewDecoder(r) local rename.
> - internal/portability/encode.go:18 — enc := json.NewEncoder(w) local rename.
> - internal/portability/*_test.go — t *testing.T parameter renames (~30 sites).
>
> Acceptance:
> - Every file in both packages complies with STYLE.md rules 1–4.
> - No behavior changes (verified by P5-T4).
>
> Bridge code: None.
> Depends on: P5-T1, P5-T2.
> Blocks: P5-T4, P5-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P5-T3.md

- [ ] [v0.14 P5-T4] Verify MCP and portability tests pass level=task order=4 +naming-convention +phase-5 +v0.14
> What: Run make test to verify behavior is preserved across both packages. All MCP server tests, tool-handler tests, tool-registry tests, and internal/portability/ encode/decode round-trip tests must pass.
>
> Why: MCP tool responses and portability JSON output are user-facing contracts; any accidental change would surface here.
>
> Code references:
> - internal/mcp/*_test.go.
> - internal/portability/encode_test.go, internal/portability/decode_test.go.
>
> Acceptance:
> - make test exits zero.
> - MCP tool responses are byte-identical to pre-sweep.
> - Workspace export/import via internal/portability/ produces byte-identical JSON output for the same input and round-trips identically.
>
> Bridge code: None.
> Depends on: P5-T3.
> Blocks: P5-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P5-T4.md

- [ ] [v0.14 P5-T5] Verify P5 packages lint clean level=task order=5 +naming-convention +phase-5 +v0.14
> What: Run make lint to confirm zero violations across internal/mcp/ and internal/portability/ with all four rules now active. Closes the phase.
>
> Why: Acceptance gate.
>
> Code references:
> - internal/mcp/, internal/portability/ — all production and test files.
>
> Acceptance:
> - make lint exits zero.
>
> Bridge code: None.
> Depends on: P5-T4.
> Blocks: P8.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P5-T5.md

### [v0.14 P6] Sweep filter/ + domain/ + syntax/ level=initiative order=6 +naming-convention +phase-6 +v0.14
> Bring every file in filter/, domain/, and syntax/ (production and tests) into compliance with STYLE.md's four rules. Remove the six exclusion entries (three packages × two linter configs) from both linters. Filter expressions, urgency scoring, taxonomy validation, and syntax/ lex/AST behavior remain identical.
>
> Why: filter/, domain/, and syntax/ together host the parse path and the core types. Bundling avoids inflating phase count for three small/medium packages; syntax/ pairs naturally with filter/.
>
> Acceptance:
> - make lint passes against all three packages with no exclusions.
> - make test passes — filter parser, urgency scoring, taxonomy validation, workflow transitions, and syntax lex/AST behavior unchanged.
>
> Plan: docs/superpowers/plans/v014-naming-convention/06-sweep-filter-domain.md
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P6.md
> Depends on: P2. Parallelizable with P3, P4, P5, P7. Blocks P8.

- [ ] [v0.14 P6-T1] Drop filter/ + domain/ + syntax/ varnamelen exclusions level=task order=1 +naming-convention +phase-6 +v0.14
> What: Delete the three linters: [varnamelen] rules whose path is ^filter/, ^domain/, and ^syntax/ from .golangci.yml. Other per-package rules stay untouched.
>
> Why: Brings rule 1 into scope for all three packages.
>
> Code references:
> - .golangci.yml linters.exclusions.rules — locate entries with path: ^filter/, path: ^domain/, path: ^syntax/.
>
> Acceptance:
> - None of the three rules exists in .golangci.yml after the change.
> - All other rules remain.
>
> Bridge code: Removes three varnamelen exclusion rules (introduced by P1-T5).
> Blocks: P6-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P6-T1.md

- [ ] [v0.14 P6-T2] Drop filter/ + domain/ + syntax/ pathfilter entries level=task order=2 +naming-convention +phase-6 +v0.14
> What: Delete the three regex lines for ^github\.com/germanamz/tusk/filter(/|$), ^github\.com/germanamz/tusk/domain(/|$), and ^github\.com/germanamz/tusk/syntax(/|$) from the excluded slice in internal/lint/pathfilter/pathfilter.go.
>
> Why: Brings rules 2, 3, 4 into scope for all three packages.
>
> Code references:
> - internal/lint/pathfilter/pathfilter.go — locate entries matching filter, domain, syntax.
>
> Acceptance:
> - None of the three entries exists in the excluded slice after the change.
> - All other entries remain.
>
> Bridge code: Removes three regex entries (introduced by P2-T4).
> Blocks: P6-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P6-T2.md

- [ ] [v0.14 P6-T3] Apply STYLE.md fixes across filter/ + domain/ + syntax/ level=task order=3 +naming-convention +phase-6 +v0.14
> What: Run make lint to enumerate every violation in filter/, domain/, and syntax/, then apply mechanical fixes per STYLE.md across all production and test files. No behavior changes. Per-file diffs are smaller than service/ or internal/tui/ sweeps; one PR for all three packages should still be reviewable.
>
> filter/ has 22 files (parser, lexer, validators, resolvers). domain/ has 36 files (entity definitions, validators, urgency overrides helpers, taxonomy validators). syntax/ has 10 files (token, AST, modifier, parse_fields). Expected violation classes: parser state-machine short locals, AST/range-var renames, missing blank lines on parser error paths, t *testing.T parameters.
>
> Why: Work-bearing task.
>
> Code references:
> - filter/ — parser, lexer, validators.
> - domain/ — entity definitions, urgency overrides, taxonomy validators.
> - syntax/ast.go:37, 48 — for _, t := range fs.Tags sites.
> - syntax/errors.go:32 — for i, e := range errs.
> - syntax/modifier_test.go:17 — for _, b := range []byte{...}.
> - syntax/*_test.go — extensive t *testing.T usage.
>
> Acceptance:
> - Every file in all three packages complies with STYLE.md rules 1–4.
> - No behavior changes (verified by P6-T4).
>
> Bridge code: None.
> Depends on: P6-T1, P6-T2.
> Blocks: P6-T4, P6-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P6-T3.md

- [ ] [v0.14 P6-T4] Verify filter/ + domain/ + syntax/ tests pass level=task order=4 +naming-convention +phase-6 +v0.14
> What: Run make test to verify behavior is preserved. Filter parser tests (boolean operators, tag include/exclude, UDA fields, urgency keys), domain tests (taxonomy validation, urgency overrides math, workflow validation), and syntax/ tests (token, AST, modifier, parse_fields) must all pass.
>
> Why: These are the parsing and core-type contracts. Edge-case coverage in the test suite is dense; any accidental change surfaces here.
>
> Code references:
> - filter/*_test.go, domain/*_test.go, syntax/*_test.go.
>
> Acceptance:
> - make test exits zero.
> - Filter expressions (status=pending,active, priority=2..4, due=today, +tag, -tag, parent=<short_id>, tree=<short_id>, uda.<key>=<value>) parse and evaluate identically.
> - Urgency scoring produces identical scores for the same inputs.
> - Taxonomy level validation enforces/rejects identical inputs.
> - syntax/ modifier registration and tag-prefix recognition behave identically.
>
> Bridge code: None.
> Depends on: P6-T3.
> Blocks: P6-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P6-T4.md

- [ ] [v0.14 P6-T5] Verify P6 packages lint clean level=task order=5 +naming-convention +phase-6 +v0.14
> What: Run make lint to confirm zero violations across filter/, domain/, and syntax/. Closes the phase.
>
> Why: Acceptance gate.
>
> Code references:
> - filter/, domain/, syntax/.
>
> Acceptance:
> - make lint exits zero.
>
> Bridge code: None.
> Depends on: P6-T4.
> Blocks: P8.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P6-T5.md

### [v0.14 P7] Sweep repository/ + sqlite/ + cmd/ + tests/e2e/ + root level=initiative order=7 +naming-convention +phase-7 +v0.14
> Bring repository/, sqlite/, cmd/, tests/e2e/, and the root package (client.go, client_test.go) into compliance with STYLE.md's four rules. Remove the five package-level exclusion entries from both linters. After this phase (combined with Phases 3–6), every package in the repository is clean.
>
> Why: This phase closes out the remaining production and test code. cmd/tusk-lint/ (introduced in Phase 1) is also swept here — it must already be clean since it was written under the convention, but the linter verifies it.
>
> Acceptance:
> - make lint passes against all five packages with no exclusions.
> - make test and make test-race pass — every repository implementation, every CLI scenario, every entry-point behavior unchanged.
> - Default DB path resolution and migration behavior unchanged.
>
> Plan: docs/superpowers/plans/v014-naming-convention/07-sweep-rest.md
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P7.md
> Depends on: P2. Parallelizable with P3, P4, P5, P6. Blocks P8.

- [ ] [v0.14 P7-T1] Drop remaining varnamelen exclusions level=task order=1 +naming-convention +phase-7 +v0.14
> What: Delete the five linters: [varnamelen] rules whose path is ^repository/, ^sqlite/, ^cmd/, ^tests/e2e/, and ^client\.go$ from .golangci.yml. Other rules stay untouched.
>
> Why: Brings rule 1 into scope for the final five packages.
>
> Code references:
> - .golangci.yml linters.exclusions.rules — locate the five entries.
>
> Acceptance:
> - None of the five rules exists in .golangci.yml after the change.
> - Existing errcheck rules remain.
>
> Bridge code: Removes five varnamelen exclusion rules (introduced by P1-T5).
> Blocks: P7-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P7-T1.md

- [ ] [v0.14 P7-T2] Drop remaining pathfilter entries level=task order=2 +naming-convention +phase-7 +v0.14
> What: Delete the five regex lines for repository, sqlite, cmd, tests/e2e, and the module-root pattern (^github\.com/germanamz/tusk$) from the excluded slice in internal/lint/pathfilter/pathfilter.go.
>
> Why: Brings rules 2, 3, 4 into scope for the final five packages.
>
> Code references:
> - internal/lint/pathfilter/pathfilter.go — locate the five entries.
>
> Acceptance:
> - None of the five entries exists in the excluded slice after the change.
> - After this task ships and assuming all other sweeps have shipped, the excluded slice is empty.
>
> Bridge code: Removes five regex entries (introduced by P2-T4).
> Blocks: P7-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P7-T2.md

- [ ] [v0.14 P7-T3] Apply STYLE.md fixes across remaining packages level=task order=3 +naming-convention +phase-7 +v0.14
> What: Run make lint to enumerate every violation across repository/, sqlite/, cmd/, tests/e2e/, and the root files. Apply mechanical fixes per STYLE.md.
>
> Coverage:
> - repository/ (10 files) — interface definitions; mostly parameter renames and short locals in test helpers.
> - sqlite/ (~30 files) — repository implementations; short locals around sql.Tx, sql.Rows, prepared statements.
> - cmd/ — cmd/tusk/main.go (360 LoC), main_test.go (86 LoC), and cmd/tusk-lint/main.go (introduced in Phase 1; verify clean).
> - tests/e2e/ — black-box CLI tests; heavy t *testing.T usage.
> - Root files: client.go, client_test.go.
>
> For cmd/tusk-lint/, the analyzers themselves use AST walking patterns where short identifiers like n for ast.Node, t for types.Type, s for ast.Stmt are common — these all rename per the style guide.
>
> Why: Final mechanical sweep. After this task ships, every package in the repo is convention-clean.
>
> Code references:
> - repository/ — 10 files of interface definitions.
> - sqlite/ (~30 files), including sqlite/task_test.go (1093 LoC).
> - cmd/tusk/main.go (360 LoC).
> - cmd/tusk/main_test.go (86 LoC).
> - cmd/tusk-lint/main.go (from P1-T3 / P2-T5).
> - tests/e2e/ — black-box harness.
> - client.go (~8.5 KB) and client_test.go (~2.8 KB) at repo root.
>
> Acceptance:
> - Every file in the listed packages complies with STYLE.md rules 1–4.
> - No behavior changes (verified by P7-T4).
>
> Bridge code: None.
> Depends on: P7-T1, P7-T2.
> Blocks: P7-T4, P7-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P7-T3.md

- [ ] [v0.14 P7-T4] Verify e2e and unit tests pass level=task order=4 +naming-convention +phase-7 +v0.14
> What: Run make test and make test-race to verify behavior is preserved. The e2e suite is the most exercising — every CLI scenario, every output format, every DB-config combination must continue to pass. make test-race should also pass (no new race conditions introduced by mechanical renames).
>
> Why: This is the broadest test surface. Catches any accidental semantic change introduced by P7-T3 across DB layer, CLI entry points, and e2e scenarios.
>
> Code references:
> - tests/e2e/ — full e2e harness across DB-config and output-format combinations.
> - sqlite/*_test.go — repository unit tests.
> - cmd/tusk/main_test.go.
>
> Acceptance:
> - make test exits zero.
> - make test-race exits zero.
> - Default DB path resolution (~/.local/share/tusk/tusk.db), flag/env override (--db > TUSK_DB), and walk-up config discovery work identically.
> - Migration application from a fresh DB produces a schema identical to pre-sweep.
> - The tusk and tusk-lint binaries both build and run identically.
>
> Bridge code: None.
> Depends on: P7-T3.
> Blocks: P7-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P7-T4.md

- [ ] [v0.14 P7-T5] Verify P7 packages lint clean level=task order=5 +naming-convention +phase-7 +v0.14
> What: Run make lint to confirm zero violations across repository/, sqlite/, cmd/, tests/e2e/, and the root package. Closes the phase.
>
> Why: Acceptance gate. After this task ships and assuming P3..P6 have also shipped, every package in the repo is convention-clean and Phase 8 can begin.
>
> Code references:
> - repository/, sqlite/, cmd/, tests/e2e/, client.go, client_test.go.
>
> Acceptance:
> - make lint exits zero.
>
> Bridge code: None.
> Depends on: P7-T4.
> Blocks: P8.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P7-T5.md

### [v0.14 P-CONFIG] Sweep config/ level=task order=7.5 +naming-convention +spec-gap +v0.14
> ## What
>
> Bring `config/` into compliance with STYLE.md's four rules. Drop the `^config/` exclusion from `.golangci.yml` and the corresponding `internal/lint/pathfilter` entry (added by P2). Apply fixes; verify `make test` and `make lint` are clean.
>
> ## Why — spec gap caught during P1-T5
>
> `config/` was missing from the original v0.14 plan-doc enumeration of packages. It has Go production code (`config/config.go`, `config/resolver.go`, `config/write.go`) with at least 6 varnamelen violations as of P1-T5. To preserve "Phase 1 ships CI-green" intent (per `01-convention-and-scaffold.md`), P1-T5 added a 13th `^config/` exclusion that was not in the spec. This task removes it.
>
> ## Scope
>
> - 6 known varnamelen violations (single-letter receivers/params/locals in `config/config.go` and `config/write.go`).
> - Plus any rule-2/3/4 violations surfaced by `tusk-lint` once analyzers ship in P2 and the pathfilter entry is added.
>
> A single task (not the usual 5-subtask sweep) because the violation count is small.
>
> ## Acceptance
>
> - `^config/` removed from `.golangci.yml` `linters.exclusions.rules`.
> - `^github.com/germanamz/tusk/config(/|$)` removed from `internal/lint/pathfilter` excluded slice.
> - All STYLE.md rule violations in `config/` fixed (mechanical only — no behavior changes).
> - `make lint` exits zero with `config/` in scope.
> - `make test ./config/...` exits zero.
>
> ## Bridge code
>
> - Removes the `^config/` `varnamelen` exclusion (introduced by P1-T5 deviation).
> - Removes the `config` pathfilter entry (must be added by P2-T4 — this task should land before P8).
>
> ## Depends on
>
> P2 (custom analyzers in place; pathfilter helper exists with config entry to remove).
>
> ## Parallelizable with
>
> P3, P4, P5, P6, P7.
>
> ## Blocks
>
> P8 (lock-in cannot start until all sweep gates pass — this includes config/).
>
> ## Follow-ups
>
> - P2-T4 must include `config` in the `internal/lint/pathfilter` excluded slice (to keep CI green between P2 and this sweep). Add it alongside the other 12 entries.
> - P8-T1 should not find a `^config/` rule remaining once this task is done.
>
> ## References
>
> - Spec: `docs/superpowers/specs/2026-04-28-v0.14-naming-convention-design.md` §3 (rollout strategy — config/ omitted from package enumeration).
> - Plan: `docs/superpowers/plans/v014-naming-convention/01-convention-and-scaffold.md` task 5 (where the 13th exclusion was added).
> - PR adding the exclusion: P1-T5 (link in tusk after merge).

### [v0.14 P8] Lock-in level=initiative order=8 +naming-convention +phase-8 +v0.14
> Verify residual exclusion infrastructure is gone and add structural regression guards so the convention cannot quietly degrade in future PRs. After this phase ships, every package in the repository is in compliance with all four STYLE.md rules and the convention is structurally protected.
>
> Why: Without lock-in, future PRs could reintroduce violations or add // nolint:varnamelen directives. The phase makes the milestone's done state structurally verifiable.
>
> Acceptance:
> - .golangci.yml contains zero linters: [varnamelen] exclusion rules.
> - internal/lint/pathfilter/pathfilter.go's excluded slice is empty.
> - make lint-style-locked is wired into CI through make lint and fails on regression triggers.
> - STYLE.md indicates "enforced repository-wide as of v0.14."
> - make build, make test, make test-race all pass.
>
> Plan: docs/superpowers/plans/v014-naming-convention/08-lock-in.md
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P8.md
> Depends on: P3, P4, P5, P6, P7 — all sweep phases must ship first.

- [ ] [v0.14 P8-T1] Verify all varnamelen exclusions are gone level=task order=1 +naming-convention +phase-8 +v0.14
> What: Run grep -A 3 'linters: \[varnamelen\]' .golangci.yml and confirm no output. If any rule remains, halt this phase and identify which sweep phase failed to remove its rule — the missing sweep must complete first.
>
> Why: This is a structural reconciliation step: the twelve sweep-phase removals must collectively zero out the per-package exclusion list. If any rule lingers, the lock-in CI guard added in P8-T3 would trigger and block the phase.
>
> Code references:
> - .golangci.yml linters.exclusions.rules — should contain only the two pre-existing errcheck rules.
>
> Acceptance:
> - grep -A 3 'linters: \[varnamelen\]' .golangci.yml produces no output.
> - The two errcheck rules from before v0.14 remain unchanged.
>
> Bridge code: Verifies any residual per-package varnamelen exclusion rules — should already be zero after Phases 3–7.
> Depends on: P3, P4, P5, P6, P7.
> Blocks: P8-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P8-T1.md

- [ ] [v0.14 P8-T2] Verify pathfilter slice is empty level=task order=2 +naming-convention +phase-8 +v0.14
> What: Open internal/lint/pathfilter/pathfilter.go and confirm the excluded slice has zero entries. If entries remain, halt and identify the missing sweep. With an empty slice the helper is a no-op (Excluded always returns false); leave the helper in place — it remains available for future per-package rollouts without re-introducing the bridge code.
>
> Why: Symmetric to P8-T1 — verifies that custom-analyzer exclusions are also fully gone. Leaving the helper as no-op infrastructure is a deliberate decision (see plan rationale).
>
> Code references:
> - internal/lint/pathfilter/pathfilter.go — should contain the helper function with an empty excluded slice.
>
> Acceptance:
> - The excluded slice in pathfilter.go is empty.
> - The Excluded(pkgPath) function still exists and returns false for all inputs.
>
> Bridge code: Verifies any residual per-package regex entries — should already be zero after Phases 3–7.
> Depends on: P3, P4, P5, P6, P7.
> Blocks: P8-T3.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P8-T2.md

- [ ] [v0.14 P8-T3] Add lint-style-locked CI guard level=task order=3 +naming-convention +phase-8 +v0.14
> What: Add a lint-style-locked Makefile target that fails CI if either of the following holds:
>
> 1. A // nolint:varnamelen directive appears anywhere in the Go source tree.
> 2. A linters: [varnamelen] exclusion rule appears in .golangci.yml.
>
> Make lint depend on lint-style-locked (in addition to lint-go and lint-tusk). Verify the target passes against the current state and fails when a directive is intentionally added then removed.
>
> Why: Structural regression guard. Without this, future PRs could silently reintroduce exclusions or nolint directives.
>
> Code references:
> - Makefile — add the new target alongside lint-go and lint-tusk.
>
> Acceptance:
> - make lint-style-locked is a Makefile target.
> - Target fails on either trigger condition with a clear error message.
> - make lint depends on lint-style-locked.
> - Target passes against the current (post-lock-in) repo state.
> - Target fails when a // nolint:varnamelen is intentionally added; passes again when removed.
>
> Bridge code: None.
> Depends on: P8-T1, P8-T2.
> Blocks: P8-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P8-T3.md

- [ ] [v0.14 P8-T4] Mark STYLE.md as enforced level=task order=4 +naming-convention +phase-8 +v0.14
> What: Update STYLE.md to indicate the convention is fully enforced. Add a one-line status note at the top: "Status: enforced repository-wide as of v0.14." If STYLE.md has an enforcement summary section (per P1-T1), append a note: "no exclusions in .golangci.yml, no // nolint:varnamelen directives anywhere; both guarded by make lint-style-locked in CI."
>
> Why: Documents the milestone's done state in the canonical convention doc. Future contributors learn from STYLE.md alone that the rules are structurally enforced.
>
> Code references:
> - STYLE.md (created in P1-T1).
>
> Acceptance:
> - STYLE.md carries the enforcement status note at the top.
> - The enforcement-summary section (if present) references lint-style-locked.
>
> Bridge code: None.
> Depends on: P8-T3.
> Blocks: P8-T5.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P8-T4.md

- [ ] [v0.14 P8-T5] Verify Phase 8 CI green level=task order=5 +naming-convention +phase-8 +v0.14
> What: Run make build, make test, make test-race, and make lint. All must pass. The lint-style-locked target runs as part of make lint and must pass.
>
> Why: Closes the v0.14 milestone. After this task ships, the convention is structurally protected and documented as such.
>
> Code references:
> - Makefile — build, test, test-race, lint, lint-style-locked.
>
> Acceptance:
> - make build exits zero.
> - make test exits zero.
> - make test-race exits zero.
> - make lint exits zero (including lint-style-locked).
> - No behavior changes from the v0.14 milestone (unchanged from prior phase verifications).
>
> Bridge code: None.
> Depends on: P8-T1, P8-T2, P8-T3, P8-T4.
> Ticket: docs/superpowers/plans/v014-naming-convention/tasks/P8-T5.md

