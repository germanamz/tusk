# Declarative Workflows — Phase 3: Simplify Types, Repo Interface & Service Internals

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the bridge implementation with its final form: simplified domain types (no UUIDs), read-only repository interface (`GetByName`, `List`), clean in-memory implementation, and updated `WorkflowService` internals. The `WorkflowService` **keeps its old public API** (`projectID` parameter, accepted but ignored) so callers don't change — Phase 4 drops `projectID`.

**Architecture:** Domain types, repo interface, inmem implementation, and WorkflowService internals are rewritten as a coordinated change. These four files are tightly coupled by Go's type system — they must be updated together for compilation. The public API surface (`IsTransitionAllowed(ctx, projectID, workflowName, from, to)`) stays the same, so `TaskService`, MCP, and `main.go` don't change.

**Tech Stack:** Go, standard library only

**Prerequisites:** Phase 2 must be complete (SQLite deleted, TaskTxProvider simplified).

**Note:** Tasks 1 and 2 in this phase form a single atomic commit. Intermediate state between them may not compile.

---

### Task 1: Rewrite domain + repo + inmem + service internals

Coordinated rewrite of the four tightly-coupled files. The WorkflowService keeps its old public method signatures (with `projectID`) for now.

**Files:**
- Modify: `internal/domain/workflow.go` (full rewrite)
- Modify: `internal/repository/workflow.go` (full rewrite)
- Modify: `internal/inmem/workflow.go` (full rewrite)
- Modify: `internal/inmem/workflow_test.go` (full rewrite)
- Modify: `internal/service/workflow.go` (rewrite internals, keep public API)
- Modify: `internal/service/workflow_test.go` (full rewrite)

- [ ] **Step 1: Rewrite `internal/domain/workflow.go`**

Replace the entire file with:

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

Replace the entire file with:

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

- [ ] **Step 3: Rewrite `internal/inmem/workflow.go`**

Replace the entire file with:

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

// List returns all workflows sorted alphabetically by name. Each is a defensive copy.
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

Note: `NewWorkflowRepository` keeps the same constructor signature (`map[string]config.WorkflowConfig`), so `main.go` doesn't need to change.

- [ ] **Step 4: Rewrite `internal/inmem/workflow_test.go`**

Replace the entire file with:

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
			t.Errorf("defensive copy failed: got %q", wf2.Name)
		}
	})
}

func TestWorkflowRepository_List(t *testing.T) {
	workflows := map[string]config.WorkflowConfig{
		"kanban":       {Statuses: []string{"pending", "active"}},
		"bug-tracking": {Statuses: []string{"open", "closed"}},
	}

	repo := inmem.NewWorkflowRepository(workflows)
	list, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(list))
	}
	if list[0].Name != "bug-tracking" || list[1].Name != "kanban" {
		t.Errorf("expected alphabetical order, got [%s, %s]", list[0].Name, list[1].Name)
	}
}
```

- [ ] **Step 5: Rewrite `internal/service/workflow.go` (keep old public API)**

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
// Phase 4 removes it from the signature.
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
// Return type is []domain.WorkflowTransition (value slice, not pointer slice).
// Callers that iterate and access fields work identically with both types.
func (s *WorkflowService) GetTransitions(ctx context.Context, projectID string, workflowName string) ([]domain.WorkflowTransition, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Transitions, nil
}
```

Important: `GetTransitions` now returns `[]domain.WorkflowTransition` (value slice) instead of `[]*domain.WorkflowTransition` (pointer slice). The only caller (`handleWorkflowResource` in `resources.go`) iterates with range and accesses `.FromStatus`/`.ToStatus` — this works identically for both value and pointer element types.

- [ ] **Step 6: Rewrite `internal/service/workflow_test.go`**

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
	allowed, err := svc.IsTransitionAllowed(context.Background(), "default", "kanban", "pending", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected pending->active to be allowed")
	}
}

func TestIsTransitionAllowed_Disallowed(t *testing.T) {
	svc := testWorkflowService(t)
	allowed, err := svc.IsTransitionAllowed(context.Background(), "default", "kanban", "pending", "completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected pending->completed to be disallowed")
	}
}

func TestIsTransitionAllowed_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowService(t)
	_, err := svc.IsTransitionAllowed(context.Background(), "default", "nonexistent", "pending", "active")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetStatuses(t *testing.T) {
	svc := testWorkflowService(t)
	statuses, err := svc.GetStatuses(context.Background(), "default", "kanban")
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
	_, err := svc.GetStatuses(context.Background(), "default", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}
```

---

### Task 2: Verify full compilation and tests

- [ ] **Step 1: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: PASS. The `GetTransitions` return type change from `[]*domain.WorkflowTransition` to `[]domain.WorkflowTransition` is transparent to `resources.go` because range iteration and field access work identically for both.

- [ ] **Step 2: Run full test suite with race detector**

Run: `cd /Users/germanamz/projects/tusk && make test-race`

Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/workflow.go internal/repository/workflow.go internal/inmem/workflow.go internal/inmem/workflow_test.go internal/service/workflow.go internal/service/workflow_test.go
git commit -m "refactor: simplify workflow types, repo interface, and service internals

Remove UUID fields from domain types. Simplify WorkflowRepository to
GetByName + List. Rewrite inmem and service to use new interface.
Public WorkflowService API unchanged (projectID still accepted)."
```
