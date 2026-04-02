# Domain Types and Repository Interfaces — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Populate `internal/domain/` with Go structs and `internal/repository/` with interfaces so downstream layers (services, SQLite, MCP) can compile against stable types.

**Architecture:** Pure data types in `domain/` with no dependencies beyond `uuid` and stdlib. Repository interfaces in `repository/` depend only on `domain/`. No business logic, no constructors, no storage imports.

**Tech Stack:** Go 1.26, `github.com/google/uuid`

**Spec:** `docs/superpowers/specs/2026-04-01-domain-types-and-repository-interfaces-design.md`

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `internal/domain/errors.go` | Sentinel domain errors |
| Modify | `internal/domain/task.go` | `Task` struct |
| Modify | `internal/domain/annotation.go` | `Annotation` struct |
| Modify | `internal/domain/relation.go` | `Relation` struct |
| Modify | `internal/domain/project.go` | `Project` struct |
| Modify | `internal/domain/tag.go` | `Tag` struct |
| Modify | `internal/domain/workflow.go` | `Workflow` and `WorkflowTransition` structs |
| Modify | `internal/domain/filter.go` | `TaskFilter` struct |
| Create | `internal/domain/domain_test.go` | Compile-time verification for domain package |
| Modify | `internal/repository/task.go` | `TaskRepository` interface |
| Modify | `internal/repository/relation.go` | `RelationRepository` interface |
| Modify | `internal/repository/project.go` | `ProjectRepository` interface |
| Modify | `internal/repository/tag.go` | `TagRepository` interface |
| Create | `internal/repository/workflow.go` | `WorkflowRepository` interface |
| Create | `internal/repository/annotation.go` | `AnnotationRepository` interface |
| Create | `internal/repository/repository_test.go` | Compile-time verification for repository package |

---

### Task 1: Domain Errors

**Files:**
- Modify: `internal/domain/errors.go`

- [ ] **Step 1: Write `errors.go`**

Replace the empty file with sentinel errors:

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/domain/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/domain/errors.go
git commit -m "feat(domain): add sentinel errors"
```

---

### Task 2: Task Struct

**Files:**
- Modify: `internal/domain/task.go`

- [ ] **Step 1: Write `task.go`**

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/domain/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/domain/task.go
git commit -m "feat(domain): add Task struct"
```

---

### Task 3: Annotation Struct

**Files:**
- Modify: `internal/domain/annotation.go`

- [ ] **Step 1: Write `annotation.go`**

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/domain/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/domain/annotation.go
git commit -m "feat(domain): add Annotation struct"
```

---

### Task 4: Relation Struct

**Files:**
- Modify: `internal/domain/relation.go`

- [ ] **Step 1: Write `relation.go`**

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/domain/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/domain/relation.go
git commit -m "feat(domain): add Relation struct"
```

---

### Task 5: Project Struct

**Files:**
- Modify: `internal/domain/project.go`

- [ ] **Step 1: Write `project.go`**

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/domain/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/domain/project.go
git commit -m "feat(domain): add Project struct"
```

---

### Task 6: Tag Struct

**Files:**
- Modify: `internal/domain/tag.go`

- [ ] **Step 1: Write `tag.go`**

```go
package domain

import "github.com/google/uuid"

