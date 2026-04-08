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

func TestAnnotationRepo_CountByTasks(t *testing.T) {
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db := store.DB()

	taskRepo := sqlite.NewTaskRepo(db)
	annRepo := sqlite.NewAnnotationRepo(db)
	ctx := context.Background()

	// Create two tasks
	t1 := &domain.Task{
		ID: uuid.New(), ShortID: "aaaaaaaa", ProjectID: "default",
		Title: "Task 1", Status: "pending", Version: 1,
		UDA:       map[string]any{},
		CreatedAt: time.Now().UTC(), ModifiedAt: time.Now().UTC(),
	}
	t2 := &domain.Task{
		ID: uuid.New(), ShortID: "bbbbbbbb", ProjectID: "default",
		Title: "Task 2", Status: "pending", Version: 1,
		UDA:       map[string]any{},
		CreatedAt: time.Now().UTC(), ModifiedAt: time.Now().UTC(),
	}
	if err := taskRepo.Create(ctx, t1); err != nil {
		t.Fatal(err)
	}
	if err := taskRepo.Create(ctx, t2); err != nil {
		t.Fatal(err)
	}

	// Add 2 annotations to t1, 1 to t2
	for _, ann := range []*domain.Annotation{
		{ID: uuid.New(), TaskID: t1.ID, Body: "note 1", CreatedAt: time.Now().UTC()},
		{ID: uuid.New(), TaskID: t1.ID, Body: "note 2", CreatedAt: time.Now().UTC()},
		{ID: uuid.New(), TaskID: t2.ID, Body: "note 3", CreatedAt: time.Now().UTC()},
	} {
		if err := annRepo.Create(ctx, ann); err != nil {
			t.Fatal(err)
		}
	}

	counts, err := annRepo.CountByTasks(ctx, []uuid.UUID{t1.ID, t2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if counts[t1.ID] != 2 {
		t.Fatalf("expected 2 annotations for t1, got %d", counts[t1.ID])
	}
	if counts[t2.ID] != 1 {
		t.Fatalf("expected 1 annotation for t2, got %d", counts[t2.ID])
	}

	// Empty input returns empty map
	empty, err := annRepo.CountByTasks(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty map, got %v", empty)
	}
}
