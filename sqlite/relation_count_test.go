package sqlite_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

func TestRelationRepo_CountBlockedByIncompleteTasks(t *testing.T) {
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db := store.DB()

	taskRepo := sqlite.NewTaskRepo(db)
	relRepo := sqlite.NewRelationRepo(db)
	ctx := context.Background()

	// Helper to create a task with a given status.
	makeTask := func(name, status string) *domain.Task {
		task := &domain.Task{
			ID: uuid.New(), ShortID: uuid.New().String()[:8], ProjectID: "default",
			Title: name, Status: status, Version: 1,
			UDA:       map[string]any{},
			CreatedAt: time.Now().UTC(), ModifiedAt: time.Now().UTC(),
		}
		if err := taskRepo.Create(ctx, task); err != nil {
			t.Fatal(err)
		}
		return task
	}

	// Helper to create a "blocks" relation: source blocks target.
	block := func(source, target *domain.Task) {
		if err := relRepo.Create(ctx, &domain.Relation{
			ID: uuid.New(), SourceID: source.ID, TargetID: target.ID,
			RelationType: "blocks", CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	taskA := makeTask("A", "pending")
	taskB := makeTask("B", "pending")
	taskC := makeTask("C", "active")
	taskD := makeTask("D", "completed")
	taskE := makeTask("E", "deleted")

	// A (pending) blocks B → incomplete blocker
	block(taskA, taskB)
	// D (completed) blocks B → NOT incomplete
	block(taskD, taskB)
	// E (deleted) blocks B → NOT incomplete
	block(taskE, taskB)
	// C (active) blocks B → incomplete blocker
	block(taskC, taskB)

	t.Run("pending blocker counts as incomplete", func(t *testing.T) {
		counts, err := relRepo.CountBlockedByIncompleteTasks(ctx, []uuid.UUID{taskB.ID})
		if err != nil {
			t.Fatal(err)
		}
		if counts[taskB.ID] != 2 {
			t.Fatalf("expected B to have 2 incomplete blockers (A+C), got %d", counts[taskB.ID])
		}
	})

	t.Run("completed blocker excluded", func(t *testing.T) {
		// D blocks B but D is completed — only A and C count
		counts, err := relRepo.CountBlockedByIncompleteTasks(ctx, []uuid.UUID{taskB.ID})
		if err != nil {
			t.Fatal(err)
		}
		if counts[taskB.ID] != 2 {
			t.Fatalf("expected 2 (completed excluded), got %d", counts[taskB.ID])
		}
	})

	t.Run("task with no incomplete blockers absent from map", func(t *testing.T) {
		// taskA has no blockers at all
		counts, err := relRepo.CountBlockedByIncompleteTasks(ctx, []uuid.UUID{taskA.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := counts[taskA.ID]; ok {
			t.Fatalf("expected A absent from map, got count %d", counts[taskA.ID])
		}
	})

	t.Run("mixed: only incomplete blockers counted", func(t *testing.T) {
		// Query both A (0 incomplete blockers) and B (2 incomplete blockers)
		counts, err := relRepo.CountBlockedByIncompleteTasks(ctx, []uuid.UUID{taskA.ID, taskB.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := counts[taskA.ID]; ok {
			t.Fatalf("expected A absent, got %d", counts[taskA.ID])
		}
		if counts[taskB.ID] != 2 {
			t.Fatalf("expected B=2, got %d", counts[taskB.ID])
		}
	})

	t.Run("empty input returns empty map", func(t *testing.T) {
		counts, err := relRepo.CountBlockedByIncompleteTasks(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(counts) != 0 {
			t.Fatalf("expected empty map, got %v", counts)
		}
	})
}

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
