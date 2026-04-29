package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

// TestWithTx_AtomicTaskAndEvent demonstrates that the Phase 2 WriteTx adapter
// exposes Tasks, Relations, and Events through a single transaction: a task
// and an event created inside one WithTx call both land after commit.
func TestWithTx_AtomicTaskAndEvent(test *testing.T) {
	store, _, _ := sqlitetest.NewStore(test)
	provider := &testWriteTxProvider{store: store, maxEvents: 10000, pruneSlack: 1000}

	task := newSmokeTask("tx_smoke")
	event := domain.NewTaskCreatedEvent(task, nil)

	ctx := context.Background()
	if err := provider.WithTx(ctx, func(tx WriteTx) error {
		if err := tx.Tasks().Create(ctx, task); err != nil {
			return err
		}
		return tx.Events().Record(ctx, event)
	}); err != nil {
		test.Fatalf("WithTx: %v", err)
	}

	taskRepo := sqlite.NewTaskRepo(store.DB())
	if _, err := taskRepo.GetByID(ctx, task.ID); err != nil {
		test.Fatalf("task missing after commit: %v", err)
	}

	eventRepo := sqlite.NewEventRepo(store.DB(), 10000, 1000)
	count, err := eventRepo.Count(ctx)

	if err != nil {
		test.Fatalf("Count: %v", err)
	}

	if count != 1 {
		test.Fatalf("expected 1 event after commit, got %d", count)
	}
}

// TestWithTx_RollbackOnError confirms the adapter respects the error path:
// returning a non-nil error must roll back both inserts.
func TestWithTx_RollbackOnError(test *testing.T) {
	store, _, _ := sqlitetest.NewStore(test)
	provider := &testWriteTxProvider{store: store, maxEvents: 10000, pruneSlack: 1000}

	task := newSmokeTask("tx_rollback")
	event := domain.NewTaskCreatedEvent(task, nil)

	sentinel := errors.New("rollback")
	txErr := provider.WithTx(context.Background(), func(tx WriteTx) error {
		if err := tx.Tasks().Create(context.Background(), task); err != nil {
			return err
		}
		if err := tx.Events().Record(context.Background(), event); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(txErr, sentinel) {
		test.Fatalf("expected sentinel, got %v", txErr)
	}

	taskRepo := sqlite.NewTaskRepo(store.DB())
	if _, err := taskRepo.GetByID(context.Background(), task.ID); err == nil {
		test.Fatalf("expected task lookup to fail after rollback")
	}
	eventRepo := sqlite.NewEventRepo(store.DB(), 10000, 1000)
	count, countErr := eventRepo.Count(context.Background())

	if countErr != nil {
		test.Fatalf("Count: %v", countErr)
	}

	if count != 0 {
		test.Fatalf("expected 0 events after rollback, got %d", count)
	}
}

func newSmokeTask(shortID string) *domain.Task {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return &domain.Task{
		ID:         uuid.New(),
		ShortID:    shortID,
		ProjectID:  domain.DefaultProjectUUID,
		Title:      shortID,
		Status:     "pending",
		Priority:   3,
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
	}
}
