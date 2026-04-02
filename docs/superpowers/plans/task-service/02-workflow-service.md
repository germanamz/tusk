# Phase 2: Implement WorkflowService

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement a thin `WorkflowService` that validates status transitions against project workflows. TaskService will depend on this.

**Prereqs:** None — this phase is independent of Phase 1.

**Files:**
- Modify: `internal/service/workflow.go` (replace the stub — currently just `package service`)
- Create: `internal/service/workflow_test.go`

---

## Background

The `WorkflowService` wraps the `WorkflowRepository` interface to answer two questions:

1. **Is a status transition allowed?** — Given a project, workflow name, and from→to statuses, check if the transition exists in the workflow's transition table.
2. **What statuses are valid?** — Given a project and workflow name, return the ordered list of valid status strings.

The default migration seeds a `_default` project (`00000000-0000-0000-0000-000000000000`) with a `default` workflow that has these transitions:

| From | To |
|---|---|
| pending | active |
| pending | deleted |
| active | completed |
| active | pending |
| active | deleted |
| completed | pending |

The `WorkflowRepository` interface (defined in `internal/repository/workflow.go`) provides:

```go
GetByProjectAndName(ctx, projectID, name) (*domain.Workflow, error)
GetTransitions(ctx, workflowID) ([]*domain.WorkflowTransition, error)
Create(ctx, wf) error
AddTransition(ctx, t) error
```

---

## Task 1: Write failing tests for WorkflowService

**Files:**
- Create: `internal/service/workflow_test.go`

- [ ] **Step 1: Create the test file**

Create `internal/service/workflow_test.go` with the following content:

```go
package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

// defaultProjectID matches the seeded _default project in the migration.
var defaultProjectID = uuid.MustParse("00000000-0000-0000-0000-000000000000")

// testWorkflowService creates a WorkflowService backed by a real in-memory
// SQLite database with all migrations applied (including seed data).
func testWorkflowService(t *testing.T) *WorkflowService {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	workflowRepo := sqlite.NewWorkflowRepo(store.DB())
	return NewWorkflowService(workflowRepo)
}

func TestIsTransitionAllowed_Allowed(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	allowed, err := svc.IsTransitionAllowed(ctx, defaultProjectID, "default", "pending", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected pending→active to be allowed")
	}
}

func TestIsTransitionAllowed_Disallowed(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	allowed, err := svc.IsTransitionAllowed(ctx, defaultProjectID, "default", "pending", "completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected pending→completed to be disallowed")
	}
}

func TestIsTransitionAllowed_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	_, err := svc.IsTransitionAllowed(ctx, defaultProjectID, "nonexistent", "pending", "active")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetStatuses(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	statuses, err := svc.GetStatuses(ctx, defaultProjectID, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"pending", "active", "completed", "deleted"}
	if len(statuses) != len(expected) {
		t.Fatalf("expected %d statuses, got %d", len(expected), len(statuses))
	}
	for i, s := range statuses {
		if s != expected[i] {
			t.Fatalf("status[%d]: expected %q, got %q", i, expected[i], s)
		}
	}
}

func TestGetStatuses_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	_, err := svc.GetStatuses(ctx, defaultProjectID, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestIsTransition|TestGetStatuses" -v`

Expected: **compilation error** — `NewWorkflowService` is not defined yet. The current `internal/service/workflow.go` only contains `package service`. This is the expected failure.

- [ ] **Step 3: Commit the failing tests**

```bash
git add internal/service/workflow_test.go
git commit -m "test(service): add failing WorkflowService tests"
```

---

## Task 2: Implement WorkflowService

**Files:**
- Modify: `internal/service/workflow.go` (replace stub)

- [ ] **Step 1: Read the current file**

Open `internal/service/workflow.go`. It currently contains only:

```go
package service
```

You will replace this with the full implementation.

- [ ] **Step 2: Write the implementation**

Replace the entire contents of `internal/service/workflow.go` with:

```go
package service

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// WorkflowService validates status transitions against project workflows.
type WorkflowService struct {
	workflowRepo repository.WorkflowRepository
}

// NewWorkflowService creates a new WorkflowService.
func NewWorkflowService(wr repository.WorkflowRepository) *WorkflowService {
	return &WorkflowService{workflowRepo: wr}
}

// IsTransitionAllowed checks whether a status transition is permitted by the
// workflow identified by projectID and workflowName.
func (s *WorkflowService) IsTransitionAllowed(ctx context.Context, projectID uuid.UUID, workflowName string, from string, to string) (bool, error) {
	wf, err := s.workflowRepo.GetByProjectAndName(ctx, projectID, workflowName)
	if err != nil {
		return false, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	transitions, err := s.workflowRepo.GetTransitions(ctx, wf.ID)
	if err != nil {
		return false, fmt.Errorf("loading transitions for workflow %q: %w", workflowName, err)
	}

	for _, t := range transitions {
		if t.FromStatus == from && t.ToStatus == to {
			return true, nil
		}
	}
	return false, nil
}

// GetStatuses returns the ordered list of valid statuses for the workflow
// identified by projectID and workflowName.
func (s *WorkflowService) GetStatuses(ctx context.Context, projectID uuid.UUID, workflowName string) ([]string, error) {
	wf, err := s.workflowRepo.GetByProjectAndName(ctx, projectID, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Statuses, nil
}
```

- [ ] **Step 3: Run the tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestIsTransition|TestGetStatuses" -v`

Expected output:

```
=== RUN   TestIsTransitionAllowed_Allowed
--- PASS: TestIsTransitionAllowed_Allowed
=== RUN   TestIsTransitionAllowed_Disallowed
--- PASS: TestIsTransitionAllowed_Disallowed
=== RUN   TestIsTransitionAllowed_WorkflowNotFound
--- PASS: TestIsTransitionAllowed_WorkflowNotFound
=== RUN   TestGetStatuses
--- PASS: TestGetStatuses
=== RUN   TestGetStatuses_WorkflowNotFound
--- PASS: TestGetStatuses_WorkflowNotFound
PASS
```

All 5 tests should PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/service/workflow.go
git commit -m "feat(service): implement WorkflowService with transition validation"
```
