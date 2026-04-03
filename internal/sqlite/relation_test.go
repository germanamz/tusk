package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// Compile-time check: *RelationRepo must implement repository.RelationRepository.
var _ repository.RelationRepository = (*RelationRepo)(nil)

// newTestRelation is a test helper that creates a *domain.Relation with all
// fields populated. It generates a fresh UUID for the ID and uses the current
// time (truncated to milliseconds) for CreatedAt.
//
// Parameters:
//   - sourceID: the UUID of the source task (the task "doing" the action)
//   - targetID: the UUID of the target task (the task "receiving" the action)
//   - relType: one of "blocks", "relates_to", "duplicates"
func newTestRelation(sourceID, targetID uuid.UUID, relType string) *domain.Relation {
	return &domain.Relation{
		ID:           uuid.New(),
		SourceID:     sourceID,
		TargetID:     targetID,
		RelationType: relType,
		CreatedAt:    time.Now().UTC().Truncate(time.Millisecond),
	}
}

// TestRelationCreate verifies that we can insert a new relation and read it back
// via GetByTask.
//
// Setup: create two tasks (source and target), then create a "blocks" relation
// from task1 to task2. Verify that GetByTask(task1.ID) returns the relation.
func TestRelationCreate(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	// Create two tasks. Relations connect two tasks, so we need both to exist.
	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	// Create a "blocks" relation: t1 blocks t2.
	rel := newTestRelation(t1.ID, t2.ID, "blocks")
	if err := repo.Create(ctx, rel); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify via GetByTask. Since t1 is the source, it should appear.
	rels, err := repo.GetByTask(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Fatalf("expected 1, got %d", len(rels))
	}
	if rels[0].RelationType != "blocks" {
		t.Fatalf("expected blocks, got %s", rels[0].RelationType)
	}
}

// TestRelationCreateDuplicate verifies that inserting the same (source, target, type)
// combination twice returns domain.ErrDuplicateRelation.
//
// This tests the UNIQUE(source_id, target_id, relation_type) constraint and our
// isUniqueViolation helper that translates the SQLite error into a domain error.
func TestRelationCreateDuplicate(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	// First insert: should succeed.
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}

	// Second insert with same source, target, and type: should fail.
	// Note that newTestRelation generates a NEW uuid.UUID for the ID field,
	// but the UNIQUE constraint is on (source_id, target_id, relation_type),
	// not on id. So even though the IDs differ, the constraint fires.
	err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks"))
	if err != domain.ErrDuplicateRelation {
		t.Fatalf("expected ErrDuplicateRelation, got %v", err)
	}
}

// TestRelationDelete verifies that Delete removes a relation and that
// GetByTask no longer returns it.
func TestRelationDelete(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	rel := newTestRelation(t1.ID, t2.ID, "relates_to")
	if err := repo.Create(ctx, rel); err != nil {
		t.Fatal(err)
	}

	// Delete the relation.
	if err := repo.Delete(ctx, rel.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify it is gone.
	rels, err := repo.GetByTask(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(rels))
	}
}

// TestRelationDeleteNotFound verifies that deleting a non-existent relation
// returns domain.ErrNotFound.
func TestRelationDeleteNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewRelationRepo(s.DB())
	err := repo.Delete(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestRelationDeleteByFields verifies that DeleteByFields removes a relation
// matching the exact (source, target, type) triple.
func TestRelationDeleteByFields(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	rel := newTestRelation(t1.ID, t2.ID, "blocks")
	if err := repo.Create(ctx, rel); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteByFields(ctx, t1.ID, t2.ID, "blocks"); err != nil {
		t.Fatalf("DeleteByFields: %v", err)
	}

	rels, err := repo.GetByTask(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(rels))
	}
}

