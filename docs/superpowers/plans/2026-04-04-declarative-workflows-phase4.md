# Declarative Workflows — Phase 4: Drop projectID from Public API

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the transitional `projectID` parameter from `WorkflowService` method signatures, add new methods (`List`, `GetByName`, `GetWorkflowWithProjects`), add `projectRepo` dependency, and update ALL call sites and tests in a single atomic commit.

**Architecture:** `WorkflowService` gains a `projectRepo` dependency. All callers drop `projectID`. This is the final API that Phase 5 (CLI/MCP) builds on.

**Tech Stack:** Go, standard library only

**Prerequisites:** Phase 3 must be complete (simplified types and repo in place).

**Note:** Tasks 1 and 2 form a single atomic commit. The constructor signature change and call site updates must happen together.

---

### Task 1: Rewrite WorkflowService + update all call sites

Change the constructor, drop `projectID` from method signatures, add new methods, and update every caller in the same step.

**Files:**
- Modify: `internal/service/workflow.go` (full rewrite — final API)
- Modify: `internal/service/workflow_test.go` (full rewrite)
- Modify: `internal/service/task.go` (4 call sites)
- Modify: `internal/service/task_test.go` (constructor call)
- Modify: `internal/mcp/resources.go` (2 call sites)
- Modify: `cmd/tusk/main.go` (constructor call)

- [ ] **Step 1: Rewrite `internal/service/workflow.go`**

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

- [ ] **Step 2: Update call sites in `internal/service/task.go`**

In `Create` (around line 98), replace:
```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, task.ProjectID, project.Workflow)
```
With:
```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, project.Workflow)
```

In `Update` (around line 253), replace:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, task.ProjectID, project.Workflow, oldStatus, task.Status)
```
With:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, oldStatus, task.Status)
```

In `checkAutoComplete` (around line 473), replace:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, cfg.TargetStatus)
```
With:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, parent.Status, cfg.TargetStatus)
```

In `checkAutoRevert` (around line 554), replace:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, revertCfg.TargetStatus)
```
With:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, parent.Status, revertCfg.TargetStatus)
```

- [ ] **Step 3: Update call sites in `internal/mcp/resources.go`**

In `handleWorkflowResource` (around line 132), replace:
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

- [ ] **Step 4: Update constructor in `cmd/tusk/main.go`**

Replace (around line 63):
```go
	workflowSvc := service.NewWorkflowService(workflowRepo)
```
With:
```go
	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
```

- [ ] **Step 5: Update constructor in `internal/service/task_test.go`**

In `testTaskEnvWithSettings`, replace:
```go
	workflowSvc := NewWorkflowService(workflowRepo)
```
With:
```go
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
```

---

### Task 2: Rewrite workflow tests + verify

Update workflow service tests for the final API and run the full suite.

**Files:**
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
	allowed, err := svc.IsTransitionAllowed(context.Background(), "kanban", "pending", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected pending->active to be allowed")
	}
}

func TestIsTransitionAllowed_Disallowed(t *testing.T) {
	svc := testWorkflowEnv(t)
	allowed, err := svc.IsTransitionAllowed(context.Background(), "kanban", "pending", "completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected pending->completed to be disallowed")
	}
}

func TestIsTransitionAllowed_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	_, err := svc.IsTransitionAllowed(context.Background(), "nonexistent", "pending", "active")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetStatuses(t *testing.T) {
	svc := testWorkflowEnv(t)
	statuses, err := svc.GetStatuses(context.Background(), "kanban")
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
	_, err := svc.GetStatuses(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetTransitions(t *testing.T) {
	svc := testWorkflowEnv(t)
	transitions, err := svc.GetTransitions(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(transitions) != 6 {
		t.Fatalf("expected 6 transitions, got %d", len(transitions))
	}
}

func TestWorkflowList(t *testing.T) {
	svc := testWorkflowEnv(t)
	workflows, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workflows) != 1 || workflows[0].Name != "kanban" {
		t.Fatalf("expected [kanban], got %v", workflows)
	}
}

func TestGetByName(t *testing.T) {
	svc := testWorkflowEnv(t)
	wf, err := svc.GetByName(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "kanban" {
		t.Fatalf("expected 'kanban', got %q", wf.Name)
	}
}

func TestGetByName_NotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	_, err := svc.GetByName(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetWorkflowWithProjects(t *testing.T) {
	svc := testWorkflowEnv(t)
	wf, projectIDs, err := svc.GetWorkflowWithProjects(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "kanban" {
		t.Fatalf("expected 'kanban', got %q", wf.Name)
	}
	if len(projectIDs) != 2 || projectIDs[0] != "backend" || projectIDs[1] != "default" {
		t.Fatalf("expected [backend, default], got %v", projectIDs)
	}
}

func TestGetWorkflowWithProjects_NotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	_, _, err := svc.GetWorkflowWithProjects(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Verify full compilation and tests**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && make test-race`

Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/service/workflow.go internal/service/workflow_test.go internal/service/task.go internal/service/task_test.go internal/mcp/resources.go cmd/tusk/main.go
git commit -m "refactor: drop projectID from WorkflowService, add new methods

Final WorkflowService API: IsTransitionAllowed, GetStatuses,
GetTransitions (no projectID), plus List, GetByName, and
GetWorkflowWithProjects. All call sites updated."
```
