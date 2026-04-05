# Declarative Workflows — Phase 1: Domain, In-Memory Repo & Transitional Service

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the new workflow infrastructure layer: simplified domain types, read-only repository interface, config-backed in-memory implementation, and a transitional WorkflowService that uses the new repo internally while keeping its old public API. After this phase, the new layer is complete and tested at the package level. The old SQLite workflow code still exists (removed in Phase 2).

**Architecture:** Domain types lose UUID fields. Repository interface becomes read-only (GetByName, List). In-memory implementation mirrors `inmem/project.go`. WorkflowService is rewritten to use the new repo but keeps `projectID` in method signatures (ignored) so callers don't break.

**Tech Stack:** Go, standard library only

**Note:** After this phase, `go build ./...` will not pass because `internal/sqlite/workflow.go` and `internal/service/task_test.go` still reference the old domain types and SQLite workflow repo. Phase 2 deletes the old code and wires everything together. Package-level builds (`go build ./internal/domain/...`, `go build ./internal/inmem/...`) all pass.

---

### Task 1: Rewrite domain types and repository interface

These are two small, tightly coupled files (17 and 15 lines respectively). Change both together since the repo interface references domain types.

**Files:**
- Modify: `internal/domain/workflow.go` (full rewrite)
- Modify: `internal/repository/workflow.go` (full rewrite)

- [ ] **Step 1: Rewrite `internal/domain/workflow.go`**

Replace the entire file contents with:

```go
package domain

// Workflow is a named set of statuses and allowed transitions.
// Workflows are config-driven in-memory entities identified by Name.
type Workflow struct {
	Name        string
	Statuses    []string
	Transitions []WorkflowTransition
}

// WorkflowTransition defines an allowed status change within a workflow.
type WorkflowTransition struct {
	FromStatus string
	ToStatus   string
}
```

- [ ] **Step 2: Rewrite `internal/repository/workflow.go`**

Replace the entire file contents with:

```go
package repository

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
)

// WorkflowRepository provides read-only access to workflow definitions.
// Implementations are backed by config, not a database.
type WorkflowRepository interface {
	// GetByName returns the workflow with the given name.
	// Returns domain.ErrNotFound if no workflow with that name exists.
	GetByName(ctx context.Context, name string) (*domain.Workflow, error)

	// List returns all workflows, sorted alphabetically by name.
	List(ctx context.Context) ([]*domain.Workflow, error)
}
```

- [ ] **Step 3: Verify domain and repository packages compile**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/domain/... && go build ./internal/repository/...`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/workflow.go internal/repository/workflow.go
git commit -m "refactor(domain): simplify Workflow types and WorkflowRepository interface

Remove DB-specific fields (UUID IDs, ProjectID, WorkflowID).
Transitions are now embedded in the Workflow struct.
Repository interface is read-only: GetByName, List."
```

---

### Task 2: Implement in-memory workflow repository

Create `internal/inmem/workflow.go` following the exact pattern of `internal/inmem/project.go`.

**Files:**
- Create: `internal/inmem/workflow.go`
- Create: `internal/inmem/workflow_test.go`

- [ ] **Step 1: Write the test file `internal/inmem/workflow_test.go`**

