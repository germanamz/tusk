# Phase 2: RelationService Implementation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `RelationService` with `Add` (including cycle detection for `blocks`), `Remove`, and `GetByTask` methods.

**Architecture:** The service resolves short IDs to UUIDs via `TaskRepository`, validates relation types, runs DFS-based cycle detection inside a transaction for `blocks` relations, and delegates persistence to `RelationRepository`.

**Tech Stack:** Go, `database/sql`, SQLite (in-memory for tests)

**Prerequisites:** Phase 1 must be complete (DBTX interface, WithTx, WithRelationTx all in place).

**Design spec:** `docs/superpowers/specs/2026-04-03-relation-service-design.md` (Sections 2 and 3)

**Key files to understand before starting:**
- `internal/service/task.go` — reference for service patterns (constructor injection, error handling, `ptr[T]` helper)
- `internal/service/task_test.go` — reference for test environment setup (`testTaskEnv`)
- `internal/repository/relation.go` — the `RelationRepository` interface your service will call
- `internal/domain/errors.go` — sentinel errors: `ErrNotFound`, `ErrCyclicBlock`, `ErrDuplicateRelation`
- `internal/domain/relation.go` — the `Relation` struct

---

### Task 1: RelationService Struct, Constructor, and `Add` (Non-Blocks Path)

**Files:**
- Modify: `internal/service/relation.go` (currently a stub with only `package service`)
- Create: `internal/service/relation_test.go`

- [ ] **Step 1: Write the test file with test environment helper**

Create `internal/service/relation_test.go`:

```go
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

type testRelationEnv struct {
	relationSvc *RelationService
	taskSvc     *TaskService
	store       *sqlite.Store
}

// testRelationEnv creates a fully wired test environment for RelationService tests.
// The DB has all migrations applied, including the _default project and default workflow.
func newTestRelationEnv(t *testing.T) *testRelationEnv {
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
	relationRepo := sqlite.NewRelationRepo(db)

	workflowSvc := NewWorkflowService(workflowRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)
	relationSvc := NewRelationService(relationRepo, taskRepo, store)

	return &testRelationEnv{
		relationSvc: relationSvc,
		taskSvc:     taskSvc,
		store:       store,
	}
}

// createTask is a helper that creates a task with the given title and returns it.
func (e *testRelationEnv) createTask(t *testing.T, title string) *domain.Task {
	t.Helper()
	task := &domain.Task{Title: title}
	if err := e.taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("creating task %q: %v", title, err)
	}
	return task
}

func TestRelationAdd(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	rel, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if rel.SourceID != taskA.ID {
		t.Errorf("SourceID = %v, want %v", rel.SourceID, taskA.ID)
	}
	if rel.TargetID != taskB.ID {
		t.Errorf("TargetID = %v, want %v", rel.TargetID, taskB.ID)
	}
	if rel.RelationType != "relates_to" {
		t.Errorf("RelationType = %q, want %q", rel.RelationType, "relates_to")
	}
	if rel.ID.String() == "" {
		t.Error("expected non-empty ID")
	}
	if rel.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestRelationAddInvalidType(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "depends_on")
	if err == nil {
		t.Fatal("expected error for invalid relation type")
	}
}

func TestRelationAddTaskNotFound(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, "nonexist", "relates_to")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRelationAddDuplicate(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}

	_, err = env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if !errors.Is(err, domain.ErrDuplicateRelation) {
		t.Fatalf("expected ErrDuplicateRelation, got: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run:
```bash
go test -v ./internal/service -run TestRelationAdd
```
Expected: Compile error — `NewRelationService`, `RelationService`, and `Add` don't exist yet.

- [ ] **Step 3: Implement the service struct, constructor, and `Add` method**

Replace the contents of `internal/service/relation.go` with:

```go
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// validRelationTypes defines the allowed relation type strings.
var validRelationTypes = map[string]bool{
	"blocks":     true,
	"relates_to": true,
	"duplicates": true,
}

// RelationTxProvider gives the service a way to run relation operations
// inside a database transaction without importing a concrete storage package.
// The SQLite Store implements this via its WithRelationTx method.
type RelationTxProvider interface {
	WithRelationTx(ctx context.Context, fn func(rr repository.RelationRepository) error) error
}

// RelationService implements relation business logic including validation
// and cycle detection for "blocks" relations.
type RelationService struct {
	relationRepo repository.RelationRepository
	taskRepo     repository.TaskRepository
	txProvider   RelationTxProvider
}

// NewRelationService creates a new RelationService with the given dependencies.
//   - rr: for non-transactional reads (GetByTask, Remove lookups)
//   - tr: to resolve short IDs to full task UUIDs
//   - txp: for atomic cycle-check + insert on "blocks" relations
func NewRelationService(
	rr repository.RelationRepository,
	tr repository.TaskRepository,
	txp RelationTxProvider,
) *RelationService {
	return &RelationService{
		relationRepo: rr,
		taskRepo:     tr,
		txProvider:   txp,
	}
}

