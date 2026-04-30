package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

var _ repository.TagRepository = (*TagRepo)(nil)

func TestTagCreate(test *testing.T) {
	store := testStore(test)
	repo := NewTagRepo(store.DB())
	ctx := context.Background()
	color := "#ff0000"
	tag := &domain.Tag{ID: uuid.New(), Name: "bug", Color: &color}

	if err := repo.Create(ctx, tag); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByName(ctx, "bug")

	if err != nil {
		test.Fatal(err)
	}

	if got.Name != "bug" {
		test.Fatalf("expected bug, got %s", got.Name)
	}
	if got.Color == nil || *got.Color != "#ff0000" {
		test.Fatalf("expected #ff0000, got %v", got.Color)
	}
}

func TestTagCreateNullColor(test *testing.T) {
	store := testStore(test)
	repo := NewTagRepo(store.DB())
	tag := &domain.Tag{ID: uuid.New(), Name: "frontend"}

	if err := repo.Create(context.Background(), tag); err != nil {
		test.Fatal(err)
	}

	got, err := repo.GetByName(context.Background(), "frontend")

	if err != nil {
		test.Fatal(err)
	}

	if got.Color != nil {
		test.Fatalf("expected nil color, got %v", got.Color)
	}
}

func TestTagGetByNameNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewTagRepo(store.DB())
	_, err := repo.GetByName(context.Background(), "nonexistent")
	if err != domain.ErrNotFound {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTagList(test *testing.T) {
	store := testStore(test)
	repo := NewTagRepo(store.DB())
	ctx := context.Background()
	for _, name := range []string{"bug", "feature", "docs"} {
		if err := repo.Create(ctx, &domain.Tag{ID: uuid.New(), Name: name}); err != nil {
			test.Fatal(err)
		}
	}
	tags, err := repo.List(ctx)

	if err != nil {
		test.Fatal(err)
	}

	if len(tags) != 3 {
		test.Fatalf("expected 3, got %d", len(tags))
	}
}

func TestTagAssignToTask(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	tagRepo := NewTagRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(test, taskRepo, task)
	tag := &domain.Tag{ID: uuid.New(), Name: "urgent"}

	if err := tagRepo.Create(ctx, tag); err != nil {
		test.Fatal(err)
	}

	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil {
		test.Fatalf("AssignToTask: %v", err)
	}

	tags, err := tagRepo.GetTaskTags(ctx, task.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(tags) != 1 {
		test.Fatalf("expected 1, got %d", len(tags))
	}
	if tags[0].Name != "urgent" {
		test.Fatalf("expected urgent, got %s", tags[0].Name)
	}
}

func TestTagAssignToTaskDuplicate(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	tagRepo := NewTagRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(test, taskRepo, task)
	tag := &domain.Tag{ID: uuid.New(), Name: "dup-assign"}

	if err := tagRepo.Create(ctx, tag); err != nil {
		test.Fatal(err)
	}

	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil {
		test.Fatal(err)
	}

	// Second assign should be idempotent (INSERT OR IGNORE).
	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil {
		test.Fatalf("expected idempotent assign, got %v", err)
	}

	tags, err := tagRepo.GetTaskTags(ctx, task.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(tags) != 1 {
		test.Fatalf("expected 1 tag after duplicate assign, got %d", len(tags))
	}
}

func TestTagRemoveFromTaskNotFound(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	tagRepo := NewTagRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(test, taskRepo, task)
	tag := &domain.Tag{ID: uuid.New(), Name: "never-assigned"}

	if err := tagRepo.Create(ctx, tag); err != nil {
		test.Fatal(err)
	}

	err := tagRepo.RemoveFromTask(ctx, task.ID, tag.ID)
	if err != domain.ErrNotFound {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTagRemoveFromTask(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	tagRepo := NewTagRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(test, taskRepo, task)
	tag := &domain.Tag{ID: uuid.New(), Name: "temp"}

	if err := tagRepo.Create(ctx, tag); err != nil {
		test.Fatal(err)
	}

	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil {
		test.Fatal(err)
	}

	if err := tagRepo.RemoveFromTask(ctx, task.ID, tag.ID); err != nil {
		test.Fatalf("RemoveFromTask: %v", err)
	}

	tags, err := tagRepo.GetTaskTags(ctx, task.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(tags) != 0 {
		test.Fatalf("expected 0 after remove, got %d", len(tags))
	}
}

func TestTagGetTaskTagsEmpty(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	tagRepo := NewTagRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(test, taskRepo, task)
	tags, err := tagRepo.GetTaskTags(ctx, task.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(tags) != 0 {
		test.Fatalf("expected 0, got %d", len(tags))
	}
}

func TestTagFilterIntegration(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	tagRepo := NewTagRepo(store.DB())
	ctx := context.Background()
	bugTag := &domain.Tag{ID: uuid.New(), Name: "bug"}
	apiTag := &domain.Tag{ID: uuid.New(), Name: "api"}
	docsTag := &domain.Tag{ID: uuid.New(), Name: "docs"}
	for _, tag := range []*domain.Tag{bugTag, apiTag, docsTag} {
		if err := tagRepo.Create(ctx, tag); err != nil {
			test.Fatal(err)
		}
	}
	taskOne := newTestTask()
	taskTwo := newTestTask()
	taskThree := newTestTask()
	for _, task := range []*domain.Task{taskOne, taskTwo, taskThree} {
		mustCreateTask(test, taskRepo, task)
	}
	for _, pair := range [][2]uuid.UUID{
		{taskOne.ID, bugTag.ID}, {taskOne.ID, apiTag.ID},
		{taskTwo.ID, bugTag.ID}, {taskTwo.ID, docsTag.ID},
		{taskThree.ID, apiTag.ID},
	} {
		if err := tagRepo.AssignToTask(ctx, pair[0], pair[1]); err != nil {
			test.Fatal(err)
		}
	}
	tasks, err := taskRepo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{Tags: []string{"bug", "api"}}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 1 || tasks[0].ID != taskOne.ID {
		test.Fatalf("expected only taskOne, got %d tasks", len(tasks))
	}

	tasks, err = taskRepo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{ExcludeTags: []string{"docs"}}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 2 {
		test.Fatalf("expected 2 excluding docs, got %d", len(tasks))
	}
}

func TestTagGetByID(test *testing.T) {
	store := testStore(test)
	repo := NewTagRepo(store.DB())
	ctx := context.Background()

	color := "#00ff00"
	tag := &domain.Tag{ID: uuid.New(), Name: "getbyid", Color: &color}

	if err := repo.Create(ctx, tag); err != nil {
		test.Fatal(err)
	}

	got, err := repo.GetByID(ctx, tag.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Name != "getbyid" {
		test.Fatalf("expected 'getbyid', got %q", got.Name)
	}
	if got.Color == nil || *got.Color != "#00ff00" {
		test.Fatalf("expected color '#00ff00', got %v", got.Color)
	}
}

func TestTagGetByIDNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewTagRepo(store.DB())

	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTagUpdate(test *testing.T) {
	store := testStore(test)
	repo := NewTagRepo(store.DB())
	ctx := context.Background()

	tag := &domain.Tag{ID: uuid.New(), Name: "old-name"}

	if err := repo.Create(ctx, tag); err != nil {
		test.Fatal(err)
	}

	newColor := "#abcdef"
	tag.Name = "new-name"
	tag.Color = &newColor

	if err := repo.Update(ctx, tag); err != nil {
		test.Fatalf("Update: %v", err)
	}

	got, err := repo.GetByID(ctx, tag.ID)

	if err != nil {
		test.Fatal(err)
	}

	if got.Name != "new-name" {
		test.Fatalf("expected 'new-name', got %q", got.Name)
	}
	if got.Color == nil || *got.Color != "#abcdef" {
		test.Fatalf("expected color '#abcdef', got %v", got.Color)
	}
}

func TestTagUpdateNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewTagRepo(store.DB())

	tag := &domain.Tag{ID: uuid.New(), Name: "ghost"}
	err := repo.Update(context.Background(), tag)
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTagDelete(test *testing.T) {
	store := testStore(test)
	repo := NewTagRepo(store.DB())
	ctx := context.Background()

	tag := &domain.Tag{ID: uuid.New(), Name: "deleteme"}

	if err := repo.Create(ctx, tag); err != nil {
		test.Fatal(err)
	}

	if err := repo.Delete(ctx, tag.ID); err != nil {
		test.Fatalf("Delete: %v", err)
	}

	_, err := repo.GetByID(ctx, tag.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestTagDeleteNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewTagRepo(store.DB())

	err := repo.Delete(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTagCountTasksByTagID(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	tagRepo := NewTagRepo(store.DB())
	ctx := context.Background()

	tag := &domain.Tag{ID: uuid.New(), Name: "counted"}

	if err := tagRepo.Create(ctx, tag); err != nil {
		test.Fatal(err)
	}

	// No assignments yet
	count, err := tagRepo.CountTasksByTagID(ctx, tag.ID)

	if err != nil {
		test.Fatal(err)
	}

	if count != 0 {
		test.Fatalf("expected 0, got %d", count)
	}

	// Assign to two tasks
	taskOne := newTestTask()
	taskTwo := newTestTask()
	mustCreateTask(test, taskRepo, taskOne)
	mustCreateTask(test, taskRepo, taskTwo)

	if err := tagRepo.AssignToTask(ctx, taskOne.ID, tag.ID); err != nil {
		test.Fatal(err)
	}

	if err := tagRepo.AssignToTask(ctx, taskTwo.ID, tag.ID); err != nil {
		test.Fatal(err)
	}

	count, err = tagRepo.CountTasksByTagID(ctx, tag.ID)

	if err != nil {
		test.Fatal(err)
	}

	if count != 2 {
		test.Fatalf("expected 2, got %d", count)
	}
}

func TestTagListWithUsage(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	tagRepo := NewTagRepo(store.DB())
	ctx := context.Background()

	color := "#ff0000"
	tag1 := &domain.Tag{ID: uuid.New(), Name: "used", Color: &color}
	tag2 := &domain.Tag{ID: uuid.New(), Name: "unused"}

	if err := tagRepo.Create(ctx, tag1); err != nil {
		test.Fatal(err)
	}

	if err := tagRepo.Create(ctx, tag2); err != nil {
		test.Fatal(err)
	}

	task := newTestTask()
	mustCreateTask(test, taskRepo, task)

	if err := tagRepo.AssignToTask(ctx, task.ID, tag1.ID); err != nil {
		test.Fatal(err)
	}

	results, err := tagRepo.ListWithUsage(ctx)

	if err != nil {
		test.Fatal(err)
	}

	if len(results) != 2 {
		test.Fatalf("expected 2 tags, got %d", len(results))
	}

	byName := map[string]domain.TagWithUsage{}
	for _, tw := range results {
		byName[tw.Tag.Name] = tw
	}

	usedTW := byName["used"]
	if usedTW.TaskCount != 1 {
		test.Fatalf("expected 'used' task count 1, got %d", usedTW.TaskCount)
	}
	if usedTW.Tag.Color == nil || *usedTW.Tag.Color != "#ff0000" {
		test.Fatalf("expected color '#ff0000', got %v", usedTW.Tag.Color)
	}

	unusedTW := byName["unused"]
	if unusedTW.TaskCount != 0 {
		test.Fatalf("expected 'unused' task count 0, got %d", unusedTW.TaskCount)
	}
}
