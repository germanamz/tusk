package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

func TestAnnotationRepo_CountByTasks(test *testing.T) {
	store, storeErr := sqlite.New(":memory:", migrations.FS)

	if storeErr != nil {
		test.Fatal(storeErr)
	}

	defer store.Close()

	db := store.DB()

	taskRepo := sqlite.NewTaskRepo(db)
	annRepo := sqlite.NewAnnotationRepo(db)
	ctx := context.Background()

	// Create two tasks
	task1 := &domain.Task{
		ID: uuid.New(), ShortID: "aaaaaaaa", ProjectID: domain.DefaultProjectUUID,
		Title: "Task 1", Status: "pending", Version: 1,
		UDA:       map[string]any{},
		CreatedAt: time.Now().UTC(), ModifiedAt: time.Now().UTC(),
	}
	task2 := &domain.Task{
		ID: uuid.New(), ShortID: "bbbbbbbb", ProjectID: domain.DefaultProjectUUID,
		Title: "Task 2", Status: "pending", Version: 1,
		UDA:       map[string]any{},
		CreatedAt: time.Now().UTC(), ModifiedAt: time.Now().UTC(),
	}

	if err := taskRepo.Create(ctx, task1); err != nil {
		test.Fatal(err)
	}

	if err := taskRepo.Create(ctx, task2); err != nil {
		test.Fatal(err)
	}

	// Add 2 annotations to task1, 1 to task2
	for _, annotation := range []*domain.Annotation{
		{ID: uuid.New(), TaskID: task1.ID, Body: "note 1", CreatedAt: time.Now().UTC()},
		{ID: uuid.New(), TaskID: task1.ID, Body: "note 2", CreatedAt: time.Now().UTC()},
		{ID: uuid.New(), TaskID: task2.ID, Body: "note 3", CreatedAt: time.Now().UTC()},
	} {
		if err := annRepo.Create(ctx, annotation); err != nil {
			test.Fatal(err)
		}
	}

	counts, countErr := annRepo.CountByTasks(ctx, []uuid.UUID{task1.ID, task2.ID})

	if countErr != nil {
		test.Fatal(countErr)
	}

	if counts[task1.ID] != 2 {
		test.Fatalf("expected 2 annotations for task1, got %d", counts[task1.ID])
	}

	if counts[task2.ID] != 1 {
		test.Fatalf("expected 1 annotation for task2, got %d", counts[task2.ID])
	}

	// Empty input returns empty map
	empty, emptyErr := annRepo.CountByTasks(ctx, nil)

	if emptyErr != nil {
		test.Fatal(emptyErr)
	}

	if len(empty) != 0 {
		test.Fatalf("expected empty map, got %v", empty)
	}
}
