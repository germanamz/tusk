# Declarative Workflows — Phase 3: Clean Up Public API

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the transitional `projectID` parameter from `WorkflowService` method signatures, add new methods (`List`, `GetByName`, `GetWorkflowWithProjects`), and update all call sites.

**Architecture:** `WorkflowService` gains a `projectRepo` dependency for `GetWorkflowWithProjects`. All callers drop `projectID`. The project-to-workflow resolution already happens before workflow calls, so this is purely a signature cleanup.

**Tech Stack:** Go, standard library only

**Prerequisites:** Phase 2 must be complete (SQLite deleted, TaskTxProvider simplified, full compilation green).

---

### Task 1: Rewrite WorkflowService — final API

Drop `projectID` from all method signatures, add `projectRepo` dependency and three new methods.

**Files:**
- Modify: `internal/service/workflow.go` (full rewrite from transitional version)
- Modify: `internal/service/workflow_test.go` (full rewrite)

- [ ] **Step 1: Rewrite `internal/service/workflow_test.go`**

Replace the entire file with:

```go
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/inmem"
)

func testWorkflowEnv(t *testing.T) *WorkflowService {
	t.Helper()
	workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: []string{"pending", "active", "completed", "deleted"},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "pending", To: "deleted"},
				{From: "active", To: "completed"},
				{From: "active", To: "pending"},
				{From: "active", To: "deleted"},
				{From: "completed", To: "pending"},
			},
		},
	})
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {Workflow: "kanban"},
	})
	return NewWorkflowService(workflowRepo, projectRepo)
}

func TestIsTransitionAllowed_Allowed(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	allowed, err := svc.IsTransitionAllowed(ctx, "kanban", "pending", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected pending->active to be allowed")
	}
}

func TestIsTransitionAllowed_Disallowed(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	allowed, err := svc.IsTransitionAllowed(ctx, "kanban", "pending", "completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected pending->completed to be disallowed")
	}
}

func TestIsTransitionAllowed_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	_, err := svc.IsTransitionAllowed(ctx, "nonexistent", "pending", "active")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetStatuses(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	statuses, err := svc.GetStatuses(ctx, "kanban")
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
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	_, err := svc.GetStatuses(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetTransitions(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	transitions, err := svc.GetTransitions(ctx, "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(transitions) != 6 {
		t.Fatalf("expected 6 transitions, got %d", len(transitions))
	}
}

func TestWorkflowList(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	workflows, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(workflows))
	}
	if workflows[0].Name != "kanban" {
		t.Fatalf("expected 'kanban', got %q", workflows[0].Name)
	}
}

func TestGetByName(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	wf, err := svc.GetByName(ctx, "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "kanban" {
		t.Fatalf("expected 'kanban', got %q", wf.Name)
	}
}

func TestGetByName_NotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	_, err := svc.GetByName(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetWorkflowWithProjects(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	wf, projectIDs, err := svc.GetWorkflowWithProjects(ctx, "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "kanban" {
		t.Fatalf("expected 'kanban', got %q", wf.Name)
	}
	if len(projectIDs) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projectIDs))
	}
	if projectIDs[0] != "backend" || projectIDs[1] != "default" {
		t.Fatalf("expected [backend, default], got %v", projectIDs)
	}
}

func TestGetWorkflowWithProjects_NotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	_, _, err := svc.GetWorkflowWithProjects(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Rewrite `internal/service/workflow.go`**

Replace the entire file with:

```go
package service

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
)

// WorkflowService validates status transitions and provides read access
// to workflow definitions from config.
type WorkflowService struct {
	workflowRepo repository.WorkflowRepository
	projectRepo  repository.ProjectRepository
}

// NewWorkflowService creates a new WorkflowService.
func NewWorkflowService(wr repository.WorkflowRepository, pr repository.ProjectRepository) *WorkflowService {
	return &WorkflowService{workflowRepo: wr, projectRepo: pr}
}

// IsTransitionAllowed checks whether a status transition is permitted
// by the named workflow.
func (s *WorkflowService) IsTransitionAllowed(ctx context.Context, workflowName string, from string, to string) (bool, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return false, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	for _, t := range wf.Transitions {
		if t.FromStatus == from && t.ToStatus == to {
			return true, nil
		}
	}
	return false, nil
}

// GetStatuses returns the ordered list of valid statuses for the named workflow.
func (s *WorkflowService) GetStatuses(ctx context.Context, workflowName string) ([]string, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Statuses, nil
}