// Add creates a new relation between two tasks identified by short IDs.
//
// For "blocks" relations, the creation is wrapped in a transaction with
// cycle detection (see checkCycle). For other types, no cycle check is needed.
//
// Returns the created Relation or an error:
//   - domain.ErrNotFound if either task short ID doesn't exist
//   - domain.ErrCyclicBlock if adding a "blocks" relation would create a cycle
//   - domain.ErrDuplicateRelation if the exact relation already exists
//   - a validation error if relType is not one of: blocks, relates_to, duplicates
func (s *RelationService) Add(ctx context.Context, sourceShortID, targetShortID, relType string) (*domain.Relation, error) {
	if !validRelationTypes[relType] {
		return nil, fmt.Errorf("invalid relation type %q: must be one of blocks, relates_to, duplicates", relType)
	}

	source, err := s.taskRepo.GetByShortID(ctx, sourceShortID)
	if err != nil {
		return nil, fmt.Errorf("resolving source task: %w", err)
	}

	target, err := s.taskRepo.GetByShortID(ctx, targetShortID)
	if err != nil {
		return nil, fmt.Errorf("resolving target task: %w", err)
	}

	rel := &domain.Relation{
		ID:           uuid.New(),
		SourceID:     source.ID,
		TargetID:     target.ID,
		RelationType: relType,
		CreatedAt:    time.Now().UTC().Truncate(time.Millisecond),
	}

	if relType == "blocks" {
		// Cycle check + insert must be atomic
		if err := s.txProvider.WithRelationTx(ctx, func(txRepo repository.RelationRepository) error {
			if err := s.checkCycle(ctx, txRepo, source.ID, target.ID); err != nil {
				return err
			}
			return txRepo.Create(ctx, rel)
		}); err != nil {
			return nil, err
		}
		return rel, nil
	}

	// Non-blocks: no cycle concern, insert directly
	if err := s.relationRepo.Create(ctx, rel); err != nil {
		return nil, err
	}
	return rel, nil
}

// checkCycle performs a DFS from targetID following outgoing "blocks" edges.
// If it reaches sourceID, that means inserting sourceID->targetID would form a cycle.
//
// Must be called inside a transaction so that no concurrent writer can insert
// a conflicting edge between the check and the subsequent insert.
func (s *RelationService) checkCycle(ctx context.Context, repo repository.RelationRepository, sourceID, targetID uuid.UUID) error {
	visited := map[uuid.UUID]bool{}
	stack := []uuid.UUID{targetID}

	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if current == sourceID {
			return domain.ErrCyclicBlock
		}
		if visited[current] {
			continue
		}
		visited[current] = true

		blocking, err := repo.GetBlocking(ctx, current)
		if err != nil {
			return fmt.Errorf("checking cycle: %w", err)
		}
		for _, rel := range blocking {
			if !visited[rel.TargetID] {
				stack = append(stack, rel.TargetID)
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
go test -v ./internal/service -run TestRelationAdd
```
Expected: All four tests pass (`TestRelationAdd`, `TestRelationAddInvalidType`, `TestRelationAddTaskNotFound`, `TestRelationAddDuplicate`).

- [ ] **Step 5: Commit**

```bash
git add internal/service/relation.go internal/service/relation_test.go
git commit -m "feat(service): add RelationService with Add and cycle detection

Implements Add method with type validation, short ID resolution,
transactional cycle detection for blocks relations, and direct
insert for non-blocks types."
```

---

### Task 2: Cycle Detection Tests

**Files:**
- Modify: `internal/service/relation_test.go`

- [ ] **Step 1: Write cycle detection tests**

Add these tests to `internal/service/relation_test.go`:

```go
func TestRelationAddBlocksSelfReference(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCyclicBlock) {
		t.Fatalf("expected ErrCyclicBlock for self-reference, got: %v", err)
	}
}

func TestRelationAddBlocksDirectCycle(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// A blocks B — should succeed
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// B blocks A — should fail (cycle: A->B->A)
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCyclicBlock) {
		t.Fatalf("expected ErrCyclicBlock for B blocks A, got: %v", err)
	}
}

func TestRelationAddBlocksTransitiveCycle(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")
	taskC := env.createTask(t, "Task C")

	// A blocks B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// B blocks C
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskC.ShortID, "blocks")
	if err != nil {
		t.Fatalf("B blocks C: %v", err)
	}

	// C blocks A — should fail (cycle: A->B->C->A)
	_, err = env.relationSvc.Add(ctx, taskC.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCyclicBlock) {
		t.Fatalf("expected ErrCyclicBlock for C blocks A, got: %v", err)
	}
}

func TestRelationAddBlocksNoCycle(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")
	taskC := env.createTask(t, "Task C")

	// A blocks B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// B blocks C — should succeed (A->B->C is a chain, not a cycle)
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskC.ShortID, "blocks")
	if err != nil {
		t.Fatalf("B blocks C: %v", err)
	}
}

