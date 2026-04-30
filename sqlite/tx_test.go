package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

func TestWithTxCommit(test *testing.T) {
	store := testStore(test)
	ctx := context.Background()

	// Insert a tag inside a transaction
	txErr := store.WithTx(ctx, func(tx *Tx) error {
		tagRepo := tx.Tags()
		return tagRepo.Create(ctx, &domain.Tag{
			ID:   uuid.New(),
			Name: "tx-test",
		})
	})

	if txErr != nil {
		test.Fatalf("WithTx commit: %v", txErr)
	}

	// Verify the tag persisted after commit
	tagRepo := NewTagRepo(store.DB())
	tag, getErr := tagRepo.GetByName(ctx, "tx-test")

	if getErr != nil {
		test.Fatalf("tag not found after commit: %v", getErr)
	}

	if tag.Name != "tx-test" {
		test.Fatalf("unexpected tag name: %s", tag.Name)
	}
}

func TestWithTxRollback(test *testing.T) {
	store := testStore(test)
	ctx := context.Background()

	// Return an error inside the transaction to trigger rollback
	txErr := store.WithTx(ctx, func(tx *Tx) error {
		tagRepo := tx.Tags()
		if createErr := tagRepo.Create(ctx, &domain.Tag{
			ID:   uuid.New(),
			Name: "rollback-test",
		}); createErr != nil {
			return createErr
		}
		return fmt.Errorf("intentional error")
	})

	if txErr == nil {
		test.Fatal("expected error from WithTx")
	}

	// Verify the tag was NOT persisted
	tagRepo := NewTagRepo(store.DB())
	_, getErr := tagRepo.GetByName(ctx, "rollback-test")

	if !errors.Is(getErr, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound after rollback, got: %v", getErr)
	}
}

func TestWithRelationTxCommit(test *testing.T) {
	store := testStore(test)
	ctx := context.Background()

	// Create two tasks first (relations need valid task FKs)
	taskRepo := NewTaskRepo(store.DB())
	task1 := newTestTask()
	task2 := newTestTask()
	mustCreateTask(test, taskRepo, task1)
	mustCreateTask(test, taskRepo, task2)

	// Create a relation inside WithRelationTx
	txErr := store.WithRelationTx(ctx, func(rr repository.RelationRepository) error {
		return rr.Create(ctx, &domain.Relation{
			ID:           uuid.New(),
			SourceID:     task1.ID,
			TargetID:     task2.ID,
			RelationType: "blocks",
			CreatedAt:    time.Now().UTC(),
		})
	})

	if txErr != nil {
		test.Fatalf("WithRelationTx: %v", txErr)
	}

	// Verify the relation persisted
	relationRepo := NewRelationRepo(store.DB())
	rels, getErr := relationRepo.GetByTask(ctx, task1.ID)

	if getErr != nil {
		test.Fatalf("GetByTask: %v", getErr)
	}

	if len(rels) != 1 {
		test.Fatalf("expected 1 relation, got %d", len(rels))
	}
}
