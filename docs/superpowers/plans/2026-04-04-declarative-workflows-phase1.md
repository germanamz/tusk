# Declarative Workflows — Phase 1: Domain, Repository & In-Memory Implementation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the workflow domain types and repository interface with config-driven equivalents, implement an in-memory repository backed by config, and remove the SQLite workflow implementation.

**Architecture:** Workflows become simple structs identified by name (no UUIDs). The repository interface becomes read-only (`GetByName`, `List`). An in-memory implementation backed by `config.WorkflowConfig` replaces the SQLite implementation. This mirrors the existing pattern in `internal/inmem/project.go`.

**Tech Stack:** Go, standard library only (no new dependencies)

---

### Task 1: Rewrite domain types

Remove DB artifacts from `domain.Workflow` and `domain.WorkflowTransition`. Transitions move from a separate entity to an embedded slice on `Workflow`.

**Files:**
- Modify: `internal/domain/workflow.go` (full rewrite, currently 17 lines)

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

This removes `ID uuid.UUID`, `ProjectID string` from `Workflow` and `ID uuid.UUID`, `WorkflowID uuid.UUID` from `WorkflowTransition`. The `uuid` import is also removed.

- [ ] **Step 2: Verify the file compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/domain/...`

Expected: compilation errors in other packages that reference the removed fields (`internal/sqlite/workflow.go`, `internal/service/workflow.go`, `internal/service/task_test.go`, `internal/sqlite/workflow_test.go`). This is expected — those files are updated in later tasks/phases. The domain package itself should compile cleanly.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/workflow.go
git commit -m "refactor(domain): simplify Workflow and WorkflowTransition types

Remove DB-specific fields (UUID IDs, ProjectID, WorkflowID) in
preparation for config-driven in-memory workflows. Transitions
are now embedded in the Workflow struct."
```

---

### Task 2: Rewrite repository interface

Simplify `WorkflowRepository` to a read-only, name-keyed interface. Remove all write methods and the composite `(projectID, name)` lookup.

**Files:**
- Modify: `internal/repository/workflow.go` (full rewrite, currently 15 lines)

- [ ] **Step 1: Rewrite `internal/repository/workflow.go`**

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

This removes `GetByProjectAndName`, `GetTransitions`, `Create`, and `AddTransition`. The `uuid` import is also removed.

- [ ] **Step 2: Verify the interface file compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/repository/...`

Expected: PASS (the repository package has no dependencies on other internal packages beyond domain).

- [ ] **Step 3: Commit**

```bash
git add internal/repository/workflow.go
git commit -m "refactor(repository): simplify WorkflowRepository to read-only interface

Replace (projectID, name) composite lookup with name-only GetByName.
Remove write methods (Create, AddTransition) and GetTransitions —
transitions are now embedded in the Workflow struct."
```

---

### Task 3: Implement in-memory workflow repository

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

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/inmem/ -run TestWorkflow -v`

Expected: compilation failure — `inmem.NewWorkflowRepository` does not exist yet.

- [ ] **Step 3: Implement `internal/inmem/workflow.go`**

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

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/inmem/ -run TestWorkflow -v`

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/inmem/workflow.go internal/inmem/workflow_test.go
git commit -m "feat(inmem): add config-backed in-memory WorkflowRepository

Implements the simplified WorkflowRepository interface (GetByName, List)
backed by config.WorkflowConfig map. Returns defensive copies.
Mirrors the pattern from inmem.ProjectRepository."
```

---

### Task 4: Delete SQLite workflow implementation and clean up migration

Remove the SQLite workflow repo, its tests, and the workflow tables from the migration.

**Files:**
- Delete: `internal/sqlite/workflow.go`
- Delete: `internal/sqlite/workflow_test.go`
- Modify: `internal/sqlite/store.go:93-94` (remove `Tx.Workflows()`)
- Modify: `migrations/001_initial.up.sql:65-96` (remove workflow tables and seed data)
- Modify: `migrations/001_initial.down.sql:1-2` (remove workflow table drops)

- [ ] **Step 1: Delete `internal/sqlite/workflow.go`**

```bash
rm internal/sqlite/workflow.go
```

- [ ] **Step 2: Delete `internal/sqlite/workflow_test.go`**

```bash
rm internal/sqlite/workflow_test.go
```

- [ ] **Step 3: Remove `Tx.Workflows()` from `internal/sqlite/store.go`**

Remove lines 93-94:

```go
// Workflows returns a WorkflowRepo operating within this transaction.
func (t *Tx) Workflows() *WorkflowRepo { return NewWorkflowRepo(t.tx) }
```

- [ ] **Step 4: Rewrite `migrations/001_initial.up.sql`**

Remove everything from line 65 onwards (the workflow tables comment, `CREATE TABLE workflows`, `CREATE TABLE workflow_transitions`, and all seed `INSERT` statements). The file should end after the `tag_assignments` index on line 63.

The final file should contain only: tasks, annotations, relations, tags, and tag_assignments tables with their indexes. No workflow tables. No seed data.

- [ ] **Step 5: Rewrite `migrations/001_initial.down.sql`**

Remove the first two lines that drop workflow tables:

```sql
DROP TABLE IF EXISTS workflow_transitions;
DROP TABLE IF EXISTS workflows;
```

The file should start with `DROP TABLE IF EXISTS tag_assignments;`.

- [ ] **Step 6: Verify the SQLite package compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/sqlite/...`

Expected: compilation errors related to `WithTaskTx` signature (still references `repository.WorkflowRepository`). This is expected — Phase 2 Task 1 will fix it.

- [ ] **Step 7: Verify SQLite tests pass (excluding store-level integration)**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -run "TestTask|TestAnnotation|TestRelation|TestTag" -v`

Expected: PASS. Workflow tests are deleted, remaining tests should work since the migration no longer creates workflow tables but that doesn't affect other tables.

- [ ] **Step 8: Commit**

```bash
git add -u internal/sqlite/workflow.go internal/sqlite/workflow_test.go internal/sqlite/store.go migrations/001_initial.up.sql migrations/001_initial.down.sql
git commit -m "refactor(sqlite): remove workflow tables and SQLite implementation

Delete WorkflowRepo, its tests, and Tx.Workflows(). Remove workflow
and workflow_transitions tables from the migration. Workflows are
now config-driven via inmem.WorkflowRepository."
```
