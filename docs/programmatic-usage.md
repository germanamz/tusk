# Programmatic Usage Guide

Tusk is a concurrent-safe task manager for humans and AI agents. It ships as a single binary with SQLite persistence, hierarchical tasks, typed relations, configurable workflows, and a built-in MCP server — so AI agents and humans can manage tasks side by side without stepping on each other's work.

Tusk's core packages are importable as a Go library. Other Go programs can embed tusk directly without shelling out to the CLI or speaking MCP.

```
go get github.com/germanamz/tusk
```

---

## Versioning

Tusk publishes semver tags that work with Go modules. Pin to a specific version:

```
go get github.com/germanamz/tusk@v0.8.0
```

Available versions:

| Version  | Notes                                                        |
|----------|--------------------------------------------------------------|
| `v0.8.0` | Introduces the root `Client` type for high-level programmatic access. |
| `v0.7.0` | Building-block packages available; no `Client` convenience type. |
| `v0.6.0` | |
| `v0.5.0` | |
| `v0.4.0` | |
| `v0.3.0` | |
| `v0.2.0` | |
| `v0.1.0` | Initial release. |

The `Client` type (described in this guide) requires **v0.8.0 or later**. Earlier versions expose the same functionality through the building-block packages (`domain`, `service`, `repository`, `sqlite`, `filter`, `config`), but you must wire the dependencies yourself — see [Building-Block Packages](#building-block-packages).

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/germanamz/tusk"
    "github.com/germanamz/tusk/domain"
)

