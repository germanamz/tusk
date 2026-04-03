# Phase 1: Transaction Infrastructure

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add transaction support to the SQLite storage layer so that multiple repository operations can run atomically within a single database transaction.

**Architecture:** Introduce a `DBTX` interface that both `*sql.DB` and `*sql.Tx` satisfy. Refactor all repository constructors to accept `DBTX` instead of `*sql.DB`. Add a `Tx` type and `WithTx` method on `Store` for scoped transactional access.

**Tech Stack:** Go, `database/sql`, SQLite

**Context for newcomers:** This project uses a layered architecture where SQLite repository implementations (in `internal/sqlite/`) each hold a `*sql.DB` field and use `ExecContext`, `QueryContext`, and `QueryRowContext` to run SQL. Currently there is no way to share a transaction across repositories. We need this for Phase 2, where cycle detection and relation insertion must be atomic.

**Design spec:** `docs/superpowers/specs/2026-04-03-relation-service-design.md` (Section 1)

---

### Task 1: Add `DBTX` Interface and Refactor All Repos

**Files:**
- Modify: `internal/sqlite/store.go` (add `DBTX` interface after line 22, before `Store` struct)
- Modify: `internal/sqlite/task.go:20-26` (change `*sql.DB` to `DBTX`)
- Modify: `internal/sqlite/relation.go:24-31` (change `*sql.DB` to `DBTX`)
- Modify: `internal/sqlite/annotation.go:17-29` (change `*sql.DB` to `DBTX`)
- Modify: `internal/sqlite/project.go:18-30` (change `*sql.DB` to `DBTX`)
- Modify: `internal/sqlite/tag.go:13-19` (change `*sql.DB` to `DBTX`)
- Modify: `internal/sqlite/workflow.go:13-19` (change `*sql.DB` to `DBTX`)

- [ ] **Step 1: Add the `DBTX` interface to `internal/sqlite/store.go`**

Open `internal/sqlite/store.go`. Add this interface **before** the `Store` struct definition (before line 26). This interface captures the three methods that `*sql.DB` and `*sql.Tx` both implement — they are the only methods our repos use.

```go
// DBTX is the common interface between *sql.DB and *sql.Tx.
// All repository implementations use only these three methods, so they can
// operate on either a raw connection pool or an active transaction.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
```

You will also need to add `"context"` to the import block in `store.go`.

- [ ] **Step 2: Refactor `TaskRepo` in `internal/sqlite/task.go`**

Change the struct field and constructor parameter from `*sql.DB` to `DBTX`:

```go
type TaskRepo struct {
	db DBTX
}

func NewTaskRepo(db DBTX) *TaskRepo {
	return &TaskRepo{db: db}
}
```

This is safe because `*sql.DB` implements `DBTX`, so all existing callers (like `main.go` calling `sqlite.NewTaskRepo(db)`) still compile.

- [ ] **Step 3: Refactor `RelationRepo` in `internal/sqlite/relation.go`**

Same change:

```go
type RelationRepo struct {
	db DBTX
}

func NewRelationRepo(db DBTX) *RelationRepo {
	return &RelationRepo{db: db}
}
```

- [ ] **Step 4: Refactor `AnnotationRepo` in `internal/sqlite/annotation.go`**

Same change:

```go
type AnnotationRepo struct {
	db DBTX
}

func NewAnnotationRepo(db DBTX) *AnnotationRepo {
	return &AnnotationRepo{db: db}
}
```

- [ ] **Step 5: Refactor `ProjectRepo` in `internal/sqlite/project.go`**

Same change:

```go
type ProjectRepo struct {
	db DBTX
}

func NewProjectRepo(db DBTX) *ProjectRepo {
	return &ProjectRepo{db: db}
}
```

- [ ] **Step 6: Refactor `TagRepo` in `internal/sqlite/tag.go`**

Same change:

```go
type TagRepo struct {
	db DBTX
}

func NewTagRepo(db DBTX) *TagRepo {
	return &TagRepo{db: db}
}
```

- [ ] **Step 7: Refactor `WorkflowRepo` in `internal/sqlite/workflow.go`**

Same change:

```go
type WorkflowRepo struct {
	db DBTX
}

func NewWorkflowRepo(db DBTX) *WorkflowRepo {
	return &WorkflowRepo{db: db}
}
```

- [ ] **Step 8: Verify everything compiles**

Run:
```bash
go build ./...
```
Expected: No errors. All existing code passes `*sql.DB` which implements `DBTX`.

- [ ] **Step 9: Run all tests to confirm no regressions**

Run:
```bash
make test
```
Expected: All tests pass. This was a type-widening refactor — no behavior changed.

- [ ] **Step 10: Commit**