func TestRelationNonBlocksAllowsBidirectional(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// A relates_to B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("A relates_to B: %v", err)
	}

	// B relates_to A — should succeed (no cycle check for non-blocks)
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskA.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("B relates_to A: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests**

Run:
```bash
go test -v ./internal/service -run "TestRelationAddBlocks|TestRelationNonBlocks"
```
Expected: All five tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/service/relation_test.go
git commit -m "test(service): add cycle detection tests for RelationService

Covers self-reference, direct cycle, transitive cycle, valid chain,
and bidirectional non-blocks relations."
```

---

### Task 3: `Remove` Method

**Files:**
- Modify: `internal/service/relation.go`
- Modify: `internal/service/relation_test.go`

- [ ] **Step 1: Write tests for Remove**

Add to `internal/service/relation_test.go`:

```go
func TestRelationRemove(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// Create a relation
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Remove it
	err = env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Verify it's gone
	rels, err := env.relationSvc.GetByTask(ctx, taskA.ShortID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("expected 0 relations after remove, got %d", len(rels))
	}
}

func TestRelationRemoveNotFound(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// Try to remove a relation that doesn't exist
	err := env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run:
```bash
go test -v ./internal/service -run TestRelationRemove
```
Expected: Compile error — `Remove` method doesn't exist yet.

- [ ] **Step 3: Implement `Remove`**

Add this method to `internal/service/relation.go`:

```go
// Remove deletes an existing relation between two tasks.
//
// It finds the relation by matching (sourceID, targetID, relType) among the
// source task's relations, then deletes by the relation's ID.
//
// Returns domain.ErrNotFound if the relation doesn't exist or if either
// task short ID is invalid.
func (s *RelationService) Remove(ctx context.Context, sourceShortID, targetShortID, relType string) error {
	source, err := s.taskRepo.GetByShortID(ctx, sourceShortID)
	if err != nil {
		return fmt.Errorf("resolving source task: %w", err)
	}

	target, err := s.taskRepo.GetByShortID(ctx, targetShortID)
	if err != nil {
		return fmt.Errorf("resolving target task: %w", err)
	}

	// Find the relation by scanning all relations for the source task
	rels, err := s.relationRepo.GetByTask(ctx, source.ID)
	if err != nil {
		return fmt.Errorf("loading relations: %w", err)
	}

	for _, rel := range rels {
		if rel.SourceID == source.ID && rel.TargetID == target.ID && rel.RelationType == relType {
			return s.relationRepo.Delete(ctx, rel.ID)
		}
	}

	return domain.ErrNotFound
}
```

- [ ] **Step 4: Run to verify they pass**

Run:
```bash
go test -v ./internal/service -run TestRelationRemove
```
Expected: Both tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/service/relation.go internal/service/relation_test.go
git commit -m "feat(service): add RelationService.Remove method

Resolves short IDs, finds the matching relation, and deletes it.
Returns ErrNotFound if the relation doesn't exist."
```

---

### Task 4: `GetByTask` Method

**Files:**
- Modify: `internal/service/relation.go`
- Modify: `internal/service/relation_test.go`

- [ ] **Step 1: Write tests for GetByTask**

Add to `internal/service/relation_test.go`:

```go
func TestRelationGetByTask(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")
	taskC := env.createTask(t, "Task C")

	// A blocks B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// C relates_to A
	_, err = env.relationSvc.Add(ctx, taskC.ShortID, taskA.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("C relates_to A: %v", err)
	}

	// GetByTask for A should return both relations
	rels, err := env.relationSvc.GetByTask(ctx, taskA.ShortID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if len(rels) != 2 {
		t.Fatalf("expected 2 relations, got %d", len(rels))
	}
}

func TestRelationGetByTaskNotFound(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	_, err := env.relationSvc.GetByTask(ctx, "nonexist")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRelationGetByTaskEmpty(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")

	rels, err := env.relationSvc.GetByTask(ctx, taskA.ShortID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("expected 0 relations, got %d", len(rels))
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run:
```bash
go test -v ./internal/service -run TestRelationGetByTask
```
Expected: Compile error — `GetByTask` method doesn't exist yet.

- [ ] **Step 3: Implement `GetByTask`**

Add to `internal/service/relation.go`:

```go
// GetByTask returns all relations involving a task (as source or target).
// The task is identified by short ID.
func (s *RelationService) GetByTask(ctx context.Context, shortID string) ([]*domain.Relation, error) {
	task, err := s.taskRepo.GetByShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}
	return s.relationRepo.GetByTask(ctx, task.ID)
}
```

- [ ] **Step 4: Run to verify they pass**

Run:
```bash
go test -v ./internal/service -run TestRelationGetByTask
```
Expected: All three tests pass.

- [ ] **Step 5: Run the full test suite**

Run:
```bash
make test
```
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/service/relation.go internal/service/relation_test.go
git commit -m "feat(service): add RelationService.GetByTask method

Returns all relations where the given task appears as source or target.
Resolves short ID to UUID before querying."
```
