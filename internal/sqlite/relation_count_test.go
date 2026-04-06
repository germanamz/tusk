package sqlite_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

func TestRelationRepo_CountBlockingByTasks(t *testing.T) {
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db := store.DB()

	taskRepo := sqlite.NewTaskRepo(db)
	relRepo := sqlite.NewRelationRepo(db)
	ctx := context.Background()

	// Create 3 tasks: A blocks B and C
	tasks := make([]*domain.Task, 3)
	for i := range tasks {
		tasks[i] = &domain.Task{
			ID: uuid.New(), ShortID: fmt.Sprintf("%08d", i), ProjectID: "default",
			Title: fmt.Sprintf("Task %d", i), Status: "pending", Version: 1,
			UDA:       map[string]any{},
			CreatedAt: time.Now().UTC(), ModifiedAt: time.Now().UTC(),
		}
		if err := taskRepo.Create(ctx, tasks[i]); err != nil {
			t.Fatal(err)
		}
	}

	// A blocks B
	if err := relRepo.Create(ctx, &domain.Relation{
		ID: uuid.New(), SourceID: tasks[0].ID, TargetID: tasks[1].ID,
		RelationType: "blocks", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	// A blocks C
	if err := relRepo.Create(ctx, &domain.Relation{
		ID: uuid.New(), SourceID: tasks[0].ID, TargetID: tasks[2].ID,
		RelationType: "blocks", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	ids := []uuid.UUID{tasks[0].ID, tasks[1].ID, tasks[2].ID}

	// CountBlockingByTasks: A blocks 2, B blocks 0, C blocks 0
	blocking, err := relRepo.CountBlockingByTasks(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if blocking[tasks[0].ID] != 2 {
		t.Fatalf("expected A blocking 2, got %d", blocking[tasks[0].ID])
	}
	if blocking[tasks[1].ID] != 0 {
		t.Fatalf("expected B blocking 0, got %d", blocking[tasks[1].ID])
	}

	// CountBlockedByTasks: A blocked by 0, B blocked by 1, C blocked by 1
	blockedBy, err := relRepo.CountBlockedByTasks(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if blockedBy[tasks[1].ID] != 1 {
		t.Fatalf("expected B blocked_by 1, got %d", blockedBy[tasks[1].ID])
	}
	if blockedBy[tasks[2].ID] != 1 {
		t.Fatalf("expected C blocked_by 1, got %d", blockedBy[tasks[2].ID])
	}

	// Empty input returns empty map
	empty, err := relRepo.CountBlockingByTasks(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty map, got %v", empty)
	}
}
