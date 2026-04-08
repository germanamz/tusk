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

func TestWithTxCommit(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Insert a tag inside a transaction
	err := store.WithTx(ctx, func(tx *Tx) error {
		tagRepo := tx.Tags()
		return tagRepo.Create(ctx, &domain.Tag{
			ID:   uuid.New(),
			Name: "tx-test",
		})
	})
	if err != nil {
		t.Fatalf("WithTx commit: %v", err)
	}

	// Verify the tag persisted after commit
	tagRepo := NewTagRepo(store.DB())
	tag, err := tagRepo.GetByName(ctx, "tx-test")
	if err != nil {
		t.Fatalf("tag not found after commit: %v", err)
	}
	if tag.Name != "tx-test" {
		t.Fatalf("unexpected tag name: %s", tag.Name)
	}
}

func TestWithTxRollback(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Return an error inside the transaction to trigger rollback
	err := store.WithTx(ctx, func(tx *Tx) error {
		tagRepo := tx.Tags()
		if err := tagRepo.Create(ctx, &domain.Tag{
			ID:   uuid.New(),
			Name: "rollback-test",
		}); err != nil {
			return err
		}
		return fmt.Errorf("intentional error")
	})
	if err == nil {
		t.Fatal("expected error from WithTx")
	}

	// Verify the tag was NOT persisted
	tagRepo := NewTagRepo(store.DB())
	_, err = tagRepo.GetByName(ctx, "rollback-test")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after rollback, got: %v", err)
	}
}

func TestWithRelationTxCommit(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Create two tasks first (relations need valid task FKs)
	taskRepo := NewTaskRepo(store.DB())
	task1 := newTestTask()
	task2 := newTestTask()
	mustCreateTask(t, taskRepo, task1)
	mustCreateTask(t, taskRepo, task2)

	// Create a relation inside WithRelationTx
	err := store.WithRelationTx(ctx, func(rr repository.RelationRepository) error {
		return rr.Create(ctx, &domain.Relation{
			ID:           uuid.New(),
			SourceID:     task1.ID,
			TargetID:     task2.ID,
			RelationType: "blocks",
			CreatedAt:    time.Now().UTC(),
		})
	})
	if err != nil {
		t.Fatalf("WithRelationTx: %v", err)
	}

	// Verify the relation persisted
	relationRepo := NewRelationRepo(store.DB())
	rels, err := relationRepo.GetByTask(ctx, task1.ID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if len(rels) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(rels))
	}
}
