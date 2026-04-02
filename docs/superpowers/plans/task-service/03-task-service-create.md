# Phase 3: TaskService Struct, Constructor, and Create Method

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the `TaskService` struct, its constructor, and the `Create` method with full validation.

**Prereqs:** Phase 1 (TaskUpdate struct) and Phase 2 (WorkflowService) must be complete.

**Files:**
- Modify: `internal/service/task.go` (replace the stub — currently just `package service`)
- Create: `internal/service/task_test.go`

---

## Background

### What `Create` does

The `Create` method is the main entry point for creating new tasks. It handles:

1. **Input validation** — title must be non-empty, priority must be 0–4
2. **Default project assignment** — if no project is specified, assign to the `_default` project (UUID `00000000-0000-0000-0000-000000000000`)
3. **Reference validation** — if a `ProjectID` or `ParentID` is given, verify they exist in the database
4. **Status validation** — default to `"pending"`, verify the status is valid for the project's workflow
5. **ID generation** — generate a UUID and derive an 8-char short ID (extend on collision)
6. **Metadata** — set `Version = 1`, timestamps, initialize UDA map

### Dependencies

`TaskService` depends on these interfaces (defined in `internal/repository/`):

- `TaskRepository` — task CRUD
- `AnnotationRepository` — annotation CRUD (used by annotation methods in later phases)
- `ProjectRepository` — project lookups for validation
- `WorkflowService` — status validation (implemented in Phase 2)

### How the test infrastructure works

Tests use a real in-memory SQLite database. The setup is:

```go
store, _ := sqlite.New(":memory:", migrations.FS)  // creates DB with all tables + seed data
taskRepo := sqlite.NewTaskRepo(store.DB())
annotationRepo := sqlite.NewAnnotationRepo(store.DB())
projectRepo := sqlite.NewProjectRepo(store.DB())
workflowRepo := sqlite.NewWorkflowRepo(store.DB())
workflowSvc := NewWorkflowService(workflowRepo)
taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)
```

The migration seeds a `_default` project and a `default` workflow with statuses `["pending", "active", "completed", "deleted"]`.

---

## Task 1: Create test file with helpers

**Files:**
- Create: `internal/service/task_test.go`

- [ ] **Step 1: Create the test file with test infrastructure**

Create `internal/service/task_test.go`:

```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

// testEnv holds all the services and repos needed for TaskService tests.
type testEnv struct {
	taskSvc     *TaskService
	workflowSvc *WorkflowService
	store       *sqlite.Store
}

// testTaskEnv creates a fully wired test environment with an in-memory SQLite DB.
// The DB has all migrations applied, including the _default project and default workflow.
func testTaskEnv(t *testing.T) *testEnv {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)

	workflowSvc := NewWorkflowService(workflowRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)

	return &testEnv{
		taskSvc:     taskSvc,
		workflowSvc: workflowSvc,
		store:       store,
	}
}

// newMinimalTask returns a Task with only the required fields set.
// The service's Create method will fill in ID, ShortID, Version, timestamps,
// and default ProjectID.
func newMinimalTask(title string) *domain.Task {
	return &domain.Task{
		Title: title,
	}
}

// mustCreateTask creates a task through the service or fails the test.
func mustCreateTask(t *testing.T, svc *TaskService, task *domain.Task) {
	t.Helper()
	if err := svc.Create(context.Background(), task); err != nil {
		t.Fatalf("mustCreateTask: %v", err)
	}
}
```

- [ ] **Step 2: Verify the file is syntactically valid**

Run: `cd /Users/germanamz/projects/tusk && go vet ./internal/service/...`

Expected: no errors. All imports (`context`, `testing`, `time`, `domain`, `sqlite`, `migrations`, `uuid`) are used by the helpers and test functions. The `errors` package is not imported yet — it will be added in Phase 4 when the first `errors.Is` assertion is needed.

- [ ] **Step 3: Commit**

```bash
git add internal/service/task_test.go
git commit -m "test(service): add TaskService test helpers and infrastructure"
```

---

## Task 2: Write failing tests for `Create`

**Files:**
- Modify: `internal/service/task_test.go` (append tests)

- [ ] **Step 1: Add Create tests**

Append to `internal/service/task_test.go`:

