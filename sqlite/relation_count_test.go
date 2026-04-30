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

func TestRelationRepo_CountBlockedByIncompleteTasks(test *testing.T) {
	store, storeErr := sqlite.New(":memory:", migrations.FS)

	if storeErr != nil {
		test.Fatal(storeErr)
	}

	defer store.Close()
	db := store.DB()

	taskRepo := sqlite.NewTaskRepo(db)
	relRepo := sqlite.NewRelationRepo(db)
	ctx := context.Background()

	// Helper to create a task with a given status.
	makeTask := func(name, status string) *domain.Task {
		task := &domain.Task{
			ID: uuid.New(), ShortID: uuid.New().String()[:8], ProjectID: domain.DefaultProjectUUID,
			Title: name, Status: status, Version: 1,
			UDA:       map[string]any{},
			CreatedAt: time.Now().UTC(), ModifiedAt: time.Now().UTC(),
		}
		if err := taskRepo.Create(ctx, task); err != nil {
			test.Fatal(err)
		}
		return task
	}

	// Helper to create a "blocks" relation: source blocks target.
	block := func(source, target *domain.Task) {
		if err := relRepo.Create(ctx, &domain.Relation{
			ID: uuid.New(), SourceID: source.ID, TargetID: target.ID,
			RelationType: "blocks", CreatedAt: time.Now().UTC(),
		}); err != nil {
			test.Fatal(err)
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

	test.Run("pending blocker counts as incomplete", func(test *testing.T) {
		counts, countErr := relRepo.CountBlockedByIncompleteTasks(ctx, []uuid.UUID{taskB.ID})

		if countErr != nil {
			test.Fatal(countErr)
		}

		if counts[taskB.ID] != 2 {
			test.Fatalf("expected B to have 2 incomplete blockers (A+C), got %d", counts[taskB.ID])
		}
	})

	test.Run("completed blocker excluded", func(test *testing.T) {
		// D blocks B but D is completed — only A and C count
		counts, countErr := relRepo.CountBlockedByIncompleteTasks(ctx, []uuid.UUID{taskB.ID})

		if countErr != nil {
			test.Fatal(countErr)
		}

		if counts[taskB.ID] != 2 {
			test.Fatalf("expected 2 (completed excluded), got %d", counts[taskB.ID])
		}
	})

	test.Run("task with no incomplete blockers absent from map", func(test *testing.T) {
		// taskA has no blockers at all
		counts, countErr := relRepo.CountBlockedByIncompleteTasks(ctx, []uuid.UUID{taskA.ID})

		if countErr != nil {
			test.Fatal(countErr)
		}

		if _, ok := counts[taskA.ID]; ok {
			test.Fatalf("expected A absent from map, got count %d", counts[taskA.ID])
		}
	})

	test.Run("mixed: only incomplete blockers counted", func(test *testing.T) {
		// Query both A (0 incomplete blockers) and B (2 incomplete blockers)
		counts, countErr := relRepo.CountBlockedByIncompleteTasks(ctx, []uuid.UUID{taskA.ID, taskB.ID})

		if countErr != nil {
			test.Fatal(countErr)
		}

		if _, ok := counts[taskA.ID]; ok {
			test.Fatalf("expected A absent, got %d", counts[taskA.ID])
		}
		if counts[taskB.ID] != 2 {
			test.Fatalf("expected B=2, got %d", counts[taskB.ID])
		}
	})

	test.Run("empty input returns empty map", func(test *testing.T) {
		counts, countErr := relRepo.CountBlockedByIncompleteTasks(ctx, nil)

		if countErr != nil {
			test.Fatal(countErr)
		}

		if len(counts) != 0 {
			test.Fatalf("expected empty map, got %v", counts)
		}
	})
}

func TestRelationRepo_CountBlockingByTasks(test *testing.T) {
	store, storeErr := sqlite.New(":memory:", migrations.FS)

	if storeErr != nil {
		test.Fatal(storeErr)
	}

	defer store.Close()
	db := store.DB()

	taskRepo := sqlite.NewTaskRepo(db)
	relRepo := sqlite.NewRelationRepo(db)
	ctx := context.Background()

	// Create 3 tasks: A blocks B and C
	tasks := make([]*domain.Task, 3)
	for index := range tasks {
		tasks[index] = &domain.Task{
			ID: uuid.New(), ShortID: fmt.Sprintf("%08d", index), ProjectID: domain.DefaultProjectUUID,
			Title: fmt.Sprintf("Task %d", index), Status: "pending", Version: 1,
			UDA:       map[string]any{},
			CreatedAt: time.Now().UTC(), ModifiedAt: time.Now().UTC(),
		}
		if err := taskRepo.Create(ctx, tasks[index]); err != nil {
			test.Fatal(err)
		}
	}

	// A blocks B
	if err := relRepo.Create(ctx, &domain.Relation{
		ID: uuid.New(), SourceID: tasks[0].ID, TargetID: tasks[1].ID,
		RelationType: "blocks", CreatedAt: time.Now().UTC(),
	}); err != nil {
		test.Fatal(err)
	}

	// A blocks C
	if err := relRepo.Create(ctx, &domain.Relation{
		ID: uuid.New(), SourceID: tasks[0].ID, TargetID: tasks[2].ID,
		RelationType: "blocks", CreatedAt: time.Now().UTC(),
	}); err != nil {
		test.Fatal(err)
	}

	ids := []uuid.UUID{tasks[0].ID, tasks[1].ID, tasks[2].ID}

	// CountBlockingByTasks: A blocks 2, B blocks 0, C blocks 0
	blocking, blockingErr := relRepo.CountBlockingByTasks(ctx, ids)

	if blockingErr != nil {
		test.Fatal(blockingErr)
	}

	if blocking[tasks[0].ID] != 2 {
		test.Fatalf("expected A blocking 2, got %d", blocking[tasks[0].ID])
	}
	if blocking[tasks[1].ID] != 0 {
		test.Fatalf("expected B blocking 0, got %d", blocking[tasks[1].ID])
	}

	// CountBlockedByTasks: A blocked by 0, B blocked by 1, C blocked by 1
	blockedBy, blockedByErr := relRepo.CountBlockedByTasks(ctx, ids)

	if blockedByErr != nil {
		test.Fatal(blockedByErr)
	}

	if blockedBy[tasks[1].ID] != 1 {
		test.Fatalf("expected B blocked_by 1, got %d", blockedBy[tasks[1].ID])
	}
	if blockedBy[tasks[2].ID] != 1 {
		test.Fatalf("expected C blocked_by 1, got %d", blockedBy[tasks[2].ID])
	}

	// Empty input returns empty map
	empty, emptyErr := relRepo.CountBlockingByTasks(ctx, nil)

	if emptyErr != nil {
		test.Fatal(emptyErr)
	}

	if len(empty) != 0 {
		test.Fatalf("expected empty map, got %v", empty)
	}
}
