# TaskService Design Spec

**Scope:** TaskService with CRUD, workflow validation, optimistic locking (v0.1 roadmap item)

---

## Decisions

- Tasks without an explicit `project_id` are auto-assigned to the `_default` project (`00000000-0000-0000-0000-000000000000`). Every task always has a project and workflow.
- `short_id` generation: TaskService generates UUID, takes first 8 hex chars, checks for collision via repo, extends to 9+ chars if needed.
- Full validation at the service boundary (title, priority, parent existence, project existence, workflow status).
- Partial updates via a `TaskUpdate` struct with pointer fields (nil = don't change).
- Annotations are owned by TaskService (no separate AnnotationService).
- TaskService depends on WorkflowService for status transition validation (not WorkflowRepo directly).
- `Delete` is a soft delete (status transition to `"deleted"` via workflow), not a row removal.
- Single `TaskService` file — no CQRS split.

---

## New Domain Type: TaskUpdate

Added to `internal/domain/task.go`:

```go
type TaskUpdate struct {
    ShortID        string          // required — identifies the task
    Version        int             // required — optimistic locking
    Title          *string
    Description    *string
    Status         *string
    Priority       *int
    ParentID       **uuid.UUID     // nil = don't change, non-nil sets or clears
    ProjectID      **uuid.UUID
    DueAt          **time.Time
    WaitUntil      **time.Time
    RecurrenceRule **string
    UDA            *map[string]any
}
```

Double-pointer pattern for nullable fields distinguishes three states: don't change (outer nil), set to null (outer non-nil, inner nil), set to value (both non-nil).

---

## WorkflowService

File: `internal/service/workflow.go`

Minimal implementation to unblock TaskService.

```go
type WorkflowService struct {
    workflowRepo repository.WorkflowRepository
}

func NewWorkflowService(wr repository.WorkflowRepository) *WorkflowService
```

### Methods

**`IsTransitionAllowed(ctx, projectID, workflowName, from, to) (bool, error)`**
Fetches workflow by project+name, gets transitions, checks if (from, to) pair exists.

**`GetStatuses(ctx, projectID, workflowName) ([]string, error)`**
Returns the ordered list of valid statuses for the workflow.

No caching — workflows change rarely and SQLite reads are fast.

---

## TaskService

File: `internal/service/task.go`

```go
type TaskService struct {
    taskRepo       repository.TaskRepository
    annotationRepo repository.AnnotationRepository
    projectRepo    repository.ProjectRepository
    workflowSvc    *WorkflowService
}

func NewTaskService(
    tr repository.TaskRepository,
    ar repository.AnnotationRepository,
    pr repository.ProjectRepository,
    ws *WorkflowService,
) *TaskService
```

Package-level constant:

```go
var DefaultProjectID = uuid.MustParse("00000000-0000-0000-0000-000000000000")
```

### Create

```go
func (s *TaskService) Create(ctx context.Context, task *domain.Task) error
```

Steps:
1. Validate title non-empty
2. Validate priority 0-4
3. If `ProjectID` is nil, set to `DefaultProjectID`
4. Validate project exists via `projectRepo.GetByID`
5. If `ParentID` set, validate parent exists via `taskRepo.GetByID`
6. If `Status` empty, default to `"pending"`. Validate status is in workflow's statuses list via `workflowSvc.GetStatuses()`
7. Generate UUID, take first 8 hex chars for short_id, check collision via `taskRepo.GetByShortID`, extend if needed
8. Set `Version = 1`, `CreatedAt = now`, `ModifiedAt = now`, initialize `UDA` to empty map if nil
9. Call `taskRepo.Create`

### Read Operations

```go
func (s *TaskService) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error)
func (s *TaskService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error)
func (s *TaskService) List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error)
func (s *TaskService) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error)
func (s *TaskService) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error)
```

All thin pass-throughs to the repo layer. Reads don't need business logic at this stage.

### Update

```go
func (s *TaskService) Update(ctx context.Context, upd domain.TaskUpdate) (*domain.Task, error)
```

Returns the updated task (callers get the new version number).

Steps:
1. Load current task via `taskRepo.GetByShortID(upd.ShortID)`
2. Early version check: if `task.Version != upd.Version`, return `domain.ErrConflict`
3. Apply patch: for each non-nil field in `TaskUpdate`, overwrite corresponding field
4. Validate patched state (title non-empty, priority 0-4, project exists if changed, parent exists if changed and is not the task itself)
5. If status changed, look up project via `projectRepo.GetByID` to get `DefaultWorkflow` name, then call `workflowSvc.IsTransitionAllowed()`. Return `domain.ErrInvalidTransition` if disallowed
6. Set `ModifiedAt = now`
7. Call `taskRepo.Update(task)` (repo handles version increment and authoritative version check)
8. Re-read via `taskRepo.GetByID(task.ID)` and return

### Convenience Transitions

```go
func (s *TaskService) Start(ctx context.Context, shortID string, version int) (*domain.Task, error)
func (s *TaskService) Complete(ctx context.Context, shortID string, version int) (*domain.Task, error)
func (s *TaskService) Delete(ctx context.Context, shortID string, version int) (*domain.Task, error)
```

Each builds a `TaskUpdate` with the appropriate status and delegates to `Update`:
- `Start` sets status to `"active"`
- `Complete` sets status to `"completed"`
- `Delete` sets status to `"deleted"` (soft delete)

### Annotations

```go
func (s *TaskService) Annotate(ctx context.Context, taskShortID string, body string) (*domain.Annotation, error)
```
1. Validate body non-empty
2. Look up task by short_id (confirms existence)
3. Generate UUID, set `CreatedAt = now`
4. Call `annotationRepo.Create`
5. Return the annotation

```go
func (s *TaskService) GetAnnotations(ctx context.Context, taskShortID string) ([]*domain.Annotation, error)
```
1. Look up task by short_id
2. Call `annotationRepo.GetByTask(task.ID)`

```go
func (s *TaskService) DeleteAnnotation(ctx context.Context, annotationID uuid.UUID) error
```
Direct pass-through to `annotationRepo.Delete`.

### Helper

```go
func ptr[T any](v T) *T {
    return &v
}
```

Unexported generic helper for pointer construction in convenience methods.

---

## Error Strategy

- Domain sentinel errors (`ErrNotFound`, `ErrConflict`, `ErrInvalidTransition`) returned as-is
- Validation errors as `fmt.Errorf` with descriptive messages:
  - `"title must not be empty"`
  - `"priority must be between 0 and 4"`
  - `fmt.Errorf("parent task not found: %w", domain.ErrNotFound)` — wraps sentinel so callers can `errors.Is()` check while the message distinguishes context
- No custom validation error type at this stage

---

## Testing Strategy

Integration tests using real SQLite repos (in-memory DB via `testStore`), matching the existing test patterns in `internal/sqlite/`.

### Files

- `internal/service/workflow_test.go`
- `internal/service/task_test.go`

### WorkflowService Tests

- `IsTransitionAllowed` — allowed returns true, disallowed returns false
- `GetStatuses` — returns correct list, error for nonexistent workflow

### TaskService Tests

**Create:**
- Happy path (all fields set, version=1, project=default)
- Empty title rejected
- Priority out of range rejected
- Invalid parent rejected
- Invalid project rejected
- Initial status validated against workflow
- Short_id generated correctly

**Read:**
- `GetByShortID` found and not found
- `List` with filters end-to-end

**Update:**
- Partial update (one field changed, others preserved)
- Version conflict returns `ErrConflict`
- Allowed status transition passes
- Disallowed status transition returns `ErrInvalidTransition`
- Validation enforced (e.g., title can't be emptied)
- Returns updated task with bumped version

**Convenience transitions:**
- `Start`, `Complete`, `Delete` happy path
- Invalid transition rejected

**Annotations:**
- `Annotate` happy path, empty body rejected, task not found
- `GetAnnotations` with results and empty
- `DeleteAnnotation` happy path and not found

---

## File Summary

| File | Change |
|---|---|
| `internal/domain/task.go` | Add `TaskUpdate` struct |
| `internal/service/workflow.go` | New: `WorkflowService` |
| `internal/service/workflow_test.go` | New: WorkflowService integration tests |
| `internal/service/task.go` | Replace stub: full `TaskService` implementation |
| `internal/service/task_test.go` | New: TaskService integration tests |
