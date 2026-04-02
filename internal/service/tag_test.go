package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

// testTagEnv creates a fully wired test environment for TagService tests.
func testTagEnv(t *testing.T) (*TagService, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db := store.DB()
	tagRepo := sqlite.NewTagRepo(db)
	tagSvc := NewTagService(tagRepo)
	return tagSvc, store
}

func TestFindOrCreate_NewTag(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	tag, err := tagSvc.FindOrCreate(ctx, "backend")
	if err != nil {
		t.Fatalf("FindOrCreate: %v", err)
	}
	if tag.ID == uuid.Nil {
		t.Fatal("expected non-nil ID")
	}
	if tag.Name != "backend" {
		t.Fatalf("expected name 'backend', got %q", tag.Name)
	}
}

func TestFindOrCreate_ExistingTag(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	first, err := tagSvc.FindOrCreate(ctx, "api")
	if err != nil {
		t.Fatalf("first FindOrCreate: %v", err)
	}

	second, err := tagSvc.FindOrCreate(ctx, "api")
	if err != nil {
		t.Fatalf("second FindOrCreate: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected same ID, got %s and %s", first.ID, second.ID)
	}
}

func TestFindOrCreate_EmptyName(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	_, err := tagSvc.FindOrCreate(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestFindOrCreate_WhitespaceName(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	_, err := tagSvc.FindOrCreate(ctx, "   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only name")
	}
}