```go
package inmem_test

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/inmem"
)

func TestWorkflowRepository_GetByName(t *testing.T) {
	workflows := map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: []string{"pending", "active", "completed", "deleted"},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "active", To: "completed"},
			},
		},
		"bug-tracking": {
			Statuses:    []string{"open", "fixed", "closed"},
			Transitions: []config.WorkflowTransitionConfig{{From: "open", To: "fixed"}},
		},
	}

	repo := inmem.NewWorkflowRepository(workflows)
	ctx := context.Background()

	t.Run("existing workflow", func(t *testing.T) {
		wf, err := repo.GetByName(ctx, "kanban")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if wf.Name != "kanban" {
			t.Errorf("expected Name 'kanban', got %q", wf.Name)
		}
		if len(wf.Statuses) != 4 {
			t.Errorf("expected 4 statuses, got %d", len(wf.Statuses))
		}
		if len(wf.Transitions) != 2 {
			t.Errorf("expected 2 transitions, got %d", len(wf.Transitions))
		}
		if wf.Transitions[0].FromStatus != "pending" || wf.Transitions[0].ToStatus != "active" {
			t.Errorf("unexpected first transition: %s -> %s", wf.Transitions[0].FromStatus, wf.Transitions[0].ToStatus)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := repo.GetByName(ctx, "nonexistent")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("returns defensive copy", func(t *testing.T) {
		wf1, _ := repo.GetByName(ctx, "kanban")
		wf1.Name = "mutated"
		wf2, _ := repo.GetByName(ctx, "kanban")
		if wf2.Name != "kanban" {
			t.Errorf("expected 'kanban' after mutation, got %q — defensive copy failed", wf2.Name)
		}
	})
}

func TestWorkflowRepository_List(t *testing.T) {
	workflows := map[string]config.WorkflowConfig{
		"kanban":       {Statuses: []string{"pending", "active"}},
		"bug-tracking": {Statuses: []string{"open", "closed"}},
		"simple":       {Statuses: []string{"todo", "done"}},
	}

	repo := inmem.NewWorkflowRepository(workflows)
	ctx := context.Background()

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 workflows, got %d", len(list))
	}

	// Verify sorted alphabetically by name
	if list[0].Name != "bug-tracking" {
		t.Errorf("expected first 'bug-tracking', got %q", list[0].Name)
	}
	if list[1].Name != "kanban" {
		t.Errorf("expected second 'kanban', got %q", list[1].Name)
	}
	if list[2].Name != "simple" {
		t.Errorf("expected third 'simple', got %q", list[2].Name)
	}
}

func TestWorkflowRepository_EmptyConfig(t *testing.T) {
	repo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{})
	ctx := context.Background()

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 workflows, got %d", len(list))
	}
}
```

- [ ] **Step 2: Implement `internal/inmem/workflow.go`**

```go
package inmem

import (
	"context"
	"sort"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
)

// Compile-time check that WorkflowRepository implements the interface.
var _ repository.WorkflowRepository = (*WorkflowRepository)(nil)

// WorkflowRepository is a read-only, in-memory implementation of
// repository.WorkflowRepository backed by config data.
type WorkflowRepository struct {
	workflows map[string]*domain.Workflow
}

// NewWorkflowRepository builds an in-memory workflow repository from config.
func NewWorkflowRepository(cfgWorkflows map[string]config.WorkflowConfig) *WorkflowRepository {
	workflows := make(map[string]*domain.Workflow, len(cfgWorkflows))
	for name, cfg := range cfgWorkflows {
		wf := &domain.Workflow{
			Name:        name,
			Statuses:    make([]string, len(cfg.Statuses)),
			Transitions: make([]domain.WorkflowTransition, len(cfg.Transitions)),
		}
		copy(wf.Statuses, cfg.Statuses)
		for i, t := range cfg.Transitions {
			wf.Transitions[i] = domain.WorkflowTransition{
				FromStatus: t.From,
				ToStatus:   t.To,
			}
		}
		workflows[name] = wf
	}
	return &WorkflowRepository{workflows: workflows}
}

// GetByName returns a defensive copy of the workflow. Returns domain.ErrNotFound if not found.
func (r *WorkflowRepository) GetByName(_ context.Context, name string) (*domain.Workflow, error) {
	wf, ok := r.workflows[name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return copyWorkflow(wf), nil
}

// List returns all workflows sorted alphabetically by name. Each workflow is a defensive copy.
func (r *WorkflowRepository) List(_ context.Context) ([]*domain.Workflow, error) {
	result := make([]*domain.Workflow, 0, len(r.workflows))
	for _, wf := range r.workflows {
		result = append(result, copyWorkflow(wf))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// copyWorkflow returns a deep copy of a Workflow, including slices.
func copyWorkflow(wf *domain.Workflow) *domain.Workflow {
	cp := *wf
	cp.Statuses = make([]string, len(wf.Statuses))
	copy(cp.Statuses, wf.Statuses)
	cp.Transitions = make([]domain.WorkflowTransition, len(wf.Transitions))
	copy(cp.Transitions, wf.Transitions)
	return &cp
}
```

