package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
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