```go
func TestCreate_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("My first task")
	if err := env.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify the service populated all required fields
	if task.ID == uuid.Nil {
		t.Fatal("expected non-nil ID")
	}
	if task.ShortID == "" {
		t.Fatal("expected non-empty ShortID")
	}
	if len(task.ShortID) < 8 {
		t.Fatalf("expected ShortID length >= 8, got %d", len(task.ShortID))
	}
	if task.Version != 1 {
		t.Fatalf("expected version 1, got %d", task.Version)
	}
	if task.Status != "pending" {
		t.Fatalf("expected status 'pending', got %q", task.Status)
	}
	if task.ProjectID == nil {
		t.Fatal("expected ProjectID to be set to default")
	}
	if *task.ProjectID != defaultProjectID {
		t.Fatalf("expected default project ID, got %s", task.ProjectID)
	}
	if task.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
	if task.ModifiedAt.IsZero() {
		t.Fatal("expected ModifiedAt to be set")
	}
	if task.UDA == nil {
		t.Fatal("expected UDA to be initialized")
	}

	// Verify it's actually persisted by reading it back
	got, err := env.taskSvc.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.Title != "My first task" {
		t.Fatalf("expected title 'My first task', got %q", got.Title)
	}
}

func TestCreate_EmptyTitle(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("")
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestCreate_PriorityTooHigh(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Bad priority")
	task.Priority = 5
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for priority > 4")
	}
}

func TestCreate_PriorityNegative(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Negative priority")
	task.Priority = -1
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for negative priority")
	}
}

func TestCreate_InvalidParent(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	nonexistent := uuid.New()
	task := newMinimalTask("Orphan task")
	task.ParentID = &nonexistent
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for nonexistent parent")
	}
}

func TestCreate_InvalidProject(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	nonexistent := uuid.New()
	task := newMinimalTask("Bad project")
	task.ProjectID = &nonexistent
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestCreate_InvalidInitialStatus(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Bad status")
	task.Status = "nonexistent_status"
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for invalid initial status")
	}
}

func TestCreate_ValidParent(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)

	child := newMinimalTask("Child")
	child.ParentID = &parent.ID
	if err := env.taskSvc.Create(ctx, child); err != nil {
		t.Fatalf("Create child: %v", err)
	}
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Fatal("expected child to reference parent")
	}
}

func TestCreate_DefaultsToDefaultProject(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("No project set")
	if err := env.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if task.ProjectID == nil || *task.ProjectID != defaultProjectID {
		t.Fatalf("expected default project, got %v", task.ProjectID)
	}
}

func TestCreate_WithAllFields(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	due := now.Add(24 * time.Hour)
	wait := now.Add(1 * time.Hour)
	rrule := "FREQ=DAILY;COUNT=5"
	projID := defaultProjectID

	task := &domain.Task{
		Title:          "Full task",
		Description:    "All fields populated",
		Status:         "pending",
		Priority:       3,
		ProjectID:      &projID,
		DueAt:          &due,
		WaitUntil:      &wait,
		RecurrenceRule: &rrule,
		UDA:            map[string]any{"custom": "value"},
	}

	if err := env.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := env.taskSvc.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.Description != "All fields populated" {
		t.Fatalf("expected description preserved, got %q", got.Description)
	}
	if got.Priority != 3 {
		t.Fatalf("expected priority 3, got %d", got.Priority)
	}
	if got.DueAt == nil {
		t.Fatal("expected DueAt to be set")
	}
	if got.RecurrenceRule == nil || *got.RecurrenceRule != rrule {
		t.Fatal("expected RecurrenceRule to be preserved")
	}
}
```

**Note:** The `TestCreate_HappyPath` test calls `env.taskSvc.GetByShortID` which won't exist yet. That's fine — it will cause a compilation error. We'll implement `GetByShortID` alongside `Create` in the next task since `Create` needs `generateShortID` which uses the same repo method.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run TestCreate -v`

Expected: **compilation error** — `NewTaskService`, `Create`, `GetByShortID` are not defined. This is the expected failure.

- [ ] **Step 3: Commit**

```bash
git add internal/service/task_test.go
git commit -m "test(service): add failing TaskService.Create tests"
```

---

## Task 3: Implement TaskService struct, constructor, Create, and GetByShortID

**Files:**
- Modify: `internal/service/task.go` (replace stub)

- [ ] **Step 1: Read the current file**

Open `internal/service/task.go`. It currently contains only:

```go
package service
```

You will replace this with the full implementation.

- [ ] **Step 2: Write the implementation**

Replace the entire contents of `internal/service/task.go` with:

