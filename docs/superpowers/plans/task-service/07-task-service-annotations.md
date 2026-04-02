# Phase 7: TaskService Annotation Methods

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement `Annotate`, `GetAnnotations`, and `DeleteAnnotation` methods on TaskService.

**Prereqs:** Phase 6 must be complete (convenience transitions exist).

**Files:**
- Modify: `internal/service/task.go` (append methods)
- Modify: `internal/service/task_test.go` (append tests)

---

## Background

Annotations are timestamped notes attached to a task. They are immutable after creation (no update method). The `AnnotationRepository` interface (defined in `internal/repository/annotation.go`) provides:

```go
Create(ctx context.Context, ann *domain.Annotation) error
GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Annotation, error)
Delete(ctx context.Context, id uuid.UUID) error
```

The `Annotation` domain type (defined in `internal/domain/annotation.go`):

```go
type Annotation struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	Body      string
	CreatedAt time.Time
}
```

All annotation methods live on `TaskService` (not a separate service) because annotations have no independent business logic.

### Method behaviors

- **`Annotate`** — validates body is non-empty, looks up task by short ID (to confirm existence and get the full UUID), generates annotation ID and timestamp, persists, returns the annotation
- **`GetAnnotations`** — looks up task by short ID, delegates to `annotationRepo.GetByTask`
- **`DeleteAnnotation`** — direct pass-through to `annotationRepo.Delete` (no task lookup needed — the annotation ID is sufficient)

---

## Task 1: Write failing tests for annotations

**Files:**
- Modify: `internal/service/task_test.go` (append)

- [ ] **Step 1: Add annotation tests**

Append to `internal/service/task_test.go`:

```go
func TestAnnotate_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Annotate me")
	mustCreateTask(t, env.taskSvc, task)

	ann, err := env.taskSvc.Annotate(ctx, task.ShortID, "This is a note")
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if ann.ID == uuid.Nil {
		t.Fatal("expected non-nil annotation ID")
	}
	if ann.TaskID != task.ID {
		t.Fatalf("expected TaskID %s, got %s", task.ID, ann.TaskID)
	}
	if ann.Body != "This is a note" {
		t.Fatalf("expected body 'This is a note', got %q", ann.Body)
	}
	if ann.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
}

func TestAnnotate_EmptyBody(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Annotate me")
	mustCreateTask(t, env.taskSvc, task)

	_, err := env.taskSvc.Annotate(ctx, task.ShortID, "")
	if err == nil {
		t.Fatal("expected error for empty annotation body")
	}
}

func TestAnnotate_TaskNotFound(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	_, err := env.taskSvc.Annotate(ctx, "nonexist", "Some note")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetAnnotations_WithResults(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Has annotations")
	mustCreateTask(t, env.taskSvc, task)

	_, err := env.taskSvc.Annotate(ctx, task.ShortID, "Note 1")
	if err != nil {
		t.Fatalf("Annotate 1: %v", err)
	}
	_, err = env.taskSvc.Annotate(ctx, task.ShortID, "Note 2")
	if err != nil {
		t.Fatalf("Annotate 2: %v", err)
	}

	annotations, err := env.taskSvc.GetAnnotations(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetAnnotations: %v", err)
	}
	if len(annotations) != 2 {
		t.Fatalf("expected 2 annotations, got %d", len(annotations))
	}
}

func TestGetAnnotations_Empty(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("No annotations")
	mustCreateTask(t, env.taskSvc, task)

	annotations, err := env.taskSvc.GetAnnotations(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetAnnotations: %v", err)
	}
	if len(annotations) != 0 {
		t.Fatalf("expected 0 annotations, got %d", len(annotations))
	}
}

func TestGetAnnotations_TaskNotFound(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	_, err := env.taskSvc.GetAnnotations(ctx, "nonexist")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteAnnotation_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Delete annotation")
	mustCreateTask(t, env.taskSvc, task)

	ann, err := env.taskSvc.Annotate(ctx, task.ShortID, "To be deleted")
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}

	if err := env.taskSvc.DeleteAnnotation(ctx, ann.ID); err != nil {
		t.Fatalf("DeleteAnnotation: %v", err)
	}

	// Verify it's gone
	annotations, err := env.taskSvc.GetAnnotations(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetAnnotations: %v", err)
	}
	if len(annotations) != 0 {
		t.Fatalf("expected 0 annotations after delete, got %d", len(annotations))
	}
}

func TestDeleteAnnotation_NotFound(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	err := env.taskSvc.DeleteAnnotation(ctx, uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestAnnotate|TestGetAnnotations|TestDeleteAnnotation" -v`

