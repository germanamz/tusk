# Domain Types and Repository Interfaces — Design Spec

**Scope:** v0.1 roadmap item — "Domain types and repository interfaces"
**Date:** 2026-04-01

---

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| UDA representation | `map[string]any` | Simple, flexible. Custom type deferred to v0.5 when schema validation lands. |
| Repo file layout | One file per entity | Matches existing scaffold convention. New files: `workflow.go`, `annotation.go`. |
| Filter struct scope | Full (all spec fields) | Just data — cheap to define now, keeps repo interface stable. |
| Time fields | `time.Time` / `*time.Time` | Idiomatic Go. Storage layer handles string conversion. |
| Struct style | Flat exported fields, no constructors | Spec places all validation in service layer. Domain types are pure data containers. |

---

## Domain Types (`internal/domain/`)

### `task.go`

```go
package domain

import (
    "time"

    "github.com/google/uuid"
)

type Task struct {
    ID             uuid.UUID
    ShortID        string
    ParentID       *uuid.UUID
    ProjectID      *uuid.UUID
    Title          string
    Description    string
    Status         string
    Priority       int
    Version        int
    DueAt          *time.Time
    WaitUntil      *time.Time
    RecurrenceRule *string
    UDA            map[string]any
    CreatedAt      time.Time
    ModifiedAt     time.Time
}
```

- Nullable fields use pointers.
- `UDA` is nil when empty.
- `Version` starts at 1 (enforced by service/storage).

### `annotation.go`

```go
package domain

import (
    "time"

    "github.com/google/uuid"
)

type Annotation struct {
    ID        uuid.UUID
    TaskID    uuid.UUID
    Body      string
    CreatedAt time.Time
}
```

### `relation.go`

```go
package domain

import (
    "time"

    "github.com/google/uuid"
)

type Relation struct {
    ID           uuid.UUID
    SourceID     uuid.UUID
    TargetID     uuid.UUID
    RelationType string
    CreatedAt    time.Time
}
```

`RelationType`: one of `"blocks"`, `"relates_to"`, `"duplicates"` — validated by service.

### `project.go`

```go
package domain

import (
    "time"

    "github.com/google/uuid"
)

type Project struct {
    ID              uuid.UUID
    Name            string
    Description     string
    DefaultWorkflow string
    CreatedAt       time.Time
}
```

### `tag.go`

```go
package domain

import "github.com/google/uuid"

type Tag struct {
    ID    uuid.UUID
    Name  string
    Color *string
}
```

### `workflow.go`

```go
package domain

import "github.com/google/uuid"

type Workflow struct {
    ID        uuid.UUID
    ProjectID uuid.UUID
    Name      string
    Statuses  []string
}

type WorkflowTransition struct {
    ID         uuid.UUID
    WorkflowID uuid.UUID
    FromStatus string
    ToStatus   string
}
```

`Statuses` is `[]string` — JSON column in SQLite gets unmarshaled by the storage layer.

### `errors.go`

```go
package domain

import "errors"

var (
    ErrNotFound          = errors.New("not found")
    ErrConflict          = errors.New("version conflict")
    ErrCyclicBlock       = errors.New("relation would create a cycle in blocks graph")
    ErrInvalidTransition = errors.New("status transition not allowed by workflow")
    ErrDuplicateRelation = errors.New("relation already exists")
)
```

Sentinel errors used with `errors.Is()`.

### `filter.go`

```go
package domain

import (
    "time"

    "github.com/google/uuid"
)

type TaskFilter struct {
    ProjectID   *uuid.UUID
    ParentID    *uuid.UUID
    RootID      *uuid.UUID  // for tree: all descendants
    Statuses    []string     // OR match
    Tags        []string     // include
    ExcludeTags []string     // exclude
    PriorityMin *int
    PriorityMax *int
    DueAfter    *time.Time
    DueBefore   *time.Time
    WaitingOnly *bool        // if true, only tasks with wait_until in future
}
```

