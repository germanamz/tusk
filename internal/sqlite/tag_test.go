package sqlite

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

var _ repository.TagRepository = (*TagRepo)(nil)

func TestTagCreate(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()
	color := "#ff0000"
	tag := &domain.Tag{ID: uuid.New(), Name: "bug", Color: &color}
	if err := repo.Create(ctx, tag); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByName(ctx, "bug")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "bug" {
		t.Fatalf("expected bug, got %s", got.Name)
	}
	if got.Color == nil || *got.Color != "#ff0000" {
		t.Fatalf("expected #ff0000, got %v", got.Color)
	}
}

func TestTagCreateNullColor(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	tag := &domain.Tag{ID: uuid.New(), Name: "frontend"}
	if err := repo.Create(context.Background(), tag); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByName(context.Background(), "frontend")
	if err != nil {
		t.Fatal(err)
	}
	if got.Color != nil {
		t.Fatalf("expected nil color, got %v", got.Color)
	}
}

func TestTagGetByNameNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	_, err := repo.GetByName(context.Background(), "nonexistent")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTagList(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()
	for _, name := range []string{"bug", "feature", "docs"} {
		if err := repo.Create(ctx, &domain.Tag{ID: uuid.New(), Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	tags, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 3 {
		t.Fatalf("expected 3, got %d", len(tags))
	}
}

func TestTagAssignToTask(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)
	tag := &domain.Tag{ID: uuid.New(), Name: "urgent"}
	if err := tagRepo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}
	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil {
		t.Fatalf("AssignToTask: %v", err)
	}
	tags, err := tagRepo.GetTaskTags(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1, got %d", len(tags))
	}
	if tags[0].Name != "urgent" {
		t.Fatalf("expected urgent, got %s", tags[0].Name)
	}
}

func TestTagAssignToTaskDuplicate(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)
	tag := &domain.Tag{ID: uuid.New(), Name: "dup-assign"}
	if err := tagRepo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}
	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil {
		t.Fatal(err)
	}
	// Second assign should be idempotent (INSERT OR IGNORE).
	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil {
		t.Fatalf("expected idempotent assign, got %v", err)
	}
	tags, err := tagRepo.GetTaskTags(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag after duplicate assign, got %d", len(tags))
	}
}

func TestTagRemoveFromTaskNotFound(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)
	tag := &domain.Tag{ID: uuid.New(), Name: "never-assigned"}
	if err := tagRepo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}
	err := tagRepo.RemoveFromTask(ctx, task.ID, tag.ID)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTagRemoveFromTask(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)
	tag := &domain.Tag{ID: uuid.New(), Name: "temp"}
	if err := tagRepo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}
	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil {
		t.Fatal(err)
	}
	if err := tagRepo.RemoveFromTask(ctx, task.ID, tag.ID); err != nil {
		t.Fatalf("RemoveFromTask: %v", err)
	}
	tags, err := tagRepo.GetTaskTags(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected 0 after remove, got %d", len(tags))
	}
}

func TestTagGetTaskTagsEmpty(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)
	tags, err := tagRepo.GetTaskTags(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected 0, got %d", len(tags))
	}
}

func TestTagFilterIntegration(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()
	bugTag := &domain.Tag{ID: uuid.New(), Name: "bug"}
	apiTag := &domain.Tag{ID: uuid.New(), Name: "api"}
	docsTag := &domain.Tag{ID: uuid.New(), Name: "docs"}
	for _, tag := range []*domain.Tag{bugTag, apiTag, docsTag} {
		if err := tagRepo.Create(ctx, tag); err != nil {
			t.Fatal(err)
		}
	}
	t1 := newTestTask()
	t2 := newTestTask()
	t3 := newTestTask()
	for _, task := range []*domain.Task{t1, t2, t3} {
		mustCreateTask(t, taskRepo, task)
	}
	for _, pair := range [][2]uuid.UUID{
		{t1.ID, bugTag.ID}, {t1.ID, apiTag.ID},
		{t2.ID, bugTag.ID}, {t2.ID, docsTag.ID},
		{t3.ID, apiTag.ID},
	} {
		if err := tagRepo.AssignToTask(ctx, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := taskRepo.List(ctx, domain.TaskFilter{Tags: []string{"bug", "api"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != t1.ID {
		t.Fatalf("expected only t1, got %d tasks", len(tasks))
	}
	tasks, err = taskRepo.List(ctx, domain.TaskFilter{ExcludeTags: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 excluding docs, got %d", len(tasks))
	}
}