```go
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// DefaultProjectID is the UUID of the seeded _default project.
// Tasks created without an explicit ProjectID are assigned to this project.
var DefaultProjectID = uuid.MustParse("00000000-0000-0000-0000-000000000000")

// TaskService implements task business logic including validation,
// workflow enforcement, and optimistic locking.
type TaskService struct {
	taskRepo       repository.TaskRepository
	annotationRepo repository.AnnotationRepository
	projectRepo    repository.ProjectRepository
	workflowSvc    *WorkflowService
}

// NewTaskService creates a new TaskService with the given dependencies.
func NewTaskService(
	tr repository.TaskRepository,
	ar repository.AnnotationRepository,
	pr repository.ProjectRepository,
	ws *WorkflowService,
) *TaskService {
	return &TaskService{
		taskRepo:       tr,
		annotationRepo: ar,
		projectRepo:    pr,
		workflowSvc:    ws,
	}
}

// Create validates and persists a new task. It populates the task's ID,
// ShortID, Version, timestamps, and default ProjectID before saving.
func (s *TaskService) Create(ctx context.Context, task *domain.Task) error {
	// Validate title
	if strings.TrimSpace(task.Title) == "" {
		return fmt.Errorf("title must not be empty")
	}

	// Validate priority
	if task.Priority < 0 || task.Priority > 4 {
		return fmt.Errorf("priority must be between 0 and 4")
	}

	// Assign default project if not set
	if task.ProjectID == nil {
		task.ProjectID = &DefaultProjectID
	}

	// Validate project exists
	project, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("project not found: %w", err)
		}
		return fmt.Errorf("looking up project: %w", err)
	}

	// Validate parent exists
	if task.ParentID != nil {
		_, err := s.taskRepo.GetByID(ctx, *task.ParentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("parent task not found: %w", err)
			}
			return fmt.Errorf("looking up parent task: %w", err)
		}
	}

	// Default and validate status
	if task.Status == "" {
		task.Status = "pending"
	}
	statuses, err := s.workflowSvc.GetStatuses(ctx, *task.ProjectID, project.DefaultWorkflow)
	if err != nil {
		return fmt.Errorf("loading workflow statuses: %w", err)
	}
	validStatus := false
	for _, st := range statuses {
		if st == task.Status {
			validStatus = true
			break
		}
	}
	if !validStatus {
		return fmt.Errorf("status %q is not valid for workflow %q", task.Status, project.DefaultWorkflow)
	}

	// Generate ID and ShortID
	task.ID = uuid.New()
	shortID, err := s.generateShortID(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("generating short ID: %w", err)
	}
	task.ShortID = shortID

	// Set metadata
	now := time.Now().UTC().Truncate(time.Millisecond)
	task.Version = 1
	task.CreatedAt = now
	task.ModifiedAt = now
	if task.UDA == nil {
		task.UDA = map[string]any{}
	}

	return s.taskRepo.Create(ctx, task)
}

// GetByShortID retrieves a task by its short ID.
func (s *TaskService) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error) {
	return s.taskRepo.GetByShortID(ctx, shortID)
}

// generateShortID derives a short ID from the task's UUID.
// It starts with 8 hex characters and extends if a collision is detected.
func (s *TaskService) generateShortID(ctx context.Context, id uuid.UUID) (string, error) {
	hex := strings.ReplaceAll(id.String(), "-", "")
	for length := 8; length <= len(hex); length++ {
		candidate := hex[:length]
		_, err := s.taskRepo.GetByShortID(ctx, candidate)
		if errors.Is(err, domain.ErrNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		// Collision — try longer prefix
	}
	return "", fmt.Errorf("could not generate unique short ID")
}

// ptr is a generic helper that returns a pointer to the given value.
func ptr[T any](v T) *T {
	return &v
}
```

- [ ] **Step 3: Run the Create tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run TestCreate -v`

Expected output — all 10 tests PASS:

```
=== RUN   TestCreate_HappyPath
--- PASS: TestCreate_HappyPath
=== RUN   TestCreate_EmptyTitle
--- PASS: TestCreate_EmptyTitle
=== RUN   TestCreate_PriorityTooHigh
--- PASS: TestCreate_PriorityTooHigh
=== RUN   TestCreate_PriorityNegative
--- PASS: TestCreate_PriorityNegative
=== RUN   TestCreate_InvalidParent
--- PASS: TestCreate_InvalidParent
=== RUN   TestCreate_InvalidProject
--- PASS: TestCreate_InvalidProject
=== RUN   TestCreate_InvalidInitialStatus
--- PASS: TestCreate_InvalidInitialStatus
=== RUN   TestCreate_ValidParent
--- PASS: TestCreate_ValidParent
=== RUN   TestCreate_DefaultsToDefaultProject
--- PASS: TestCreate_DefaultsToDefaultProject
=== RUN   TestCreate_WithAllFields
--- PASS: TestCreate_WithAllFields
PASS
```

- [ ] **Step 4: Run the full test suite so far**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -v`

Expected: all service tests pass (5 workflow + 10 create = 15 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/service/task.go internal/service/task_test.go
git commit -m "feat(service): implement TaskService struct, constructor, and Create method"
```