type Tag struct {
	ID    uuid.UUID
	Name  string
	Color *string
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/domain/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/domain/tag.go
git commit -m "feat(domain): add Tag struct"
```

---

### Task 7: Workflow and WorkflowTransition Structs

**Files:**
- Modify: `internal/domain/workflow.go`

- [ ] **Step 1: Write `workflow.go`**

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/domain/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/domain/workflow.go
git commit -m "feat(domain): add Workflow and WorkflowTransition structs"
```

---

### Task 8: TaskFilter Struct

**Files:**
- Modify: `internal/domain/filter.go`

- [ ] **Step 1: Write `filter.go`**

```go
package domain

import (
	"time"

	"github.com/google/uuid"
)

type TaskFilter struct {
	ProjectID   *uuid.UUID
	ParentID    *uuid.UUID
	RootID      *uuid.UUID // for tree: all descendants
	Statuses    []string   // OR match
	Tags        []string   // include
	ExcludeTags []string   // exclude
	PriorityMin *int
	PriorityMax *int
	DueAfter    *time.Time
	DueBefore   *time.Time
	WaitingOnly *bool // if true, only tasks with wait_until in future
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/domain/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/domain/filter.go
git commit -m "feat(domain): add TaskFilter struct"
```

---

### Task 9: Domain Package Compile Test

**Files:**
- Create: `internal/domain/domain_test.go`

A test that instantiates every type to catch regressions if fields are renamed or removed.

- [ ] **Step 1: Write the test**

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

func TestTypesCompile(t *testing.T) {
	now := time.Now()
	id := uuid.New()
	priority := 3
	waiting := true

	_ = domain.Task{
		ID:             id,
		ShortID:        "a3f8b2c1",
		ParentID:       &id,
		ProjectID:      &id,
		Title:          "test",
		Description:    "desc",
		Status:         "pending",
		Priority:       priority,
		Version:        1,
		DueAt:          &now,
		WaitUntil:      &now,
		RecurrenceRule: nil,
		UDA:            map[string]any{"key": "val"},
		CreatedAt:      now,
		ModifiedAt:     now,
	}

	_ = domain.Annotation{
		ID:        id,
		TaskID:    id,
		Body:      "note",
		CreatedAt: now,
	}

	_ = domain.Relation{
		ID:           id,
		SourceID:     id,
		TargetID:     id,
		RelationType: "blocks",
		CreatedAt:    now,
	}

	_ = domain.Project{
		ID:              id,
		Name:            "backend",
		Description:     "desc",
		DefaultWorkflow: "default",
		CreatedAt:       now,
	}

	color := "#ff0000"
	_ = domain.Tag{
		ID:    id,
		Name:  "bug",
		Color: &color,
	}

	_ = domain.Workflow{
		ID:        id,
		ProjectID: id,
		Name:      "default",
		Statuses:  []string{"pending", "active", "completed", "deleted"},
	}

	_ = domain.WorkflowTransition{
		ID:         id,
		WorkflowID: id,
		FromStatus: "pending",
		ToStatus:   "active",
	}

	_ = domain.TaskFilter{
		ProjectID:   &id,
		ParentID:    &id,
		RootID:      &id,
		Statuses:    []string{"pending"},
		Tags:        []string{"bug"},
		ExcludeTags: []string{"docs"},
		PriorityMin: &priority,
		PriorityMax: &priority,
		DueAfter:    &now,
		DueBefore:   &now,
		WaitingOnly: &waiting,
	}
}

func TestSentinelErrors(t *testing.T) {
	errors := []error{
		domain.ErrNotFound,
		domain.ErrConflict,
		domain.ErrCyclicBlock,
		domain.ErrInvalidTransition,
		domain.ErrDuplicateRelation,
	}
	for _, err := range errors {
		if err == nil {
			t.Fatal("sentinel error is nil")
		}
		if err.Error() == "" {
			t.Fatal("sentinel error has empty message")
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/domain/ -v`
Expected: `PASS` — both `TestTypesCompile` and `TestSentinelErrors` pass.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/domain_test.go
git commit -m "test(domain): add compile-time verification test"
```

---

### Task 10: TaskRepository Interface

**Files:**
- Modify: `internal/repository/task.go`

- [ ] **Step 1: Write `task.go`**

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/repository/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/repository/task.go
git commit -m "feat(repository): add TaskRepository interface"
```

---

### Task 11: RelationRepository Interface

**Files:**
- Modify: `internal/repository/relation.go`

- [ ] **Step 1: Write `relation.go`**

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/repository/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/repository/relation.go
git commit -m "feat(repository): add RelationRepository interface"
```

---

### Task 12: ProjectRepository Interface

**Files:**
- Modify: `internal/repository/project.go`

- [ ] **Step 1: Write `project.go`**

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/repository/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/repository/project.go
git commit -m "feat(repository): add ProjectRepository interface"
```

---

### Task 13: TagRepository Interface

**Files:**
- Modify: `internal/repository/tag.go`

- [ ] **Step 1: Write `tag.go`**

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/repository/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/repository/tag.go
git commit -m "feat(repository): add TagRepository interface"
```

---

### Task 14: WorkflowRepository Interface

**Files:**
- Create: `internal/repository/workflow.go`

- [ ] **Step 1: Write `workflow.go`**

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/repository/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/repository/workflow.go
git commit -m "feat(repository): add WorkflowRepository interface"
```

---

### Task 15: AnnotationRepository Interface

**Files:**
- Create: `internal/repository/annotation.go`

- [ ] **Step 1: Write `annotation.go`**

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

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/repository/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/repository/annotation.go
git commit -m "feat(repository): add AnnotationRepository interface"
```

---

### Task 16: Repository Package Compile Test

**Files:**
- Create: `internal/repository/repository_test.go`

A test that verifies all interfaces are satisfiable by asserting a nil pointer of a mock struct implements each interface.

- [ ] **Step 1: Write the test**

```go
package repository_test

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// Minimal stubs — just enough to prove the interfaces compile.

type stubTaskRepo struct{}

func (s *stubTaskRepo) Create(_ context.Context, _ *domain.Task) error                    { return nil }
func (s *stubTaskRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Task, error)      { return nil, nil }
func (s *stubTaskRepo) GetByShortID(_ context.Context, _ string) (*domain.Task, error)    { return nil, nil }
func (s *stubTaskRepo) Update(_ context.Context, _ *domain.Task) error                    { return nil }
func (s *stubTaskRepo) Delete(_ context.Context, _ uuid.UUID, _ int) error                { return nil }
func (s *stubTaskRepo) List(_ context.Context, _ domain.TaskFilter) ([]*domain.Task, error) { return nil, nil }
func (s *stubTaskRepo) GetChildren(_ context.Context, _ uuid.UUID) ([]*domain.Task, error)   { return nil, nil }
func (s *stubTaskRepo) GetDescendants(_ context.Context, _ uuid.UUID) ([]*domain.Task, error) { return nil, nil }

type stubRelationRepo struct{}

func (s *stubRelationRepo) Create(_ context.Context, _ *domain.Relation) error                          { return nil }
func (s *stubRelationRepo) Delete(_ context.Context, _ uuid.UUID) error                                 { return nil }
func (s *stubRelationRepo) GetByTask(_ context.Context, _ uuid.UUID) ([]*domain.Relation, error)        { return nil, nil }
func (s *stubRelationRepo) GetBlocking(_ context.Context, _ uuid.UUID) ([]*domain.Relation, error)      { return nil, nil }
func (s *stubRelationRepo) GetBlockedBy(_ context.Context, _ uuid.UUID) ([]*domain.Relation, error)     { return nil, nil }
func (s *stubRelationRepo) Exists(_ context.Context, _, _ uuid.UUID, _ string) (bool, error)            { return false, nil }

type stubProjectRepo struct{}

func (s *stubProjectRepo) Create(_ context.Context, _ *domain.Project) error                 { return nil }
func (s *stubProjectRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Project, error)   { return nil, nil }
func (s *stubProjectRepo) GetByName(_ context.Context, _ string) (*domain.Project, error)    { return nil, nil }
func (s *stubProjectRepo) List(_ context.Context) ([]*domain.Project, error)                 { return nil, nil }
func (s *stubProjectRepo) Update(_ context.Context, _ *domain.Project) error                 { return nil }
func (s *stubProjectRepo) Delete(_ context.Context, _ uuid.UUID) error                       { return nil }

type stubTagRepo struct{}

func (s *stubTagRepo) Create(_ context.Context, _ *domain.Tag) error                        { return nil }
func (s *stubTagRepo) GetByName(_ context.Context, _ string) (*domain.Tag, error)           { return nil, nil }
func (s *stubTagRepo) List(_ context.Context) ([]*domain.Tag, error)                        { return nil, nil }
func (s *stubTagRepo) AssignToTask(_ context.Context, _, _ uuid.UUID) error                 { return nil }
func (s *stubTagRepo) RemoveFromTask(_ context.Context, _, _ uuid.UUID) error               { return nil }
func (s *stubTagRepo) GetTaskTags(_ context.Context, _ uuid.UUID) ([]*domain.Tag, error)    { return nil, nil }

type stubWorkflowRepo struct{}

func (s *stubWorkflowRepo) GetByProjectAndName(_ context.Context, _ uuid.UUID, _ string) (*domain.Workflow, error)       { return nil, nil }
func (s *stubWorkflowRepo) GetTransitions(_ context.Context, _ uuid.UUID) ([]*domain.WorkflowTransition, error)          { return nil, nil }
func (s *stubWorkflowRepo) Create(_ context.Context, _ *domain.Workflow) error                                           { return nil }
func (s *stubWorkflowRepo) AddTransition(_ context.Context, _ *domain.WorkflowTransition) error                          { return nil }

type stubAnnotationRepo struct{}

func (s *stubAnnotationRepo) Create(_ context.Context, _ *domain.Annotation) error                     { return nil }
func (s *stubAnnotationRepo) GetByTask(_ context.Context, _ uuid.UUID) ([]*domain.Annotation, error)   { return nil, nil }
func (s *stubAnnotationRepo) Delete(_ context.Context, _ uuid.UUID) error                              { return nil }

func TestInterfaceSatisfaction(t *testing.T) {
	var _ repository.TaskRepository = (*stubTaskRepo)(nil)
	var _ repository.RelationRepository = (*stubRelationRepo)(nil)
	var _ repository.ProjectRepository = (*stubProjectRepo)(nil)
	var _ repository.TagRepository = (*stubTagRepo)(nil)
	var _ repository.WorkflowRepository = (*stubWorkflowRepo)(nil)
	var _ repository.AnnotationRepository = (*stubAnnotationRepo)(nil)
}
```

- [ ] **Step 2: Run the test**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/repository/ -v`
Expected: `PASS` — `TestInterfaceSatisfaction` passes.

- [ ] **Step 3: Commit**

```bash
git add internal/repository/repository_test.go
git commit -m "test(repository): add interface satisfaction test"
```

---

### Task 17: Full Build and Test Verification

- [ ] **Step 1: Run all tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/domain/ ./internal/repository/ -v`
Expected: All tests pass.

- [ ] **Step 2: Build the project**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`
Expected: no errors. Other packages (`service/`, `sqlite/`, `mcp/`, `tui/`) still compile because they only have `package` declarations.

- [ ] **Step 3: Run vet**

Run: `cd /Users/germanamz/projects/tusk && go vet ./internal/domain/ ./internal/repository/`
Expected: no output (clean).
