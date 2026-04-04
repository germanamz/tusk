package sqlite

import (
	"context"
	"errors"
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

func TestTagGetByID(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()

	color := "#00ff00"
	tag := &domain.Tag{ID: uuid.New(), Name: "getbyid", Color: &color}
	if err := repo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByID(ctx, tag.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "getbyid" {
		t.Fatalf("expected 'getbyid', got %q", got.Name)
	}
	if got.Color == nil || *got.Color != "#00ff00" {
		t.Fatalf("expected color '#00ff00', got %v", got.Color)
	}
}

func TestTagGetByIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())

	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTagUpdate(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()

	tag := &domain.Tag{ID: uuid.New(), Name: "old-name"}
	if err := repo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}

	newColor := "#abcdef"
	tag.Name = "new-name"
	tag.Color = &newColor
	if err := repo.Update(ctx, tag); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.GetByID(ctx, tag.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "new-name" {
		t.Fatalf("expected 'new-name', got %q", got.Name)
	}
	if got.Color == nil || *got.Color != "#abcdef" {
		t.Fatalf("expected color '#abcdef', got %v", got.Color)
	}
}

func TestTagUpdateNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())

	tag := &domain.Tag{ID: uuid.New(), Name: "ghost"}
	err := repo.Update(context.Background(), tag)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTagDelete(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()

	tag := &domain.Tag{ID: uuid.New(), Name: "deleteme"}
	if err := repo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(ctx, tag.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := repo.GetByID(ctx, tag.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestTagDeleteNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())

	err := repo.Delete(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTagCountTasksByTagID(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()

	tag := &domain.Tag{ID: uuid.New(), Name: "counted"}
	if err := tagRepo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}

	// No assignments yet
	count, err := tagRepo.CountTasksByTagID(ctx, tag.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}

	// Assign to two tasks
	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)
	if err := tagRepo.AssignToTask(ctx, t1.ID, tag.ID); err != nil {
		t.Fatal(err)
	}
	if err := tagRepo.AssignToTask(ctx, t2.ID, tag.ID); err != nil {
		t.Fatal(err)
	}

	count, err = tagRepo.CountTasksByTagID(ctx, tag.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2, got %d", count)
	}
}

func TestTagListWithUsage(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()

	color := "#ff0000"
	tag1 := &domain.Tag{ID: uuid.New(), Name: "used", Color: &color}
	tag2 := &domain.Tag{ID: uuid.New(), Name: "unused"}
	if err := tagRepo.Create(ctx, tag1); err != nil {
		t.Fatal(err)
	}
	if err := tagRepo.Create(ctx, tag2); err != nil {
		t.Fatal(err)
	}

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)
	if err := tagRepo.AssignToTask(ctx, task.ID, tag1.ID); err != nil {
		t.Fatal(err)
	}

	results, err := tagRepo.ListWithUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(results))
	}

	byName := map[string]domain.TagWithUsage{}
	for _, tw := range results {
		byName[tw.Tag.Name] = tw
	}

	usedTW := byName["used"]
	if usedTW.TaskCount != 1 {
		t.Fatalf("expected 'used' task count 1, got %d", usedTW.TaskCount)
	}
	if usedTW.Tag.Color == nil || *usedTW.Tag.Color != "#ff0000" {
		t.Fatalf("expected color '#ff0000', got %v", usedTW.Tag.Color)
	}

	unusedTW := byName["unused"]
	if unusedTW.TaskCount != 0 {
		t.Fatalf("expected 'unused' task count 0, got %d", unusedTW.TaskCount)
	}
}
