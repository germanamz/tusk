# Tag Support Phase 1: TagService Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the `TagService` that encapsulates tag business logic (find-or-create, assign/remove from tasks, query).

**Architecture:** A new `TagService` in `internal/service/tag.go` wraps the existing `repository.TagRepository` interface. It adds find-or-create semantics (auto-create tags on first use) and bulk assign/remove operations. Tests use in-memory SQLite like existing service tests.

**Tech Stack:** Go, SQLite (in-memory for tests), `github.com/google/uuid`

**Spec:** `docs/superpowers/specs/2026-04-02-tag-support-design.md`

---

### Task 1: TagService constructor and FindOrCreate

**Files:**
- Create: `internal/service/tag.go`
- Create: `internal/service/tag_test.go`

- [ ] **Step 1: Write the test file with FindOrCreate tests**

Create `internal/service/tag_test.go`:

```go
package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

// testTagEnv creates a fully wired test environment for TagService tests.
func testTagEnv(t *testing.T) (*TagService, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db := store.DB()
	tagRepo := sqlite.NewTagRepo(db)
	tagSvc := NewTagService(tagRepo)
	return tagSvc, store
}

func TestFindOrCreate_NewTag(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	tag, err := tagSvc.FindOrCreate(ctx, "backend")
	if err != nil {
		t.Fatalf("FindOrCreate: %v", err)
	}
	if tag.ID == uuid.Nil {
		t.Fatal("expected non-nil ID")
	}
	if tag.Name != "backend" {
		t.Fatalf("expected name 'backend', got %q", tag.Name)
	}
}

func TestFindOrCreate_ExistingTag(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	first, err := tagSvc.FindOrCreate(ctx, "api")
	if err != nil {
		t.Fatalf("first FindOrCreate: %v", err)
	}

	second, err := tagSvc.FindOrCreate(ctx, "api")
	if err != nil {
		t.Fatalf("second FindOrCreate: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected same ID, got %s and %s", first.ID, second.ID)
	}
}

func TestFindOrCreate_EmptyName(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	_, err := tagSvc.FindOrCreate(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestFindOrCreate_WhitespaceName(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	_, err := tagSvc.FindOrCreate(ctx, "   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only name")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run TestFindOrCreate -v`

Expected: Compilation error — `NewTagService` and `FindOrCreate` not defined.

- [ ] **Step 3: Write the TagService with constructor and FindOrCreate**

Create `internal/service/tag.go`:

```go
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// TagService encapsulates tag business logic including find-or-create
// semantics and bulk assign/remove operations.
type TagService struct {
	tagRepo repository.TagRepository
}

// NewTagService creates a new TagService with the given repository.
func NewTagService(tagRepo repository.TagRepository) *TagService {
	return &TagService{tagRepo: tagRepo}
}

// FindOrCreate returns the existing tag with the given name, or creates
// a new one if it doesn't exist. Empty or whitespace-only names are rejected.
func (s *TagService) FindOrCreate(ctx context.Context, name string) (*domain.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name must not be empty")
	}

	tag, err := s.tagRepo.GetByName(ctx, name)
	if err == nil {
		return tag, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("looking up tag %q: %w", name, err)
	}

	tag = &domain.Tag{
		ID:   uuid.New(),
		Name: name,
	}
	if err := s.tagRepo.Create(ctx, tag); err != nil {
		return nil, fmt.Errorf("creating tag %q: %w", name, err)
	}
	return tag, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run TestFindOrCreate -v`

Expected: All 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/tag.go internal/service/tag_test.go
git commit -m "feat(service): add TagService with FindOrCreate"
```

---

### Task 2: AssignToTask (bulk find-or-create + assign)

**Files:**
- Modify: `internal/service/tag_test.go`
- Modify: `internal/service/tag.go`

This task also requires a task to exist in the database so tags can be assigned to it. The test helper creates a task via `TaskService`.

- [ ] **Step 1: Write tests for AssignToTask**

Add to `internal/service/tag_test.go` — a helper to create a task and tests for AssignToTask:

```go
// mustCreateTaskForTags creates a task via TaskService for use in tag tests.
func mustCreateTaskForTags(t *testing.T, store *sqlite.Store) *domain.Task {
	t.Helper()
	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)
	workflowSvc := NewWorkflowService(workflowRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)

	task := &domain.Task{Title: "test task"}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("creating test task: %v", err)
	}
	return task
}

func TestAssignToTask_MultipleTags(t *testing.T) {
	tagSvc, store := testTagEnv(t)
	ctx := context.Background()
	task := mustCreateTaskForTags(t, store)

	err := tagSvc.AssignToTask(ctx, task.ID, []string{"bug", "urgent"})
	if err != nil {
		t.Fatalf("AssignToTask: %v", err)
	}

	tags, err := tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTaskTags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}

	names := map[string]bool{}
	for _, tg := range tags {
		names[tg.Name] = true
	}
	if !names["bug"] || !names["urgent"] {
		t.Fatalf("expected tags 'bug' and 'urgent', got %v", names)
	}
}

func TestAssignToTask_EmptySlice(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	err := tagSvc.AssignToTask(ctx, uuid.New(), []string{})
	if err != nil {
		t.Fatalf("AssignToTask with empty slice should be no-op, got: %v", err)
	}
}

