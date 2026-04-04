package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
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

// mustCreateTaskForTags creates a task via TaskService for use in tag tests.
func mustCreateTaskForTags(t *testing.T, store *sqlite.Store) *domain.Task {
	t.Helper()
	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)
	workflowSvc := NewWorkflowService(workflowRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)

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

func TestCreate_NewTag(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	tag, err := tagSvc.Create(ctx, "feature", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tag.Name != "feature" {
		t.Fatalf("expected name 'feature', got %q", tag.Name)
	}
	if tag.Color != nil {
		t.Fatalf("expected nil color, got %v", tag.Color)
	}
}

func TestCreate_WithColor(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	color := "#ff0000"
	tag, err := tagSvc.Create(ctx, "urgent", &color)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tag.Color == nil || *tag.Color != "#ff0000" {
		t.Fatalf("expected color '#ff0000', got %v", tag.Color)
	}
}

func TestCreate_Duplicate(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "dup", nil); err != nil {
		t.Fatal(err)
	}

	_, err := tagSvc.Create(ctx, "dup", nil)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestCreate_EmptyName(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	_, err := tagSvc.Create(ctx, "", nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreate_WhitespaceName(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	_, err := tagSvc.Create(ctx, "   ", nil)
	if err == nil {
		t.Fatal("expected error for whitespace-only name")
	}
}

func TestDelete_UnusedTag(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "removable", nil); err != nil {
		t.Fatal(err)
	}

	if err := tagSvc.Delete(ctx, "removable"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify it's gone
	tags, err := tagSvc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected 0 tags after delete, got %d", len(tags))
	}
}

func TestDelete_TagInUse(t *testing.T) {
	tagSvc, store := testTagEnv(t)
	ctx := context.Background()

	task := mustCreateTaskForTags(t, store)
	if err := tagSvc.AssignToTask(ctx, task.ID, []string{"busy"}); err != nil {
		t.Fatal(err)
	}

	err := tagSvc.Delete(ctx, "busy")
	if !errors.Is(err, domain.ErrTagInUse) {
		t.Fatalf("expected ErrTagInUse, got %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	err := tagSvc.Delete(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestList(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

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