// GetTransitions returns all allowed transitions for the named workflow.
func (s *WorkflowService) GetTransitions(ctx context.Context, workflowName string) ([]domain.WorkflowTransition, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Transitions, nil
}

// List returns all workflows from config.
func (s *WorkflowService) List(ctx context.Context) ([]*domain.Workflow, error) {
	return s.workflowRepo.List(ctx)
}

// GetByName returns a single workflow by name.
// Returns domain.ErrNotFound if the workflow does not exist.
func (s *WorkflowService) GetByName(ctx context.Context, name string) (*domain.Workflow, error) {
	return s.workflowRepo.GetByName(ctx, name)
}

// GetWorkflowWithProjects returns a workflow and the sorted list of project IDs
// that reference it. Returns domain.ErrNotFound if the workflow does not exist.
func (s *WorkflowService) GetWorkflowWithProjects(ctx context.Context, name string) (*domain.Workflow, []string, error) {
	wf, err := s.workflowRepo.GetByName(ctx, name)
	if err != nil {
		return nil, nil, err
	}

	projects, err := s.projectRepo.List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("listing projects: %w", err)
	}

	var projectIDs []string
	for _, p := range projects {
		if p.Workflow == name {
			projectIDs = append(projectIDs, p.ID)
		}
	}
	return wf, projectIDs, nil
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/service/workflow.go internal/service/workflow_test.go
git commit -m "refactor(service): drop projectID from WorkflowService, add new methods

Remove transitional projectID params. Add List, GetByName, and
GetWorkflowWithProjects. Add projectRepo dependency."
```

---

### Task 2: Update all call sites

Drop `projectID` from all workflow calls in TaskService, MCP, and update `main.go` constructor.

**Files:**
- Modify: `internal/service/task.go` (4 call sites)
- Modify: `internal/mcp/resources.go` (2 call sites)
- Modify: `cmd/tusk/main.go` (constructor call)

- [ ] **Step 1: Update TaskService call sites in `internal/service/task.go`**

Replace in `Create` (around line 98):

```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, task.ProjectID, project.Workflow)
```
With:
```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, project.Workflow)
```

Replace in `Update` (around line 253):

```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, task.ProjectID, project.Workflow, oldStatus, task.Status)
```
With:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, oldStatus, task.Status)
```

Replace in `checkAutoComplete` (around line 473):

```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, cfg.TargetStatus)
```
With:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, parent.Status, cfg.TargetStatus)
```

Replace in `checkAutoRevert` (around line 554):

```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, revertCfg.TargetStatus)
```
With:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, parent.Status, revertCfg.TargetStatus)
```

- [ ] **Step 2: Update MCP resource handler in `internal/mcp/resources.go`**

Replace (around lines 132-137):

```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, project.ID, project.Workflow)
```
With:
```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, project.Workflow)
```

Replace:
```go
	transitions, err := s.workflowSvc.GetTransitions(ctx, project.ID, project.Workflow)
```
With:
```go
	transitions, err := s.workflowSvc.GetTransitions(ctx, project.Workflow)
```

- [ ] **Step 3: Update `cmd/tusk/main.go` constructor**

Replace (around line 63):

```go
	workflowSvc := service.NewWorkflowService(workflowRepo)
```
With:
```go
	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
```

- [ ] **Step 4: Commit**

```bash
git add internal/service/task.go internal/mcp/resources.go cmd/tusk/main.go
git commit -m "refactor: drop projectID from all workflow call sites

Update TaskService, MCP resource handler, and main.go DI to use
the final WorkflowService signatures."
```

---

### Task 3: Update tests and verify

Update task_test.go for the new constructor and run the full suite.

**Files:**
- Modify: `internal/service/task_test.go`

- [ ] **Step 1: Update `testTaskEnvWithSettings`**

Find the line:

```go
	workflowSvc := NewWorkflowService(workflowRepo)
```

Replace with:

```go
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
```

- [ ] **Step 2: Run full test suite**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && make test`

Expected: full compilation PASS, all tests PASS.

- [ ] **Step 3: Run with race detector**

Run: `cd /Users/germanamz/projects/tusk && make test-race`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/service/task_test.go
git commit -m "test(service): update TaskService tests for final WorkflowService API

Pass projectRepo to NewWorkflowService in test setup."
```
