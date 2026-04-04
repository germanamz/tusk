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

- [ ] **Story: MCP server with stdio transport**
  - [ ] Server setup and lifecycle management
  - [ ] stdio transport implementation
  - [ ] Tool registration framework

- [ ] **Story: Task tools**
  - [ ] `tusk_task_create` — TaskService.Create
  - [ ] `tusk_task_list` — TaskService.List
  - [ ] `tusk_task_get` — TaskService.GetByShortID
  - [ ] `tusk_task_modify` — TaskService.Update
  - [ ] `tusk_task_start` — TaskService.Start
  - [ ] `tusk_task_done` — TaskService.Complete
  - [ ] `tusk_task_delete` — TaskService.Delete
  - [ ] `tusk_task_annotate` — TaskService.Annotate
  - [ ] `tusk_task_tree` — TaskService.Tree

- [ ] **Story: Relation & project tools**
  - [ ] `tusk_relation_add` — RelationService.Add
  - [ ] `tusk_relation_remove` — RelationService.Remove
  - [ ] `tusk_project_list` — ProjectService.List
  - [ ] `tusk_project_create` — ProjectService.Create

### Initiative: MCP Resources

> Expose tasks, projects, and workflows as readable resources.

- [ ] **Story: MCP resource definitions**
  - [ ] `tusk://tasks/{short_id}` resource
  - [ ] `tusk://projects/{name}` resource
  - [ ] `tusk://projects/{name}/workflow` resource

### Initiative: MCP Concurrency

> End-to-end optimistic locking through MCP tool I/O.

- [ ] **Story: Version passing**
  - [ ] Include `version` in all task tool responses
  - [ ] Accept `version` in modify/start/done/delete tool inputs
  - [ ] Return ErrConflict on version mismatch

---

## v0.4 — Urgency & UX

**Goal:** Smart task prioritization and polished terminal experience.

### Initiative: Urgency Scoring

> Weighted multi-factor urgency algorithm for task ranking.

- [ ] **Story: Urgency engine**
  - [ ] Implement scoring with default weights (priority, due, age, status, blocking, blocked, tags, project, annotations, waiting)
  - [ ] Sigmoid curve for due-date urgency
  - [ ] Integrate urgency into default list sort

- [ ] **Story: Configurable urgency weights**
  - [ ] Load weights from `~/.config/tusk/config.toml`
  - [ ] Allow per-project weight overrides
  - [ ] `tusk next` — display highest-urgency actionable task

### Initiative: TUI Polish

> Color, formatting, and quality-of-life improvements.

- [ ] **Story: Color-coded output**
  - [ ] Color by priority level
  - [ ] Color by status
  - [ ] Respect `NO_COLOR` / `--no-color` flag

- [ ] **Story: Tag colors**
  - [ ] Assign hex colors to tags
  - [ ] Display colored tags in list/info/tree output

- [ ] **Story: Undo**
  - [ ] `tusk undo` — revert last mutation
  - [ ] Store mutation log for rollback

---

## v0.5 — Advanced Features

**Goal:** Recurrence, user-defined attributes, additional transports, and data portability.

### Initiative: Recurrence

> Automatic task instance generation from RFC 5545 RRULE.

- [ ] **Story: RRULE support**
  - [ ] Parse RFC 5545 RRULE strings
  - [ ] Generate next instance on task completion
  - [ ] Handle recurrence edge cases (end date, count limit)

### Initiative: User-Defined Attributes

> Schemaless custom fields with optional per-project validation.

- [ ] **Story: UDA support**
  - [ ] Store/retrieve UDA JSON on tasks
  - [ ] Per-project UDA schema validation
  - [ ] Display UDAs in `tusk info`

### Initiative: MCP SSE Transport

> Network-accessible MCP server for multi-client scenarios.

- [ ] **Story: SSE transport**
  - [ ] SSE transport implementation
  - [ ] `tusk mcp serve --transport sse --port <port>`

### Initiative: Data Portability

> Export and sync capabilities.

- [ ] **Story: Export**
  - [ ] `tusk export --format json` — full dump
  - [ ] `tusk export --format csv` — flat export
  - [ ] `tusk sync` — import/export for offline use

---

## Future

**Goal:** Scale to multi-user, richer interfaces, and deeper integrations.

### Initiative: PostgreSQL Backend

- [ ] **Story: PostgreSQL repository implementation**
  - [ ] Implement all repository interfaces for PostgreSQL
  - [ ] Connection pooling and migration support

### Initiative: Interactive TUI

- [ ] **Story: Bubbletea-based interactive TUI**
  - [ ] Task list with keyboard navigation
  - [ ] Inline editing and status transitions

### Initiative: REST API

- [ ] **Story: HTTP REST API**
  - [ ] RESTful endpoints mirroring CLI/MCP capabilities
  - [ ] Authentication and authorization

### Initiative: Integrations & Extensions

- [ ] **Story: Webhook notifications**
  - [ ] Fire webhooks on task state changes

- [ ] **Story: Time tracking**
  - [ ] Start/stop timer on tasks
  - [ ] Report time spent

### Initiative: Advanced Filters

- [ ] **Story: Boolean operators in filters**
  - [ ] `AND` / `OR` / `NOT` operators
  - [ ] Parenthesized grouping

- [ ] **Story: Quoted string support in filters**
  - [ ] Enable `title:"some text"` and `description:"some text"` fields