All fields optional. Nil/empty = no filter. Repository implementations compose SQL WHERE clauses from non-nil fields.

---

## Repository Interfaces (`internal/repository/`)

### `task.go`

```go
package repository

import (
    "context"

    "github.com/germanamz/tusk/internal/domain"
    "github.com/google/uuid"
)

type TaskRepository interface {
    Create(ctx context.Context, task *domain.Task) error
    GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error)
    GetByShortID(ctx context.Context, shortID string) (*domain.Task, error)
    Update(ctx context.Context, task *domain.Task) error
    Delete(ctx context.Context, id uuid.UUID, version int) error
    List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error)
    GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error)
    GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error)
}
```

- `Update` checks `task.Version` — returns `domain.ErrConflict` if stale.
- `Delete` takes version for the same optimistic locking reason.
- `GetDescendants` is recursive (CTE in SQLite). Used for tree view and completion propagation.

### `relation.go`

```go
package repository

import (
    "context"

    "github.com/germanamz/tusk/internal/domain"
    "github.com/google/uuid"
)

type RelationRepository interface {
    Create(ctx context.Context, rel *domain.Relation) error
    Delete(ctx context.Context, id uuid.UUID) error
    GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
    GetBlocking(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
    GetBlockedBy(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
    Exists(ctx context.Context, sourceID, targetID uuid.UUID, relType string) (bool, error)
}
```

- `GetByTask`: all relations where the task is source or target.
- `GetBlocking` / `GetBlockedBy`: directional queries for urgency scoring and cycle detection.

### `project.go`

```go
package repository

import (
    "context"

    "github.com/germanamz/tusk/internal/domain"
    "github.com/google/uuid"
)

type ProjectRepository interface {
    Create(ctx context.Context, project *domain.Project) error
    GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)
    GetByName(ctx context.Context, name string) (*domain.Project, error)
    List(ctx context.Context) ([]*domain.Project, error)
    Update(ctx context.Context, project *domain.Project) error
    Delete(ctx context.Context, id uuid.UUID) error
}
```

### `tag.go`

```go
package repository

import (
    "context"

    "github.com/germanamz/tusk/internal/domain"
    "github.com/google/uuid"
)

type TagRepository interface {
    Create(ctx context.Context, tag *domain.Tag) error
    GetByName(ctx context.Context, name string) (*domain.Tag, error)
    List(ctx context.Context) ([]*domain.Tag, error)
    AssignToTask(ctx context.Context, taskID, tagID uuid.UUID) error
    RemoveFromTask(ctx context.Context, taskID, tagID uuid.UUID) error
    GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error)
}
```

### `workflow.go` (new file)

```go
package repository

import (
    "context"

    "github.com/germanamz/tusk/internal/domain"
    "github.com/google/uuid"
)

type WorkflowRepository interface {
    GetByProjectAndName(ctx context.Context, projectID uuid.UUID, name string) (*domain.Workflow, error)
    GetTransitions(ctx context.Context, workflowID uuid.UUID) ([]*domain.WorkflowTransition, error)
    Create(ctx context.Context, wf *domain.Workflow) error
    AddTransition(ctx context.Context, t *domain.WorkflowTransition) error
}
```

### `annotation.go` (new file)

```go
package repository

import (
    "context"

    "github.com/germanamz/tusk/internal/domain"
    "github.com/google/uuid"
)

type AnnotationRepository interface {
    Create(ctx context.Context, ann *domain.Annotation) error
    GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Annotation, error)
    Delete(ctx context.Context, id uuid.UUID) error
}
```

---

## Conventions

- Every method takes `context.Context` as first parameter.
- Return `domain.ErrNotFound` for missing entities.
- Return `domain.ErrConflict` when optimistic lock fails.
- No pagination — deferred until list sizes warrant it.
- No transactions exposed in interfaces — each repository method call is atomic (per spec).
