# Declarative Workflows — Phase 1: Bridge In-Memory Repo + Swap DI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a config-backed in-memory `WorkflowRepository` that implements the **current** interface, then swap `main.go` and test setup to use it instead of the SQLite implementation. After this phase, workflows are served from config, the SQLite workflow code becomes dead code, and `go build ./...` passes with all tests green.

**Architecture:** The bridge implementation uses synthetic UUIDs to satisfy the current `GetByProjectAndName`/`GetTransitions` interface. `Create` and `AddTransition` are no-ops. This is ~70 lines of throwaway code that gets replaced in Phase 3 when the interface is simplified.

**Tech Stack:** Go, standard library only

---

### Task 1: Create bridge in-memory WorkflowRepository

Implement the **current** `WorkflowRepository` interface backed by config. The current interface requires UUID-keyed lookups for transitions, so the bridge assigns synthetic UUIDs at construction time.

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

func TestWorkflowRepository_GetByProjectAndName(t *testing.T) {
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
		wf, err := repo.GetByProjectAndName(ctx, "default", "kanban")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if wf.Name != "kanban" {
			t.Errorf("expected Name 'kanban', got %q", wf.Name)
		}
		if len(wf.Statuses) != 4 {
			t.Errorf("expected 4 statuses, got %d", len(wf.Statuses))
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := repo.GetByProjectAndName(ctx, "any", "nonexistent")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestWorkflowRepository_GetTransitions(t *testing.T) {
	workflows := map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: []string{"pending", "active"},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "active", To: "pending"},
			},
		},
	}

	repo := inmem.NewWorkflowRepository(workflows)
	ctx := context.Background()

	wf, _ := repo.GetByProjectAndName(ctx, "default", "kanban")
	transitions, err := repo.GetTransitions(ctx, wf.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(transitions))
	}
	if transitions[0].FromStatus != "pending" || transitions[0].ToStatus != "active" {
		t.Errorf("unexpected transition: %s -> %s", transitions[0].FromStatus, transitions[0].ToStatus)
	}
}

func TestWorkflowRepository_GetTransitions_NotFound(t *testing.T) {
	repo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{})
	ctx := context.Background()

	_, err := repo.GetTransitions(ctx, [16]byte{}) // zero UUID
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Implement `internal/inmem/workflow.go`**

```go
package inmem

import (
	"context"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// Compile-time check that WorkflowRepository implements the interface.
var _ repository.WorkflowRepository = (*WorkflowRepository)(nil)

// WorkflowRepository is a read-only, in-memory implementation of
// repository.WorkflowRepository backed by config data.
// This is a bridge implementation that satisfies the current interface
// (with UUID-keyed lookups). It will be simplified when the interface
// is updated to name-keyed lookups in a later phase.
type WorkflowRepository struct {
	byName      map[string]*domain.Workflow
	transitions map[uuid.UUID][]*domain.WorkflowTransition
}

// NewWorkflowRepository builds an in-memory workflow repository from config.
func NewWorkflowRepository(cfgWorkflows map[string]config.WorkflowConfig) *WorkflowRepository {
	r := &WorkflowRepository{
		byName:      make(map[string]*domain.Workflow, len(cfgWorkflows)),
		transitions: make(map[uuid.UUID][]*domain.WorkflowTransition, len(cfgWorkflows)),
	}
	for name, cfg := range cfgWorkflows {
		id := uuid.New()
		wf := &domain.Workflow{
			ID:       id,
			Name:     name,
			Statuses: make([]string, len(cfg.Statuses)),
		}
		copy(wf.Statuses, cfg.Statuses)
		r.byName[name] = wf

		for _, t := range cfg.Transitions {
			r.transitions[id] = append(r.transitions[id], &domain.WorkflowTransition{
				ID:         uuid.New(),
				WorkflowID: id,
				FromStatus: t.From,
				ToStatus:   t.To,
			})
		}
	}
	return r
}

// GetByProjectAndName returns the workflow with the given name.
// projectID is accepted for interface compatibility but ignored —
// workflows are global in config, not per-project.
func (r *WorkflowRepository) GetByProjectAndName(_ context.Context, _ string, name string) (*domain.Workflow, error) {
	wf, ok := r.byName[name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *wf
	cp.Statuses = make([]string, len(wf.Statuses))
	copy(cp.Statuses, wf.Statuses)
	return &cp, nil
}

// GetTransitions returns the transitions for the workflow with the given ID.
func (r *WorkflowRepository) GetTransitions(_ context.Context, workflowID uuid.UUID) ([]*domain.WorkflowTransition, error) {
	ts, ok := r.transitions[workflowID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return ts, nil
}

// Create is a no-op. Workflows are defined in config.
func (r *WorkflowRepository) Create(_ context.Context, _ *domain.Workflow) error {
	return nil
}

// AddTransition is a no-op. Transitions are defined in config.
func (r *WorkflowRepository) AddTransition(_ context.Context, _ *domain.WorkflowTransition) error {
	return nil
}
```

- [ ] **Step 3: Run tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/inmem/ -run TestWorkflow -v`

Expected: all PASS.

- [ ] **Step 4: Verify full project still compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: PASS. Only new files added — nothing changed.

- [ ] **Step 5: Commit**

```bash
git add internal/inmem/workflow.go internal/inmem/workflow_test.go
git commit -m "feat(inmem): add bridge in-memory WorkflowRepository

Implements the current WorkflowRepository interface backed by config.
Uses synthetic UUIDs for GetTransitions compatibility. Create and
AddTransition are no-ops. Will be simplified when the interface is
updated to name-keyed lookups."
```

---

### Task 2: Swap DI to use in-memory repo

Change `main.go` and `task_test.go` to use the in-memory workflow repo instead of the SQLite one. The SQLite workflow code becomes dead code.

**Files:**
- Modify: `cmd/tusk/main.go:58` (workflow repo creation)
- Modify: `internal/service/task_test.go:33,68` (test setup)

- [ ] **Step 1: Update `cmd/tusk/main.go` line 58**

Replace:

```go
	workflowRepo := sqlite.NewWorkflowRepo(db)
```

With:

```go
	workflowRepo := inmem.NewWorkflowRepository(cfg.Workflows)
```

The `inmem` package is already imported on line 10.

- [ ] **Step 2: Update `testTaskEnvWithSettings` in `internal/service/task_test.go` line 33**

Replace:

```go
	workflowRepo := sqlite.NewWorkflowRepo(db)
```

With:

```go
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
```

- [ ] **Step 3: Update `testTaskEnv` in `internal/service/task_test.go` (lines 54-78)**

Replace the entire function with:

```go
func testTaskEnv(t *testing.T) *testEnv {
	t.Helper()
	return testTaskEnvWithSettings(t, config.ProjectSettingsConfig{})
}
```

- [ ] **Step 4: Run full test suite**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && make test`

Expected: PASS. All tests green — the in-memory repo produces identical results to the seeded SQLite data.

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/main.go internal/service/task_test.go
git commit -m "refactor: swap workflow repo from SQLite to in-memory config-backed impl

main.go and task_test.go now use inmem.WorkflowRepository instead of
sqlite.WorkflowRepo. SQLite workflow code is now dead code."
```