- [ ] **Step 3: Run tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/inmem/ -run TestWorkflow -v`

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/inmem/workflow.go internal/inmem/workflow_test.go
git commit -m "feat(inmem): add config-backed in-memory WorkflowRepository

Implements the simplified WorkflowRepository interface (GetByName, List)
backed by config.WorkflowConfig map. Returns defensive copies.
Mirrors the pattern from inmem.ProjectRepository."
```

---

### Task 3: Rewrite WorkflowService (transitional)

Rewrite `WorkflowService` to use the new repository interface internally. **Keep the old public method signatures** (`projectID` parameter) so callers in `TaskService` don't need to change yet. Phase 2 removes the old call sites, Phase 3 drops `projectID`.

**Files:**
- Modify: `internal/service/workflow.go` (full rewrite)
- Modify: `internal/service/workflow_test.go` (full rewrite)

- [ ] **Step 1: Rewrite `internal/service/workflow_test.go`**

Replace the entire file with:

```go
package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/inmem"
)

func testWorkflowService(t *testing.T) *WorkflowService {
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
	return NewWorkflowService(workflowRepo)
}

func TestIsTransitionAllowed_Allowed(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	allowed, err := svc.IsTransitionAllowed(ctx, "default", "kanban", "pending", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected pending->active to be allowed")
	}
}

func TestIsTransitionAllowed_Disallowed(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	allowed, err := svc.IsTransitionAllowed(ctx, "default", "kanban", "pending", "completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected pending->completed to be disallowed")
	}
}

func TestIsTransitionAllowed_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	_, err := svc.IsTransitionAllowed(ctx, "default", "nonexistent", "pending", "active")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetStatuses(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	statuses, err := svc.GetStatuses(ctx, "default", "kanban")
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

	_, err := svc.GetStatuses(ctx, "default", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
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

// WorkflowService validates status transitions against workflow definitions.
type WorkflowService struct {
	workflowRepo repository.WorkflowRepository
}

// NewWorkflowService creates a new WorkflowService.
func NewWorkflowService(wr repository.WorkflowRepository) *WorkflowService {
	return &WorkflowService{workflowRepo: wr}
}

// IsTransitionAllowed checks whether a status transition is permitted
// by the named workflow.
// Note: projectID is accepted for backward compatibility but ignored.
// It will be removed in Phase 3 when all call sites are updated.
func (s *WorkflowService) IsTransitionAllowed(ctx context.Context, projectID string, workflowName string, from string, to string) (bool, error) {
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
// Note: projectID is accepted for backward compatibility but ignored.
func (s *WorkflowService) GetStatuses(ctx context.Context, projectID string, workflowName string) ([]string, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Statuses, nil
}

// GetTransitions returns all allowed transitions for the named workflow.
// Note: projectID is accepted for backward compatibility but ignored.
func (s *WorkflowService) GetTransitions(ctx context.Context, projectID string, workflowName string) ([]domain.WorkflowTransition, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Transitions, nil
}
```

- [ ] **Step 3: Run workflow tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestIsTransition|TestGetStatuses" -v`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/service/workflow.go internal/service/workflow_test.go
git commit -m "refactor(service): rewrite WorkflowService for config-driven workflows

Uses new WorkflowRepository.GetByName internally. Keeps old public
API with projectID params as transitional bridge — callers unchanged.
Tests now use inmem.WorkflowRepository."
```