Expected: **compilation error** — `Annotate`, `GetAnnotations`, `DeleteAnnotation` methods are not defined on `TaskService`.

- [ ] **Step 3: Commit**

```bash
git add internal/service/task_test.go
git commit -m "test(service): add failing annotation tests"
```

---

## Task 2: Implement annotation methods

**Files:**
- Modify: `internal/service/task.go`

- [ ] **Step 1: Add annotation methods**

Open `internal/service/task.go`. Find the `Delete` method (the last convenience transition). Add the following methods directly after it:

```go
// Annotate adds a timestamped note to a task.
func (s *TaskService) Annotate(ctx context.Context, taskShortID string, body string) (*domain.Annotation, error) {
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("annotation body must not be empty")
	}

	task, err := s.taskRepo.GetByShortID(ctx, taskShortID)
	if err != nil {
		return nil, err
	}

	ann := &domain.Annotation{
		ID:        uuid.New(),
		TaskID:    task.ID,
		Body:      body,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := s.annotationRepo.Create(ctx, ann); err != nil {
		return nil, err
	}
	return ann, nil
}

// GetAnnotations returns all annotations for a task, identified by short ID.
func (s *TaskService) GetAnnotations(ctx context.Context, taskShortID string) ([]*domain.Annotation, error) {
	task, err := s.taskRepo.GetByShortID(ctx, taskShortID)
	if err != nil {
		return nil, err
	}
	return s.annotationRepo.GetByTask(ctx, task.ID)
}

// DeleteAnnotation removes an annotation by its ID.
func (s *TaskService) DeleteAnnotation(ctx context.Context, annotationID uuid.UUID) error {
	return s.annotationRepo.Delete(ctx, annotationID)
}
```

- [ ] **Step 2: Run the annotation tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestAnnotate|TestGetAnnotations|TestDeleteAnnotation" -v`

Expected output — all 8 tests PASS:

```
=== RUN   TestAnnotate_HappyPath
--- PASS: TestAnnotate_HappyPath
=== RUN   TestAnnotate_EmptyBody
--- PASS: TestAnnotate_EmptyBody
=== RUN   TestAnnotate_TaskNotFound
--- PASS: TestAnnotate_TaskNotFound
=== RUN   TestGetAnnotations_WithResults
--- PASS: TestGetAnnotations_WithResults
=== RUN   TestGetAnnotations_Empty
--- PASS: TestGetAnnotations_Empty
=== RUN   TestGetAnnotations_TaskNotFound
--- PASS: TestGetAnnotations_TaskNotFound
=== RUN   TestDeleteAnnotation_HappyPath
--- PASS: TestDeleteAnnotation_HappyPath
=== RUN   TestDeleteAnnotation_NotFound
--- PASS: TestDeleteAnnotation_NotFound
PASS
```

- [ ] **Step 3: Run the full service test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -v`

Expected: all tests pass (5 workflow + 10 create + 7 read + 9 update + 6 transitions + 8 annotations = 45 tests).

- [ ] **Step 4: Commit**

```bash
git add internal/service/task.go
git commit -m "feat(service): implement Annotate, GetAnnotations, DeleteAnnotation"
```