func main() {
    ctx := context.Background()

    client, err := tusk.NewClient(tusk.Config{
        DBPath: "/tmp/tusk.db",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Create a task.
    task := &domain.Task{
        Title:    "Implement auth middleware",
        Priority: 3,
    }
    if err := client.Tasks.Create(ctx, task); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Created task %s (ID %s)\n", task.ShortID, task.ID)

    // Start it.
    task, err = client.Tasks.Start(ctx, task.ShortID, task.Version, "")
    if err != nil {
        log.Fatal(err)
    }

    // Complete it.
    task, err = client.Tasks.Complete(ctx, task.ShortID, task.Version)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Task %s completed\n", task.ShortID)
}
```

---

## Client

The `Client` type in the root `tusk` package is the single entry point. It opens a SQLite database, runs migrations, wires all repositories and services, and exposes them as public fields.

```go
type Client struct {
    Tasks     *service.TaskService
    Tags      *service.TagService
    Relations *service.RelationService
    Projects  *service.ProjectService
    Workflows *service.WorkflowService
    Players   *service.PlayerService
}
```

### Creating a Client

```go
client, err := tusk.NewClient(tusk.Config{
    DBPath: "/path/to/tusk.db",  // Required. Parent dirs are created automatically.
})
defer client.Close()
```

`NewClient` validates config cross-references, opens SQLite with WAL mode, runs embedded migrations, and wires everything up. Call `Close()` when done to release the database connection.

### Config

```go
type Config struct {
    // DBPath is the path to the SQLite database file. Required.
    DBPath string

    // Urgency holds weights for the urgency scoring algorithm.
    // When zero-valued, default weights are used.
    Urgency config.UrgencyConfig
}
```

Only `DBPath` is required. A fresh database is seeded with the built-in `default` project and `kanban` workflow via migrations — additional projects and workflows are created at runtime via `client.Projects.Create(...)` and `client.Workflows.Create(...)`.

Configuration is purely programmatic. No config files are read and no environment variables are consulted.

---

## Tasks

### Creating Tasks

Pass a `*domain.Task` with at least a `Title`. The service auto-populates `ID`, `ShortID`, `Version` (1), `Status` (`"pending"`), `ProjectID` (`"default"`), and timestamps.

```go
task := &domain.Task{
    Title:       "Build the thing",
    Description: "Detailed markdown description",
    Priority:    2, // 0 (none) through 4 (urgent)
}
err := client.Tasks.Create(ctx, task)
// task.ID, task.ShortID, task.Version are now populated.
```

Optional fields you can set before calling Create:

| Field            | Type              | Notes                                        |
|------------------|-------------------|----------------------------------------------|
| `Priority`       | `int`             | 0–4. Defaults to 0.                          |
| `Description`    | `string`          | Markdown body.                               |
| `ProjectID`      | `string`          | Must reference a configured project.         |
| `ParentID`       | `*uuid.UUID`      | Parent task UUID. Cycle detection enforced.   |
| `DueAt`          | `*time.Time`      | Due date.                                    |
| `WaitUntil`      | `*time.Time`      | Task is hidden from "next" until this time.  |
| `RecurrenceRule`  | `*string`         | RFC 5545 RRULE string.                       |
| `UDA`            | `map[string]any`  | User-defined attributes. Values must be strings. |

### Fetching Tasks

```go
// By short ID (8-char hex string shown in CLI output).
task, err := client.Tasks.GetByShortID(ctx, "a3f8b2c1")

// By full UUID.
task, err := client.Tasks.GetByID(ctx, someUUID)
```

Returns `domain.ErrNotFound` (wrapped) if the task doesn't exist.

### Listing and Filtering

```go
// List all pending and active tasks (default when no status filter is given).
tasks, err := client.Tasks.List(ctx, nil)

// With a filter.
tasks, err := client.Tasks.List(ctx, &domain.TermFilter{
    TaskFilter: domain.TaskFilter{
        Statuses:  []string{"active"},
        ProjectID: ptr("backend"),
        Tags:      []string{"api"},
    },
})
```

Results are scored by the urgency engine and returned sorted by urgency descending.

See [Filters](#filters) for the full filtering API.

### Updating Tasks

`TaskUpdate` uses pointer semantics to distinguish "don't change" from "set to value" from "clear field":

- `nil` — don't change
- For simple pointer fields (`*string`, `*int`): non-nil = set to value
- For double pointer fields (`**string`, `**uuid.UUID`, `**time.Time`): outer non-nil + inner nil = clear (set NULL), outer non-nil + inner non-nil = set to value

```go
newTitle := "Updated title"
updated, err := client.Tasks.Update(ctx, domain.TaskUpdate{
    ShortID: task.ShortID, // required — identifies the task
    Version: task.Version, // required — optimistic locking
    Title:   &newTitle,
})
```

**Clearing a nullable field:**

```go
// Clear the due date.
var nilTime *time.Time
updated, err := client.Tasks.Update(ctx, domain.TaskUpdate{
    ShortID: task.ShortID,
    Version: task.Version,
    DueAt:   &nilTime, // outer non-nil, inner nil → set to NULL
})
```

**Setting a nullable field:**

```go
due := time.Now().Add(48 * time.Hour)
duePtr := &due
updated, err := client.Tasks.Update(ctx, domain.TaskUpdate{
    ShortID: task.ShortID,
    Version: task.Version,
    DueAt:   &duePtr, // outer non-nil, inner non-nil → set to value
})
```

**Changing status via Update:**

```go
active := "active"
updated, err := client.Tasks.Update(ctx, domain.TaskUpdate{
    ShortID: task.ShortID,
    Version: task.Version,
    Status:  &active,
})
```

Status changes are validated against the project's workflow. Invalid transitions return `domain.ErrInvalidTransition`.

### TaskUpdate Fields

| Field            | Type              | Semantics                                         |
|------------------|-------------------|----------------------------------------------------|
| `ShortID`        | `string`          | Required. Identifies the task.                     |
| `Version`        | `int`             | Required. Optimistic lock.                         |
| `Title`          | `*string`         | nil = no change, non-nil = set                     |
| `Description`    | `**string`        | nil = no change, *nil = clear, *val = set          |
| `Status`         | `*string`         | nil = no change, non-nil = transition              |
| `Priority`       | `*int`            | nil = no change, non-nil = set                     |
| `ParentID`       | `**uuid.UUID`     | nil = no change, *nil = unparent, *val = reparent  |
| `ProjectID`      | `*string`         | nil = no change, non-nil = move to project         |
| `DueAt`          | `**time.Time`     | nil = no change, *nil = clear, *val = set          |
| `WaitUntil`      | `**time.Time`     | nil = no change, *nil = clear, *val = set          |
| `RecurrenceRule`  | `**string`        | nil = no change, *nil = clear, *val = set          |
| `UDA`            | `*map[string]any` | nil = no change, map merges keys (empty string value = delete key) |
| `ClaimedBy`      | `**string`        | nil = no change, *nil = clear, *val = set          |
| `ClaimedAt`      | `**time.Time`     | nil = no change, *nil = clear, *val = set          |

### Lifecycle Shortcuts

These methods handle status transitions, claiming, and workflow validation in one call:

```go
// Start a task (pending → active). Optionally claim for a player.
task, err := client.Tasks.Start(ctx, shortID, version, "player-1")
// Pass "" for playerID to start without claiming.

// Complete a task (active → completed).
task, err := client.Tasks.Complete(ctx, shortID, version)

// Soft-delete a task ({pending,active} → deleted).
task, err := client.Tasks.Delete(ctx, shortID, version)
```

All return the updated task with a new `Version`.

### Next Task

Returns the highest-urgency actionable task (pending or active, not waiting, not blocked by incomplete tasks):

```go
task, err := client.Tasks.Next(ctx)
if errors.Is(err, domain.ErrNotFound) {
    // No actionable tasks.
}
```

### Hierarchy

```go
// Direct children of a task.
children, err := client.Tasks.GetChildren(ctx, parentUUID)

// All descendants (recursive).
descendants, err := client.Tasks.GetDescendants(ctx, rootUUID)
```

Parent assignment is set via `ParentID` on Create or Update. Cycles are detected and rejected with `domain.ErrCyclicParent`.

### Annotations

Timestamped, immutable notes attached to tasks:

```go
// Add an annotation.
ann, err := client.Tasks.Annotate(ctx, shortID, "Blocked by upstream API changes")

// List annotations for a task.
annotations, err := client.Tasks.GetAnnotations(ctx, shortID)

// Delete an annotation.
err := client.Tasks.DeleteAnnotation(ctx, annotationUUID)
```

---

## Players and Claiming

Players represent humans or agents that work with tusk. They self-register by providing a string ID.

### Registering Players

```go
player, err := client.Players.Register(ctx, "agent-1", "agent")
// Type is "human" or "agent".

player, err := client.Players.Register(ctx, "german", "human")
```

Returns `domain.ErrConflict` if the ID is already taken.

### Claiming Tasks

```go
// Claim a task for a player (must be unclaimed or claimed by the same player).
task, err := client.Tasks.Claim(ctx, shortID, "agent-1", version)

// Release a claim (only the claimant can release).
task, err := client.Tasks.Release(ctx, shortID, "agent-1", version)
```

`Start` auto-claims for the player if a `playerID` is provided and the task is unclaimed. If another player holds the claim, `domain.ErrTaskClaimed` is returned.

### Task Queue

For multi-agent coordination:

```go
// List unclaimed, unblocked, actionable tasks.
available, err := client.Tasks.Available(ctx, nil) // nil filter = default statuses

// Atomically find, claim, and start the highest-urgency available task.
task, err := client.Tasks.Pop(ctx, "agent-1", nil)
if errors.Is(err, domain.ErrNoAvailableTasks) {
    // Nothing to do.
}
```

Both accept a `domain.FilterExpr` to scope the search (e.g., by project or tags).

### Player Queries

```go
player, err := client.Players.GetByID(ctx, "agent-1")
err := client.Players.UpdateLastSeen(ctx, "agent-1")
players, err := client.Players.List(ctx)
```

---

## Tags

```go
// Find or create a tag by name.
tag, err := client.Tags.FindOrCreate(ctx, "urgent")

// Create explicitly (fails with ErrConflict if exists).
tag, err := client.Tags.Create(ctx, "bug", ptr("#ff0000"))

// Assign tags to a task (find-or-create each tag automatically).
err := client.Tags.AssignToTask(ctx, taskUUID, []string{"bug", "urgent"})

// Remove tags from a task (silently skips missing assignments).
err := client.Tags.RemoveFromTask(ctx, taskUUID, []string{"urgent"})

// Get tags for a task.
tags, err := client.Tags.GetTaskTags(ctx, taskUUID)

// Batch fetch tags for multiple tasks.
tagMap, err := client.Tags.GetTaskTagsBatch(ctx, []uuid.UUID{id1, id2})

// List all tags.
tags, err := client.Tags.List(ctx)

// List tags with task counts.
tagsWithUsage, err := client.Tags.ListWithUsage(ctx)

// Rename a tag.
tag, err := client.Tags.Rename(ctx, "old-name", "new-name")

// Update a tag's color (nil to clear).
tag, err := client.Tags.Modify(ctx, "bug", ptr("#00ff00"))

// Delete a tag (fails with ErrTagInUse if assigned to any task).
tag, err := client.Tags.Delete(ctx, "unused-tag")
```

---

## Relations

Typed, directed edges between tasks:

```go
// Add a relation. Types: "blocks", "relates_to", "duplicates".
rel, err := client.Relations.Add(ctx, sourceShortID, targetShortID, "blocks")

// Remove a relation.
err := client.Relations.Remove(ctx, sourceShortID, targetShortID, "blocks")

// Get all relations for a task (as source or target).
rels, err := client.Relations.GetByTask(ctx, shortID)
```

**Cycle detection:** `blocks` relations are checked for cycles via DFS before insertion. If adding the edge would create a cycle, `domain.ErrCyclicBlock` is returned. The check and insert happen atomically within a transaction.

**Duplicate detection:** Adding a relation that already exists returns `domain.ErrDuplicateRelation`.

**Short ID resolution:** Source and target are specified by short ID. If either doesn't exist, `domain.ErrSourceNotFound` or `domain.ErrTargetNotFound` is returned.

---

## Projects and Workflows

Projects and workflows live in the database and are mutated through the service layer at runtime. Migrations seed every fresh database with a `default` project bound to the `kanban` workflow; additional entries are created by calling `client.Projects.Create` / `client.Workflows.Create`.

### Creating Projects

```go
kanban, err := client.Workflows.GetByName(ctx, "kanban")
if err != nil {
    log.Fatal(err)
}

backend, err := client.Projects.Create(ctx, service.CreateProjectInput{
    Name:       "backend",
    WorkflowID: kanban.ID,
    Settings: domain.ProjectSettings{
        AutoCompleteParent: &domain.AutoCompleteParentConfig{
            TriggerStatus: "completed",
            TargetStatus:  "completed",
        },
    },
})
```

### Creating Workflows

```go
dev, err := client.Workflows.Create(ctx, service.CreateWorkflowInput{
    Name: "dev",
    Statuses: map[string]domain.StatusConfig{
        "backlog":     {},
        "in_progress": {},
        "review":      {},
        "done":        {},
    },
    Transitions: []domain.WorkflowTransition{
        {From: "backlog", To: "in_progress"},
        {From: "in_progress", To: "review"},
        {From: "review", To: "done"},
        {From: "review", To: "in_progress"},
    },
})
```

### Querying Projects

```go
project, err := client.Projects.GetByID(ctx, "backend")
projects, err := client.Projects.List(ctx) // sorted by ID
```

### Querying Workflows

```go
// Check if a transition is allowed.
ok, err := client.Workflows.IsTransitionAllowed(ctx, "kanban", "pending", "active")

// Get all valid statuses for a workflow.
statuses, err := client.Workflows.GetStatuses(ctx, "kanban")

// Get all allowed transitions.
transitions, err := client.Workflows.GetTransitions(ctx, "kanban")

// List all workflows.
workflows, err := client.Workflows.List(ctx)

// Get a workflow by name.
wf, err := client.Workflows.GetByName(ctx, "kanban")

// Get a workflow and which projects use it.
wf, projectIDs, err := client.Workflows.GetWorkflowWithProjects(ctx, "kanban")
```

---

## Filters

Tusk supports two ways to build filters: programmatic construction and string parsing.

### Programmatic Filters

Filter expressions implement `domain.FilterExpr`. The leaf node is `TermFilter`, which wraps a `TaskFilter`:

```go
// Simple filter: active tasks in the backend project.
filter := &domain.TermFilter{
    TaskFilter: domain.TaskFilter{
        Statuses:  []string{"active"},
        ProjectID: ptr("backend"),
    },
}
tasks, err := client.Tasks.List(ctx, filter)
```

**Boolean composition:**

```go
// (status=active AND priority >= 3) OR (tagged "urgent")
filter := &domain.OrFilter{
    Children: []domain.FilterExpr{
        &domain.AndFilter{
            Children: []domain.FilterExpr{
                &domain.TermFilter{TaskFilter: domain.TaskFilter{
                    Statuses:    []string{"active"},
                    PriorityMin: ptr(3),
                }},
            },
        },
        &domain.TermFilter{TaskFilter: domain.TaskFilter{
            Tags: []string{"urgent"},
        }},
    },
}
```

**Negation:**

```go
// NOT status=deleted
filter := &domain.NotFilter{
    Child: &domain.TermFilter{TaskFilter: domain.TaskFilter{
        Statuses: []string{"deleted"},
    }},
}
```

**Passing `nil`** as the filter to `List` or `Available` returns tasks with the default status filter (`pending` + `active`).

### TaskFilter Fields

| Field               | Type                | Behavior                                        |
|---------------------|---------------------|-------------------------------------------------|
| `ProjectID`         | `*string`           | Exact match on project ID.                      |
| `ParentID`          | `*uuid.UUID`        | Direct children of this parent.                 |
| `RootID`            | `*uuid.UUID`        | All descendants of this root (tree query).      |
| `Statuses`          | `[]string`          | OR match — task matches any listed status.      |
| `Tags`              | `[]string`          | Task must have all listed tags.                 |
| `ExcludeTags`       | `[]string`          | Task must not have any listed tag.              |
| `PriorityMin`       | `*int`              | Minimum priority (inclusive).                   |
| `PriorityMax`       | `*int`              | Maximum priority (inclusive).                   |
| `DueAfter`          | `*time.Time`        | Due date after this time (exclusive).           |
| `DueBefore`         | `*time.Time`        | Due date before this time (exclusive).          |
| `WaitingOnly`       | `*bool`             | If true, only tasks with `WaitUntil` in future. |
| `TitleContains`     | `*string`           | Case-insensitive substring match in title.      |
| `DescriptionContains` | `*string`         | Case-insensitive substring match in description.|
| `UDA`               | `map[string]string` | AND match on UDA key-value pairs.               |
| `ClaimedBy`         | `*string`           | Exact match on claiming player ID.              |
| `Unclaimed`         | `*bool`             | If true, only tasks where `ClaimedBy` is NULL.  |

### Parsing Filter Strings

The `filter` package provides a 3-stage pipeline (Lex → Parse → Resolve) for parsing TaskWarrior-style filter strings into `domain.FilterExpr`:

```go
import "github.com/germanamz/tusk/filter"

expr, parseErrors := filter.ParseExpr("status=active +urgent priority=3..4")
if len(parseErrors) > 0 {
    // Handle parse errors.
}

// Resolve short IDs, dates, etc. into a domain.FilterExpr.
// The resolver needs a task lookup function for parent= and tree= filters.
resolved, err := filter.ResolveExpr(expr, taskLookupFunc)
```

**Filter string syntax:**

```
status=pending,active          # comma-separated statuses (OR)
project=backend                # project name
priority=2                     # exact priority (or: low, medium, high, urgent)
priority=1..3                  # priority range
due=2026-04-10                 # exact due date
due=today..friday              # relative date range
parent=<short_id>              # direct children
tree=<short_id>                # all descendants
waiting=true                   # only waiting tasks
title="foo"                    # substring in title (quoted)
description="bar"              # substring in description (quoted)
claimed_by=agent-1             # filter by player
unclaimed=true                 # only unclaimed
uda.custom_key=value           # UDA key-value match
+tagname                       # require tag
-tagname                       # exclude tag
AND, OR, NOT                   # boolean operators
(...)                          # grouping
```

---

## Optimistic Locking

Every mutable entity carries a `Version` field. All mutation operations require the current version and increment it on success. If the version doesn't match (another writer updated the entity since you last read it), the operation returns `domain.ErrConflict`.

```go
task, _ := client.Tasks.GetByShortID(ctx, "a3f8b2c1")
// task.Version == 1

task, err := client.Tasks.Start(ctx, task.ShortID, task.Version, "")
// task.Version == 2

// Stale version → conflict.
_, err = client.Tasks.Complete(ctx, task.ShortID, 1) // version 1 is stale
// errors.Is(err, domain.ErrConflict) == true
```

**Always use the version from the most recent read or mutation result.** This enables safe concurrent access from multiple goroutines, processes, or agents sharing the same database.

---

## Error Handling

All errors are in the `domain` package and checkable with `errors.Is()`:

| Error                       | Meaning                                                  |
|-----------------------------|----------------------------------------------------------|
| `domain.ErrNotFound`        | Entity doesn't exist.                                    |
| `domain.ErrConflict`        | Version mismatch (optimistic locking).                   |
| `domain.ErrInvalidTransition` | Status change not allowed by workflow.                 |
| `domain.ErrCyclicBlock`     | Adding this `blocks` relation would create a cycle.      |
| `domain.ErrCyclicParent`    | Setting this parent would create a cycle.                |
| `domain.ErrDuplicateRelation` | Relation already exists.                               |
| `domain.ErrTagInUse`        | Cannot delete tag; it's assigned to tasks.               |
| `domain.ErrTaskClaimed`     | Task is claimed by a different player.                   |
| `domain.ErrNoAvailableTasks` | No unclaimed, unblocked tasks match the filter.         |
| `domain.ErrSourceNotFound`  | Source task in relation doesn't exist (wraps ErrNotFound).|
| `domain.ErrTargetNotFound`  | Target task in relation doesn't exist (wraps ErrNotFound).|

```go
task, err := client.Tasks.GetByShortID(ctx, "nonexist1")
if errors.Is(err, domain.ErrNotFound) {
    // Task doesn't exist.
}
```

---

## Custom Configuration

Projects and workflows are mutable database rows — see [Creating Projects](#creating-projects) and [Creating Workflows](#creating-workflows) for the runtime API. The only globally-tunable section of `tusk.Config` is `Urgency`.

### Custom Urgency Weights

```go
Urgency: config.UrgencyConfig{
    PriorityWeight:    6.0,  // Weight for priority (0-4 normalized to 0-1).
    DueWeight:         12.0, // Weight for due date proximity (sigmoid curve).
    AgeWeight:         2.0,  // Weight for task age (days, capped at 365).
    ActiveWeight:      4.0,  // Bonus for active status.
    BlockingWeight:    8.0,  // Bonus for tasks that block others.
    BlockedWeight:     -5.0, // Penalty for blocked tasks.
    TagsWeight:        1.0,  // Weight for tag count.
    ProjectWeight:     1.0,  // Bonus for project membership.
    AnnotationsWeight: 1.0,  // Weight for annotation count.
    WaitingWeight:     -3.0, // Penalty for waiting tasks.
},
```

Default weights are used when `Urgency` is zero-valued.

---

## Domain Types Reference

### Task

```go
type Task struct {
    ID             uuid.UUID      // Auto-generated on Create.
    ShortID        string         // 8+ hex chars derived from UUID.
    ParentID       *uuid.UUID     // Optional parent task.
    ProjectID      string         // Project this task belongs to.
    Title          string         // Required.
    Description    string         // Markdown body.
    Status         string         // Governed by workflow.
    Priority       int            // 0 (none) through 4 (urgent).
    Version        int            // Optimistic locking. Starts at 1.
    DueAt          *time.Time     // Optional due date.
    WaitUntil      *time.Time     // Task hidden from "next" until this time.
    RecurrenceRule *string        // RFC 5545 RRULE string.
    UDA            map[string]any // User-defined attributes (string values).
    CreatedAt      time.Time      // Auto-set on Create.
    ModifiedAt     time.Time      // Auto-updated on every mutation.
    ClaimedBy      *string        // Player ID holding the claim.
    ClaimedAt      *time.Time     // When the claim was made.
    Urgency        float64        // Computed at read time, not persisted.
}
```

### Annotation

```go
type Annotation struct {
    ID        uuid.UUID
    TaskID    uuid.UUID
    Body      string
    CreatedAt time.Time
}
```

### Relation

```go
type Relation struct {
    ID           uuid.UUID
    SourceID     uuid.UUID
    TargetID     uuid.UUID
    RelationType string    // "blocks", "relates_to", "duplicates"
    CreatedAt    time.Time
}
```

### Tag

```go
type Tag struct {
    ID    uuid.UUID
    Name  string
    Color *string // Optional hex color.
}

type TagWithUsage struct {
    Tag       Tag
    TaskCount int
}
```

### Player

```go
type Player struct {
    ID           string    // Self-declared unique string.
    Type         string    // "human" or "agent".
    RegisteredAt time.Time
    LastSeenAt   time.Time
}
```

### Project

```go
type Project struct {
    ID         uuid.UUID
    Name       string
    WorkflowID uuid.UUID
    Settings   ProjectSettings
    Version    int
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```

`Name` is the human-readable identifier (previously `ID` in the config-driven era). `WorkflowID` references a `Workflow` row by UUID. `Version` is used for optimistic locking.

### Workflow

```go
type Workflow struct {
    ID          uuid.UUID
    Name        string
    Statuses    map[string]StatusConfig
    Transitions []WorkflowTransition
    Version     int
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

---

## Building-Block Packages

For consumers who need full control, the individual packages are importable directly:

| Package      | Purpose                                                        |
|--------------|----------------------------------------------------------------|
| `domain`     | Core types (`Task`, `TaskUpdate`, `FilterExpr`, errors). No dependencies. |
| `service`    | Business logic (validation, workflow enforcement, urgency scoring). |
| `repository` | Go interfaces only — no implementation.                        |
| `sqlite`     | SQLite implementations of repository interfaces.               |
| `filter`     | 3-stage filter parser: Lex → Parse → Resolve.                 |
| `config`     | Global config types (`StorageConfig`, `UrgencyConfig`, `TUIConfig`, `MCPConfig`) and Viper-based loader. |
| `migrations` | Embedded SQL migration files.                                  |

You can wire these yourself instead of using `Client`:

```go
import (
    "github.com/germanamz/tusk/migrations"
    "github.com/germanamz/tusk/service"
    "github.com/germanamz/tusk/sqlite"
)

store, _ := sqlite.New("/tmp/tusk.db", migrations.FS)
defer store.Close()

db := store.DB()
taskRepo := sqlite.NewTaskRepo(db)
// ... wire other repos and services manually.
```

This is useful for custom storage backends (implement the `repository` interfaces), testing with mocks, or embedding only the parts you need.
