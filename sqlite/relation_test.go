package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
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
func TestRelationCreate(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	repo := NewRelationRepo(store.DB())
	ctx := context.Background()

	// Create two tasks. Relations connect two tasks, so we need both to exist.
	taskOne := newTestTask()
	taskTwo := newTestTask()
	mustCreateTask(test, taskRepo, taskOne)
	mustCreateTask(test, taskRepo, taskTwo)

	// Create a "blocks" relation: taskOne blocks taskTwo.
	rel := newTestRelation(taskOne.ID, taskTwo.ID, "blocks")

	if err := repo.Create(ctx, rel); err != nil {
		test.Fatalf("Create: %v", err)
	}

	// Verify via GetByTask. Since taskOne is the source, it should appear.
	rels, err := repo.GetByTask(ctx, taskOne.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(rels) != 1 {
		test.Fatalf("expected 1, got %d", len(rels))
	}
	if rels[0].RelationType != "blocks" {
		test.Fatalf("expected blocks, got %s", rels[0].RelationType)
	}
}

// TestRelationCreateDuplicate verifies that inserting the same (source, target, type)
// combination twice returns domain.ErrDuplicateRelation.
//
// This tests the UNIQUE(source_id, target_id, relation_type) constraint and our
// isUniqueViolation helper that translates the SQLite error into a domain error.
func TestRelationCreateDuplicate(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	repo := NewRelationRepo(store.DB())
	ctx := context.Background()

	taskOne := newTestTask()
	taskTwo := newTestTask()
	mustCreateTask(test, taskRepo, taskOne)
	mustCreateTask(test, taskRepo, taskTwo)

	// First insert: should succeed.
	if err := repo.Create(ctx, newTestRelation(taskOne.ID, taskTwo.ID, "blocks")); err != nil {
		test.Fatal(err)
	}

	// Second insert with same source, target, and type: should fail.
	// Note that newTestRelation generates a NEW uuid.UUID for the ID field,
	// but the UNIQUE constraint is on (source_id, target_id, relation_type),
	// not on id. So even though the IDs differ, the constraint fires.
	err := repo.Create(ctx, newTestRelation(taskOne.ID, taskTwo.ID, "blocks"))
	if err != domain.ErrDuplicateRelation {
		test.Fatalf("expected ErrDuplicateRelation, got %v", err)
	}
}

// TestRelationDelete verifies that Delete removes a relation and that
// GetByTask no longer returns it.
func TestRelationDelete(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	repo := NewRelationRepo(store.DB())
	ctx := context.Background()

	taskOne := newTestTask()
	taskTwo := newTestTask()
	mustCreateTask(test, taskRepo, taskOne)
	mustCreateTask(test, taskRepo, taskTwo)

	rel := newTestRelation(taskOne.ID, taskTwo.ID, "relates_to")

	if err := repo.Create(ctx, rel); err != nil {
		test.Fatal(err)
	}

	// Delete the relation.
	if err := repo.Delete(ctx, rel.ID); err != nil {
		test.Fatalf("Delete: %v", err)
	}

	// Verify it is gone.
	rels, err := repo.GetByTask(ctx, taskOne.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(rels) != 0 {
		test.Fatalf("expected 0 after delete, got %d", len(rels))
	}
}

// TestRelationDeleteNotFound verifies that deleting a non-existent relation
// returns domain.ErrNotFound.
func TestRelationDeleteNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewRelationRepo(store.DB())
	err := repo.Delete(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestRelationDeleteByFields verifies that DeleteByFields removes a relation
// matching the exact (source, target, type) triple.
func TestRelationDeleteByFields(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	repo := NewRelationRepo(store.DB())
	ctx := context.Background()

	taskOne := newTestTask()
	taskTwo := newTestTask()
	mustCreateTask(test, taskRepo, taskOne)
	mustCreateTask(test, taskRepo, taskTwo)

	rel := newTestRelation(taskOne.ID, taskTwo.ID, "blocks")

	if err := repo.Create(ctx, rel); err != nil {
		test.Fatal(err)
	}

	if err := repo.DeleteByFields(ctx, taskOne.ID, taskTwo.ID, "blocks"); err != nil {
		test.Fatalf("DeleteByFields: %v", err)
	}

	rels, err := repo.GetByTask(ctx, taskOne.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(rels) != 0 {
		test.Fatalf("expected 0 after delete, got %d", len(rels))
	}
}

// TestRelationDeleteByFieldsNotFound verifies that DeleteByFields returns
// domain.ErrNotFound when no matching relation exists.
func TestRelationDeleteByFieldsNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewRelationRepo(store.DB())
	err := repo.DeleteByFields(context.Background(), uuid.New(), uuid.New(), "blocks")
	if err != domain.ErrNotFound {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestRelationGetByTask verifies that GetByTask returns relations where the
// task is EITHER the source OR the target.
//
// Setup:
//   - taskOne -> taskTwo (blocks) — taskOne is source
//   - taskThree -> taskOne (relates_to) — taskOne is target
//
// GetByTask(taskOne) should return BOTH relations (2 total) because taskOne appears
// as source in one and target in the other.
func TestRelationGetByTask(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	repo := NewRelationRepo(store.DB())
	ctx := context.Background()

	taskOne := newTestTask()
	taskTwo := newTestTask()
	taskThree := newTestTask()
	mustCreateTask(test, taskRepo, taskOne)
	mustCreateTask(test, taskRepo, taskTwo)
	mustCreateTask(test, taskRepo, taskThree)

	// taskOne is source in this relation.
	if err := repo.Create(ctx, newTestRelation(taskOne.ID, taskTwo.ID, "blocks")); err != nil {
		test.Fatal(err)
	}

	// taskOne is target in this relation.
	if err := repo.Create(ctx, newTestRelation(taskThree.ID, taskOne.ID, "relates_to")); err != nil {
		test.Fatal(err)
	}

	rels, err := repo.GetByTask(ctx, taskOne.ID)

	if err != nil {
		test.Fatal(err)
	}

	// Both relations involve taskOne, so both should be returned.
	if len(rels) != 2 {
		test.Fatalf("expected 2, got %d", len(rels))
	}
}

// TestRelationGetBlocking verifies that GetBlocking returns only relations
// where the given task is the SOURCE and the type is "blocks".
//
// Setup:
//   - taskOne blocks taskTwo — should be returned (taskOne is source, type is "blocks")
//   - taskOne blocks taskThree — should be returned (taskOne is source, type is "blocks")
//   - taskTwo relates_to taskThree — should NOT be returned (different type)
//
// GetBlocking(taskOne) should return 2 relations.
func TestRelationGetBlocking(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	repo := NewRelationRepo(store.DB())
	ctx := context.Background()

	taskOne := newTestTask()
	taskTwo := newTestTask()
	taskThree := newTestTask()
	mustCreateTask(test, taskRepo, taskOne)
	mustCreateTask(test, taskRepo, taskTwo)
	mustCreateTask(test, taskRepo, taskThree)

	// taskOne is the blocker in both of these.
	if err := repo.Create(ctx, newTestRelation(taskOne.ID, taskTwo.ID, "blocks")); err != nil {
		test.Fatal(err)
	}

	if err := repo.Create(ctx, newTestRelation(taskOne.ID, taskThree.ID, "blocks")); err != nil {
		test.Fatal(err)
	}

	// This is a different type — should not appear in GetBlocking results.
	if err := repo.Create(ctx, newTestRelation(taskTwo.ID, taskThree.ID, "relates_to")); err != nil {
		test.Fatal(err)
	}

	blocking, err := repo.GetBlocking(ctx, taskOne.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(blocking) != 2 {
		test.Fatalf("expected 2, got %d", len(blocking))
	}
}

// TestRelationGetBlockedBy verifies that GetBlockedBy returns only relations
// where the given task is the TARGET and the type is "blocks".
//
// Setup:
//   - taskOne blocks taskTwo — GetBlockedBy(taskTwo) should return this (taskTwo is the target)
//
// GetBlockedBy(taskTwo) should return 1 relation, and its SourceID should be taskOne.
func TestRelationGetBlockedBy(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	repo := NewRelationRepo(store.DB())
	ctx := context.Background()

	taskOne := newTestTask()
	taskTwo := newTestTask()
	mustCreateTask(test, taskRepo, taskOne)
	mustCreateTask(test, taskRepo, taskTwo)

	// taskOne blocks taskTwo: taskOne is the source (blocker), taskTwo is the target (blocked).
	if err := repo.Create(ctx, newTestRelation(taskOne.ID, taskTwo.ID, "blocks")); err != nil {
		test.Fatal(err)
	}

	// Ask "what is blocking taskTwo?" — should return the relation with taskOne as source.
	blockedBy, err := repo.GetBlockedBy(ctx, taskTwo.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(blockedBy) != 1 {
		test.Fatalf("expected 1, got %d", len(blockedBy))
	}
	// Verify that the source of the blocking relation is taskOne.
	if blockedBy[0].SourceID != taskOne.ID {
		test.Fatal("expected source to be taskOne")
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
func TestRelationExists(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	repo := NewRelationRepo(store.DB())
	ctx := context.Background()

	taskOne := newTestTask()
	taskTwo := newTestTask()
	mustCreateTask(test, taskRepo, taskOne)
	mustCreateTask(test, taskRepo, taskTwo)

	// Create: taskOne blocks taskTwo.
	if err := repo.Create(ctx, newTestRelation(taskOne.ID, taskTwo.ID, "blocks")); err != nil {
		test.Fatal(err)
	}

	// Scenario 1: exact match — should be true.
	exists, err := repo.Exists(ctx, taskOne.ID, taskTwo.ID, "blocks")

	if err != nil {
		test.Fatal(err)
	}

	if !exists {
		test.Fatal("expected true")
	}

	// Scenario 2: reversed direction — should be false.
	// taskTwo does NOT block taskOne. Relations are directional!
	exists, err = repo.Exists(ctx, taskTwo.ID, taskOne.ID, "blocks")

	if err != nil {
		test.Fatal(err)
	}

	if exists {
		test.Fatal("expected false for reverse")
	}

	// Scenario 3: different type — should be false.
	// taskOne blocks taskTwo, but taskOne does NOT "relates_to" taskTwo.
	exists, err = repo.Exists(ctx, taskOne.ID, taskTwo.ID, "relates_to")

	if err != nil {
		test.Fatal(err)
	}

	if exists {
		test.Fatal("expected false for different type")
	}
}