// TestRelationDeleteByFieldsNotFound verifies that DeleteByFields returns
// domain.ErrNotFound when no matching relation exists.
func TestRelationDeleteByFieldsNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewRelationRepo(s.DB())
	err := repo.DeleteByFields(context.Background(), uuid.New(), uuid.New(), "blocks")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestRelationGetByTask verifies that GetByTask returns relations where the
// task is EITHER the source OR the target.
//
// Setup:
//   - t1 -> t2 (blocks) — t1 is source
//   - t3 -> t1 (relates_to) — t1 is target
//
// GetByTask(t1) should return BOTH relations (2 total) because t1 appears
// as source in one and target in the other.
func TestRelationGetByTask(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	t3 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)
	mustCreateTask(t, taskRepo, t3)

	// t1 is source in this relation.
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}
	// t1 is target in this relation.
	if err := repo.Create(ctx, newTestRelation(t3.ID, t1.ID, "relates_to")); err != nil {
		t.Fatal(err)
	}

	rels, err := repo.GetByTask(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Both relations involve t1, so both should be returned.
	if len(rels) != 2 {
		t.Fatalf("expected 2, got %d", len(rels))
	}
}

// TestRelationGetBlocking verifies that GetBlocking returns only relations
// where the given task is the SOURCE and the type is "blocks".
//
// Setup:
//   - t1 blocks t2 — should be returned (t1 is source, type is "blocks")
//   - t1 blocks t3 — should be returned (t1 is source, type is "blocks")
//   - t2 relates_to t3 — should NOT be returned (different type)
//
// GetBlocking(t1) should return 2 relations.
func TestRelationGetBlocking(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	t3 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)
	mustCreateTask(t, taskRepo, t3)

	// t1 is the blocker in both of these.
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestRelation(t1.ID, t3.ID, "blocks")); err != nil {
		t.Fatal(err)
	}
	// This is a different type — should not appear in GetBlocking results.
	if err := repo.Create(ctx, newTestRelation(t2.ID, t3.ID, "relates_to")); err != nil {
		t.Fatal(err)
	}

	blocking, err := repo.GetBlocking(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocking) != 2 {
		t.Fatalf("expected 2, got %d", len(blocking))
	}
}

// TestRelationGetBlockedBy verifies that GetBlockedBy returns only relations
// where the given task is the TARGET and the type is "blocks".
//
// Setup:
//   - t1 blocks t2 — GetBlockedBy(t2) should return this (t2 is the target)
//
// GetBlockedBy(t2) should return 1 relation, and its SourceID should be t1.
func TestRelationGetBlockedBy(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	// t1 blocks t2: t1 is the source (blocker), t2 is the target (blocked).
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}

	// Ask "what is blocking t2?" — should return the relation with t1 as source.
	blockedBy, err := repo.GetBlockedBy(ctx, t2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blockedBy) != 1 {
		t.Fatalf("expected 1, got %d", len(blockedBy))
	}
	// Verify that the source of the blocking relation is t1.
	if blockedBy[0].SourceID != t1.ID {
		t.Fatal("expected source to be t1")
	}
}

// TestRelationExists verifies the Exists method, which checks whether a
// specific (source, target, type) combination exists in the database.
//
// This test checks three scenarios:
//  1. The exact combination exists — should return true.
//  2. The reverse direction (swap source and target) — should return false
//     because relations are directional.
//  3. Same source and target but different type — should return false.
func TestRelationExists(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	// Create: t1 blocks t2.
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}

	// Scenario 1: exact match — should be true.
	exists, err := repo.Exists(ctx, t1.ID, t2.ID, "blocks")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected true")
	}

	// Scenario 2: reversed direction — should be false.
	// t2 does NOT block t1. Relations are directional!
	exists, err = repo.Exists(ctx, t2.ID, t1.ID, "blocks")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected false for reverse")
	}

	// Scenario 3: different type — should be false.
	// t1 blocks t2, but t1 does NOT "relates_to" t2.
	exists, err = repo.Exists(ctx, t1.ID, t2.ID, "relates_to")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected false for different type")
	}
}