```bash
git add internal/sqlite/store.go internal/sqlite/task.go internal/sqlite/relation.go internal/sqlite/annotation.go internal/sqlite/project.go internal/sqlite/tag.go internal/sqlite/workflow.go
git commit -m "refactor(sqlite): introduce DBTX interface for transaction support

Widen all repo constructors from *sql.DB to DBTX interface. This
allows repos to operate on either a connection pool or an active
transaction. No behavior change."
```

---

### Task 2: Add `Tx` Type and `WithTx` Method

**Files:**
- Modify: `internal/sqlite/store.go` (add `Tx` struct and `WithTx` method after the `Close()` method, around line 60)

- [ ] **Step 1: Write a test for `WithTx`**

Create `internal/sqlite/tx_test.go`:

```go
package sqlite_test

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

func TestWithTxCommit(t *testing.T) {
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Insert a tag inside a transaction
	err = store.WithTx(ctx, func(tx *sqlite.Tx) error {
		tagRepo := tx.Tags()
		return tagRepo.Create(ctx, &domain.Tag{
			ID:   uuid.New(),
			Name: "tx-test",
		})
	})
	if err != nil {
		t.Fatalf("WithTx commit: %v", err)
	}

	// Verify the tag persisted after commit
	tagRepo := sqlite.NewTagRepo(store.DB())
	tag, err := tagRepo.GetByName(ctx, "tx-test")
	if err != nil {
		t.Fatalf("tag not found after commit: %v", err)
	}
	if tag.Name != "tx-test" {
		t.Fatalf("unexpected tag name: %s", tag.Name)
	}
}

func TestWithTxRollback(t *testing.T) {
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Return an error inside the transaction to trigger rollback
	err = store.WithTx(ctx, func(tx *sqlite.Tx) error {
		tagRepo := tx.Tags()
		if err := tagRepo.Create(ctx, &domain.Tag{
			ID:   uuid.New(),
			Name: "rollback-test",
		}); err != nil {
			return err
		}
		return fmt.Errorf("intentional error")
	})
	if err == nil {
		t.Fatal("expected error from WithTx")
	}

	// Verify the tag was NOT persisted
	tagRepo := sqlite.NewTagRepo(store.DB())
	_, err = tagRepo.GetByName(ctx, "rollback-test")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after rollback, got: %v", err)
	}
}
```

You will need these imports in the test file:

```go
import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)
```

- [ ] **Step 2: Run the test to see it fail**

Run:
```bash
go test -v ./internal/sqlite -run TestWithTx
```
Expected: Compile error — `store.WithTx` and `sqlite.Tx` don't exist yet.

- [ ] **Step 3: Implement `Tx` type and `WithTx` on `Store`**

In `internal/sqlite/store.go`, add after the `Close()` method (after line 60):

```go
// Tx wraps an active database transaction and provides access to
// transactional repository instances. Repos created from a Tx share
// the same underlying *sql.Tx, so all their operations are atomic.
type Tx struct {
	tx *sql.Tx
}

// Tasks returns a TaskRepo operating within this transaction.
func (t *Tx) Tasks() *TaskRepo { return NewTaskRepo(t.tx) }

// Relations returns a RelationRepo operating within this transaction.
func (t *Tx) Relations() *RelationRepo { return NewRelationRepo(t.tx) }

// Annotations returns an AnnotationRepo operating within this transaction.
func (t *Tx) Annotations() *AnnotationRepo { return NewAnnotationRepo(t.tx) }

// Projects returns a ProjectRepo operating within this transaction.
func (t *Tx) Projects() *ProjectRepo { return NewProjectRepo(t.tx) }

// Tags returns a TagRepo operating within this transaction.
func (t *Tx) Tags() *TagRepo { return NewTagRepo(t.tx) }

// Workflows returns a WorkflowRepo operating within this transaction.
func (t *Tx) Workflows() *WorkflowRepo { return NewWorkflowRepo(t.tx) }

// WithTx executes fn within a database transaction. If fn returns nil,
// the transaction is committed. If fn returns an error (or panics),
// the transaction is rolled back and the error is returned.
func (s *Store) WithTx(ctx context.Context, fn func(tx *Tx) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer sqlTx.Rollback()

	if err := fn(&Tx{tx: sqlTx}); err != nil {
		return err
	}
	return sqlTx.Commit()
}
```

You will also need to add `"context"` to the imports in `store.go` if not already present (it should already be there from the `DBTX` interface added in Task 1).

- [ ] **Step 4: Run the test to see it pass**

Run:
```bash
go test -v ./internal/sqlite -run TestWithTx
```
Expected: Both `TestWithTxCommit` and `TestWithTxRollback` pass.

- [ ] **Step 5: Run all tests to confirm no regressions**

