package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// Compile-time check: *AnnotationRepo must implement repository.AnnotationRepository.
// If AnnotationRepo is missing any method, this line produces a compile error.
// The nil pointer is never dereferenced — it costs nothing at runtime.
var _ repository.AnnotationRepository = (*AnnotationRepo)(nil)

// TestAnnotationCreate verifies that we can insert a new annotation and read it back
// via GetByTask. It exercises Create and GetByTask together because you need
// GetByTask to verify that Create actually persisted the data.
//
// Note: we must create a task first because annotations have a foreign key
// (task_id) pointing to the tasks table. Without a valid task, the INSERT would
// fail with a foreign key constraint error.
func TestAnnotationCreate(t *testing.T) {
	// testStore creates an in-memory SQLite database with all migrations applied.
	s := testStore(t)

	// We need a TaskRepo to create the parent task.
	taskRepo := NewTaskRepo(s.DB())

	// This is the repo we are actually testing.
	repo := NewAnnotationRepo(s.DB())

	ctx := context.Background()

	// Create a parent task. newTestTask() returns a *domain.Task with all fields
	// populated. mustCreateTask inserts it and calls t.Fatal if anything goes wrong.
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	// Build an annotation attached to the task we just created.
	// time.Now().UTC().Truncate(time.Millisecond) matches SQLite's millisecond
	// precision — without Truncate, the round-trip would lose sub-millisecond
	// data and comparisons would fail.
	ann := &domain.Annotation{
		ID:        uuid.New(),
		TaskID:    task.ID,
		Body:      "Blocked by upstream API changes",
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	// Create should succeed with no error.
	if err := repo.Create(ctx, ann); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read back all annotations for this task and verify our annotation is there.
	anns, err := repo.GetByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(anns) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(anns))
	}
	if anns[0].Body != "Blocked by upstream API changes" {
		t.Fatalf("wrong body: %q", anns[0].Body)
	}
}

// TestAnnotationGetByTaskEmpty verifies that GetByTask returns an empty slice
// (not an error) when a task has no annotations. This is important: "no results"
// is not an error condition for a list query.
func TestAnnotationGetByTaskEmpty(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewAnnotationRepo(s.DB())
	ctx := context.Background()

	// Create a task but do NOT create any annotations for it.
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	anns, err := repo.GetByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Should be 0 annotations, not an error.
	if len(anns) != 0 {
		t.Fatalf("expected 0 annotations, got %d", len(anns))
	}
}

// TestAnnotationGetByTaskMultiple verifies that GetByTask returns all annotations
// for a given task, not just the first one.
func TestAnnotationGetByTaskMultiple(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewAnnotationRepo(s.DB())
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	// Create 3 annotations on the same task.
	for _, body := range []string{"First", "Second", "Third"} {
		ann := &domain.Annotation{
			ID:        uuid.New(),
			TaskID:    task.ID,
			Body:      body,
			CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
		}
		if err := repo.Create(ctx, ann); err != nil {
			t.Fatal(err)
		}
	}

	anns, err := repo.GetByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(anns) != 3 {
		t.Fatalf("expected 3 annotations, got %d", len(anns))
	}
}

// TestAnnotationDelete verifies that Delete removes an annotation and that
// GetByTask no longer returns it.
func TestAnnotationDelete(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewAnnotationRepo(s.DB())
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	ann := &domain.Annotation{
		ID:        uuid.New(),
		TaskID:    task.ID,
		Body:      "To be deleted",
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, ann); err != nil {
		t.Fatal(err)
	}

	// Delete the annotation we just created.
	if err := repo.Delete(ctx, ann.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify it is gone.
	anns, err := repo.GetByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(anns) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(anns))
	}
}

// TestAnnotationDeleteNotFound verifies that deleting a non-existent annotation
// returns domain.ErrNotFound. This uses the RowsAffected pattern: the DELETE
// SQL succeeds but affects 0 rows, which we translate to ErrNotFound.
func TestAnnotationDeleteNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewAnnotationRepo(s.DB())

	// uuid.New() generates a random UUID that does not exist in the DB.
	err := repo.Delete(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestAnnotationRepo_GetByTasks_Batch exercises the batch read used by the
// markdown renderer. It covers three cases: a happy path with two tasks each
// owning two annotations, an empty input slice, and a non-existent task ID.
func TestAnnotationRepo_GetByTasks_Batch(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewAnnotationRepo(s.DB())
	ctx := context.Background()

	task1 := newTestTask()
	mustCreateTask(t, taskRepo, task1)
	task2 := newTestTask()
	mustCreateTask(t, taskRepo, task2)

	// Seed two annotations per task with distinct timestamps so we can assert
	// the per-task result is sorted ascending by created_at.
	base := time.Now().UTC().Truncate(time.Millisecond)
	mkAnn := func(taskID uuid.UUID, body string, offset time.Duration) *domain.Annotation {
		return &domain.Annotation{
			ID:        uuid.New(),
			TaskID:    taskID,
			Body:      body,
			CreatedAt: base.Add(offset),
		}
	}
	for _, ann := range []*domain.Annotation{
		mkAnn(task1.ID, "t1-a", 0),
		mkAnn(task1.ID, "t1-b", time.Millisecond),
		mkAnn(task2.ID, "t2-a", 2*time.Millisecond),
		mkAnn(task2.ID, "t2-b", 3*time.Millisecond),
	} {
		if err := repo.Create(ctx, ann); err != nil {
			t.Fatalf("Create %q: %v", ann.Body, err)
		}
	}

	got, err := repo.GetByTasks(ctx, []uuid.UUID{task1.ID, task2.ID})
	if err != nil {
		t.Fatalf("GetByTasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(got))
	}
	if len(got[task1.ID]) != 2 {
		t.Fatalf("task1: expected 2 annotations, got %d", len(got[task1.ID]))
	}
	if len(got[task2.ID]) != 2 {
		t.Fatalf("task2: expected 2 annotations, got %d", len(got[task2.ID]))
	}
	if got[task1.ID][0].Body != "t1-a" || got[task1.ID][1].Body != "t1-b" {
		t.Fatalf("task1: expected ascending order [t1-a, t1-b], got [%q, %q]",
			got[task1.ID][0].Body, got[task1.ID][1].Body)
	}
	if got[task2.ID][0].Body != "t2-a" || got[task2.ID][1].Body != "t2-b" {
		t.Fatalf("task2: expected ascending order [t2-a, t2-b], got [%q, %q]",
			got[task2.ID][0].Body, got[task2.ID][1].Body)
	}

	empty, err := repo.GetByTasks(ctx, []uuid.UUID{})
	if err != nil {
		t.Fatalf("GetByTasks(empty): %v", err)
	}
	if empty == nil {
		t.Fatal("GetByTasks(empty) returned nil map; expected non-nil")
	}
	if len(empty) != 0 {
		t.Fatalf("GetByTasks(empty) expected 0 entries, got %d", len(empty))
	}

	missing, err := repo.GetByTasks(ctx, []uuid.UUID{uuid.New()})
	if err != nil {
		t.Fatalf("GetByTasks(missing): %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("GetByTasks(missing) expected 0 entries, got %d", len(missing))
	}
}