func TestAssignToTask_Idempotent(t *testing.T) {
	tagSvc, store := testTagEnv(t)
	ctx := context.Background()
	task := mustCreateTaskForTags(t, store)

	if err := tagSvc.AssignToTask(ctx, task.ID, []string{"api"}); err != nil {
		t.Fatalf("first AssignToTask: %v", err)
	}
	if err := tagSvc.AssignToTask(ctx, task.ID, []string{"api"}); err != nil {
		t.Fatalf("second AssignToTask (should be idempotent): %v", err)
	}

	tags, err := tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTaskTags: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag after idempotent assign, got %d", len(tags))
	}
}
```

Also add the import for `"github.com/google/uuid"` if not already present (it was added in Task 1).

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestAssignToTask|TestGetTaskTags" -v`

Expected: Compilation error — `AssignToTask` and `GetTaskTags` not defined on `TagService`.

- [ ] **Step 3: Implement AssignToTask and GetTaskTags**

Add to `internal/service/tag.go`:

```go
// AssignToTask finds-or-creates each tag by name and assigns them to the task.
// An empty tagNames slice is a no-op.
func (s *TagService) AssignToTask(ctx context.Context, taskID uuid.UUID, tagNames []string) error {
	for _, name := range tagNames {
		tag, err := s.FindOrCreate(ctx, name)
		if err != nil {
			return err
		}
		if err := s.tagRepo.AssignToTask(ctx, taskID, tag.ID); err != nil {
			return fmt.Errorf("assigning tag %q to task: %w", name, err)
		}
	}
	return nil
}

// GetTaskTags returns all tags assigned to a task.
func (s *TagService) GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error) {
	return s.tagRepo.GetTaskTags(ctx, taskID)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestAssignToTask" -v`

Expected: All 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/tag.go internal/service/tag_test.go
git commit -m "feat(service): add TagService.AssignToTask and GetTaskTags"
```

---

### Task 3: RemoveFromTask and List

**Files:**
- Modify: `internal/service/tag_test.go`
- Modify: `internal/service/tag.go`

- [ ] **Step 1: Write tests for RemoveFromTask and List**

Add to `internal/service/tag_test.go`:

```go
func TestRemoveFromTask_ExistingTag(t *testing.T) {
	tagSvc, store := testTagEnv(t)
	ctx := context.Background()
	task := mustCreateTaskForTags(t, store)

	if err := tagSvc.AssignToTask(ctx, task.ID, []string{"bug", "urgent"}); err != nil {
		t.Fatalf("AssignToTask: %v", err)
	}

	if err := tagSvc.RemoveFromTask(ctx, task.ID, []string{"bug"}); err != nil {
		t.Fatalf("RemoveFromTask: %v", err)
	}

	tags, err := tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTaskTags: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].Name != "urgent" {
		t.Fatalf("expected remaining tag 'urgent', got %q", tags[0].Name)
	}
}

func TestRemoveFromTask_NonexistentTag(t *testing.T) {
	tagSvc, store := testTagEnv(t)
	ctx := context.Background()
	task := mustCreateTaskForTags(t, store)

	// Removing a tag that was never assigned — should be a silent no-op
	err := tagSvc.RemoveFromTask(ctx, task.ID, []string{"nonexistent"})
	if err != nil {
		t.Fatalf("RemoveFromTask for nonexistent tag should be no-op, got: %v", err)
	}
}

func TestRemoveFromTask_EmptySlice(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	err := tagSvc.RemoveFromTask(ctx, uuid.New(), []string{})
	if err != nil {
		t.Fatalf("RemoveFromTask with empty slice should be no-op, got: %v", err)
	}
}

func TestList(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	// Create some tags
	if _, err := tagSvc.FindOrCreate(ctx, "alpha"); err != nil {
		t.Fatalf("FindOrCreate: %v", err)
	}
	if _, err := tagSvc.FindOrCreate(ctx, "beta"); err != nil {
		t.Fatalf("FindOrCreate: %v", err)
	}

	tags, err := tagSvc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestRemoveFromTask|TestList" -v`

Expected: Compilation error — `RemoveFromTask` and `List` not defined on `TagService`.

- [ ] **Step 3: Implement RemoveFromTask and List**

Add to `internal/service/tag.go`:

```go
// RemoveFromTask removes the named tags from the task.
// If a tag name doesn't exist or isn't assigned, it's silently skipped.
// An empty tagNames slice is a no-op.
func (s *TagService) RemoveFromTask(ctx context.Context, taskID uuid.UUID, tagNames []string) error {
	for _, name := range tagNames {
		tag, err := s.tagRepo.GetByName(ctx, name)
		if errors.Is(err, domain.ErrNotFound) {
			continue // tag doesn't exist — nothing to remove
		}
		if err != nil {
			return fmt.Errorf("looking up tag %q: %w", name, err)
		}
		err = s.tagRepo.RemoveFromTask(ctx, taskID, tag.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("removing tag %q from task: %w", name, err)
		}
	}
	return nil
}

// List returns all tags in the system.
func (s *TagService) List(ctx context.Context) ([]*domain.Tag, error) {
	return s.tagRepo.List(ctx)
}
```

- [ ] **Step 4: Run all TagService tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestFindOrCreate|TestAssignToTask|TestRemoveFromTask|TestList" -v`

Expected: All 10 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/tag.go internal/service/tag_test.go
git commit -m "feat(service): add TagService.RemoveFromTask and List"
```

---

### Task 4: Run full test suite

**Files:** None (verification only)

- [ ] **Step 1: Run all project tests to ensure nothing is broken**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -v`

Expected: All tests PASS, including existing TaskService, SQLite, and all new TagService tests.

- [ ] **Step 2: Verify no lint or vet issues**

Run: `cd /Users/germanamz/projects/tusk && go vet ./...`

Expected: No issues.
