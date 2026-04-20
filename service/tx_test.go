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
func TestWithTx_AtomicTaskAndEvent(t *testing.T) {
	store, _, _ := sqlitetest.NewStore(t)
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
		t.Fatalf("WithTx: %v", err)
	}

	taskRepo := sqlite.NewTaskRepo(store.DB())
	if _, err := taskRepo.GetByID(ctx, task.ID); err != nil {
		t.Fatalf("task missing after commit: %v", err)
	}

	eventRepo := sqlite.NewEventRepo(store.DB(), 10000, 1000)
	count, err := eventRepo.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 event after commit, got %d", count)
	}
}

// TestWithTx_RollbackOnError confirms the adapter respects the error path:
// returning a non-nil error must roll back both inserts.
func TestWithTx_RollbackOnError(t *testing.T) {
	store, _, _ := sqlitetest.NewStore(t)
	provider := &testWriteTxProvider{store: store, maxEvents: 10000, pruneSlack: 1000}

	task := newSmokeTask("tx_rollback")
	event := domain.NewTaskCreatedEvent(task, nil)

	sentinel := errors.New("rollback")
	err := provider.WithTx(context.Background(), func(tx WriteTx) error {
		if err := tx.Tasks().Create(context.Background(), task); err != nil {
			return err
		}
		if err := tx.Events().Record(context.Background(), event); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}

	taskRepo := sqlite.NewTaskRepo(store.DB())
	if _, err := taskRepo.GetByID(context.Background(), task.ID); err == nil {
		t.Fatalf("expected task lookup to fail after rollback")
	}
	eventRepo := sqlite.NewEventRepo(store.DB(), 10000, 1000)
	count, err := eventRepo.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 events after rollback, got %d", count)
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