Run:
```bash
make test
```
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/sqlite/store.go internal/sqlite/tx_test.go
git commit -m "feat(sqlite): add Tx type and WithTx for transactional repo access

Tx wraps a *sql.Tx and provides scoped access to all repos within
a single transaction. WithTx handles begin/commit/rollback lifecycle."
```

---

### Task 3: Add `WithRelationTx` Method

**Files:**
- Modify: `internal/sqlite/store.go` (add `WithRelationTx` after `WithTx`)

**Context:** The service layer cannot import the `sqlite` package (dependency rule: dependencies flow downward only). So the service will define a `RelationTxProvider` interface. The `Store` must implement it. The interface will be defined in Phase 2, but we implement the `Store` side here.

The method signature must match what the service layer expects:

```go
func (s *Store) WithRelationTx(ctx context.Context, fn func(relationRepo repository.RelationRepository) error) error
```

This gives the service a `RelationRepository` that runs inside a transaction, without the service knowing anything about SQLite or `*sql.Tx`.

- [ ] **Step 1: Write a test for `WithRelationTx`**

Add to `internal/sqlite/tx_test.go`:

```go
func TestWithRelationTxCommit(t *testing.T) {
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create two tasks first (relations need valid task FKs)
	taskRepo := sqlite.NewTaskRepo(store.DB())
	task1 := newTestTask(t, "Task 1")
	task2 := newTestTask(t, "Task 2")
	mustCreateTask(t, taskRepo, task1)
	mustCreateTask(t, taskRepo, task2)

	// Create a relation inside WithRelationTx
	err = store.WithRelationTx(ctx, func(rr repository.RelationRepository) error {
		return rr.Create(ctx, &domain.Relation{
			ID:           uuid.New(),
			SourceID:     task1.ID,
			TargetID:     task2.ID,
			RelationType: "blocks",
			CreatedAt:    time.Now().UTC(),
		})
	})
	if err != nil {
		t.Fatalf("WithRelationTx: %v", err)
	}

	// Verify the relation persisted
	relationRepo := sqlite.NewRelationRepo(store.DB())
	rels, err := relationRepo.GetByTask(ctx, task1.ID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if len(rels) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(rels))
	}
}
```

You'll need to add these imports to the test file:

```go
"time"

"github.com/germanamz/tusk/internal/repository"
```

Note: `newTestTask` and `mustCreateTask` are helpers defined in `internal/sqlite/task_test.go`. Since this test is in `package sqlite_test`, you can use them if they are exported, or you may need to create local helpers. Check: if `task_test.go` uses `package sqlite` (internal tests), you cannot reuse them from `package sqlite_test`. In that case, create a local helper in `tx_test.go`:

```go
func newTestTask(t *testing.T, title string) *domain.Task {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	hex := strings.ReplaceAll(id.String(), "-", "")
	return &domain.Task{
		ID:         id,
		ShortID:    hex[:8],
		Title:      title,
		Status:     "pending",
		Version:    1,
		UDA:        map[string]any{},
		CreatedAt:  now,
		ModifiedAt: now,
	}
}

func mustCreateTask(t *testing.T, repo *sqlite.TaskRepo, task *domain.Task) {
	t.Helper()
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("creating task: %v", err)
	}
}
```

Check the package declaration at the top of the existing test files in `internal/sqlite/` (e.g., `relation_test.go`). If they use `package sqlite` (not `package sqlite_test`), change your `tx_test.go` to also use `package sqlite` and reuse the existing helpers directly without the local definitions above.

- [ ] **Step 2: Run the test to see it fail**

Run:
```bash
go test -v ./internal/sqlite -run TestWithRelationTx
```
Expected: Compile error — `store.WithRelationTx` doesn't exist yet.

- [ ] **Step 3: Implement `WithRelationTx`**

In `internal/sqlite/store.go`, add after the `WithTx` method:

```go
// WithRelationTx executes fn with a RelationRepository backed by a transaction.
// This is the concrete implementation of service.RelationTxProvider.
func (s *Store) WithRelationTx(ctx context.Context, fn func(rr repository.RelationRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Relations())
	})
}
```

Add `"github.com/germanamz/tusk/internal/repository"` to the imports in `store.go`.

- [ ] **Step 4: Run the test to see it pass**

Run:
```bash
go test -v ./internal/sqlite -run TestWithRelationTx
```
Expected: PASS.

- [ ] **Step 5: Run the full test suite**

Run:
```bash
make test
```
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/sqlite/store.go internal/sqlite/tx_test.go
git commit -m "feat(sqlite): add WithRelationTx for atomic relation operations

Implements the method that service.RelationTxProvider will require.
Delegates to WithTx, providing a transactional RelationRepository
to the callback."
```
