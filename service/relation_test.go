package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite"
)

type testRelationEnv struct {
	relationSvc *RelationService
	taskSvc     *TaskService
	store       *sqlite.Store
}

// newTestRelationEnv creates a fully wired test environment for RelationService tests.
// The DB has all migrations applied, including the default project and kanban workflow.
func newTestRelationEnv(test *testing.T) *testRelationEnv {
	test.Helper()
	bundle, projectRepo, workflowRepo := newSeededBundle(test)

	resolver, projects := singleBundleResolver(bundle, domain.DefaultProjectUUID)
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	taskSvc := NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, nil)
	relationSvc := NewRelationService(resolver, projects)

	return &testRelationEnv{
		relationSvc: relationSvc,
		taskSvc:     taskSvc,
		store:       bundle.Store,
	}
}

// createTask is a helper that creates a task with the given title and returns it.
func (env *testRelationEnv) createTask(test *testing.T, title string) *domain.Task {
	test.Helper()
	task := &domain.Task{Title: title}
	if err := env.taskSvc.Create(context.Background(), task); err != nil {
		test.Fatalf("creating task %q: %v", title, err)
	}
	return task
}

func TestRelationAdd(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	rel, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")

	if err != nil {
		test.Fatalf("Add: %v", err)
	}

	if rel.SourceID != taskA.ID {
		test.Errorf("SourceID = %v, want %v", rel.SourceID, taskA.ID)
	}
	if rel.TargetID != taskB.ID {
		test.Errorf("TargetID = %v, want %v", rel.TargetID, taskB.ID)
	}
	if rel.RelationType != "relates_to" {
		test.Errorf("RelationType = %q, want %q", rel.RelationType, "relates_to")
	}
	if rel.ID.String() == "" {
		test.Error("expected non-empty ID")
	}
	if rel.CreatedAt.IsZero() {
		test.Error("expected non-zero CreatedAt")
	}
}

func TestRelationAddInvalidType(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "depends_on")
	if err == nil {
		test.Fatal("expected error for invalid relation type")
	}
}

func TestRelationAddTaskNotFound(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, "nonexist", "relates_to")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRelationAddDuplicate(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	_, firstErr := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")

	if firstErr != nil {
		test.Fatalf("first Add: %v", firstErr)
	}

	_, secondErr := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if !errors.Is(secondErr, domain.ErrDuplicateRelation) {
		test.Fatalf("expected ErrDuplicateRelation, got: %v", secondErr)
	}
}

func TestRelationAddBlocksSelfReference(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCyclicBlock) {
		test.Fatalf("expected ErrCyclicBlock for self-reference, got: %v", err)
	}
}

func TestRelationAddBlocksDirectCycle(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	// A blocks B — should succeed
	_, firstErr := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")

	if firstErr != nil {
		test.Fatalf("A blocks B: %v", firstErr)
	}

	// B blocks A — should fail (cycle: A->B->A)
	_, secondErr := env.relationSvc.Add(ctx, taskB.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(secondErr, domain.ErrCyclicBlock) {
		test.Fatalf("expected ErrCyclicBlock for B blocks A, got: %v", secondErr)
	}
}

func TestRelationAddBlocksTransitiveCycle(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")
	taskC := env.createTask(test, "Task C")

	// A blocks B
	_, firstErr := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")

	if firstErr != nil {
		test.Fatalf("A blocks B: %v", firstErr)
	}

	// B blocks C
	_, secondErr := env.relationSvc.Add(ctx, taskB.ShortID, taskC.ShortID, "blocks")

	if secondErr != nil {
		test.Fatalf("B blocks C: %v", secondErr)
	}

	// C blocks A — should fail (cycle: A->B->C->A)
	_, thirdErr := env.relationSvc.Add(ctx, taskC.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(thirdErr, domain.ErrCyclicBlock) {
		test.Fatalf("expected ErrCyclicBlock for C blocks A, got: %v", thirdErr)
	}
}

func TestRelationAddBlocksNoCycle(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")
	taskC := env.createTask(test, "Task C")

	// A blocks B
	_, firstErr := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")

	if firstErr != nil {
		test.Fatalf("A blocks B: %v", firstErr)
	}

	// B blocks C — should succeed (A->B->C is a chain, not a cycle)
	_, secondErr := env.relationSvc.Add(ctx, taskB.ShortID, taskC.ShortID, "blocks")

	if secondErr != nil {
		test.Fatalf("B blocks C: %v", secondErr)
	}
}

func TestRelationNonBlocksAllowsBidirectional(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	// A relates_to B
	_, firstErr := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")

	if firstErr != nil {
		test.Fatalf("A relates_to B: %v", firstErr)
	}

	// B relates_to A — should succeed (no cycle check for non-blocks)
	_, secondErr := env.relationSvc.Add(ctx, taskB.ShortID, taskA.ShortID, "relates_to")

	if secondErr != nil {
		test.Fatalf("B relates_to A: %v", secondErr)
	}
}

func TestRelationRemove(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	// Create a relation
	_, addErr := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")

	if addErr != nil {
		test.Fatalf("Add: %v", addErr)
	}

	// Remove it
	removeErr := env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks")

	if removeErr != nil {
		test.Fatalf("Remove: %v", removeErr)
	}

	// Verify it's gone
	rels, getErr := env.relationSvc.GetByTask(ctx, taskA.ShortID)

	if getErr != nil {
		test.Fatalf("GetByTask: %v", getErr)
	}

	if len(rels) != 0 {
		test.Fatalf("expected 0 relations after remove, got %d", len(rels))
	}
}

func TestRelationRemoveNotFound(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	// Try to remove a relation that doesn't exist
	err := env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRelationGetByTask(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")
	taskC := env.createTask(test, "Task C")

	// A blocks B
	_, firstErr := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")

	if firstErr != nil {
		test.Fatalf("A blocks B: %v", firstErr)
	}

	// C relates_to A
	_, secondErr := env.relationSvc.Add(ctx, taskC.ShortID, taskA.ShortID, "relates_to")

	if secondErr != nil {
		test.Fatalf("C relates_to A: %v", secondErr)
	}

	// GetByTask for A should return both relations
	rels, getErr := env.relationSvc.GetByTask(ctx, taskA.ShortID)

	if getErr != nil {
		test.Fatalf("GetByTask: %v", getErr)
	}

	if len(rels) != 2 {
		test.Fatalf("expected 2 relations, got %d", len(rels))
	}
}

func TestRelationGetByTaskNotFound(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	_, err := env.relationSvc.GetByTask(ctx, "nonexist")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRelationGetByTaskEmpty(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")

	rels, getErr := env.relationSvc.GetByTask(ctx, taskA.ShortID)

	if getErr != nil {
		test.Fatalf("GetByTask: %v", getErr)
	}

	if len(rels) != 0 {
		test.Fatalf("expected 0 relations, got %d", len(rels))
	}
}

func TestEvents_RelationAdd_RelatesTo_EmitsRelationAdded(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := WithActor(context.Background(), "german")

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	baseline := countByType(listAllEvents(test, env.store))
	rel, addErr := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")

	if addErr != nil {
		test.Fatalf("Add: %v", addErr)
	}

	events := listAllEvents(test, env.store)
	after := countByType(events)
	delta := func(key domain.EventType) int { return after[key] - baseline[key] }
	if delta(domain.EventRelationAdded) != 1 {
		test.Fatalf("expected +1 relation_added, got +%d", delta(domain.EventRelationAdded))
	}

	event := firstEventOfType(test, events, domain.EventRelationAdded)
	if event.EntityID != rel.ID.String() {
		test.Fatalf("entity_id: got %q, want %q", event.EntityID, rel.ID.String())
	}
	if event.EntityKind != domain.EntityRelation {
		test.Fatalf("entity_kind: got %q, want %q", event.EntityKind, domain.EntityRelation)
	}
	if event.PlayerID == nil || *event.PlayerID != "german" {
		test.Fatalf("player_id: got %v, want *\"german\"", event.PlayerID)
	}
	payload, ok := event.Payload.(domain.RelationAddedPayload)
	if !ok {
		test.Fatalf("payload: got %T, want RelationAddedPayload", event.Payload)
	}
	if payload.SourceShortID != taskA.ShortID {
		test.Fatalf("source_short_id: got %q, want %q", payload.SourceShortID, taskA.ShortID)
	}
	if payload.TargetShortID != taskB.ShortID {
		test.Fatalf("target_short_id: got %q, want %q", payload.TargetShortID, taskB.ShortID)
	}
	if payload.RelationKind != "relates_to" {
		test.Fatalf("relation_kind: got %q, want %q", payload.RelationKind, "relates_to")
	}
}

func TestEvents_RelationAdd_Blocks_EmitsRelationAdded(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := WithActor(context.Background(), "german")

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	baseline := countByType(listAllEvents(test, env.store))
	rel, addErr := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")

	if addErr != nil {
		test.Fatalf("Add blocks: %v", addErr)
	}

	events := listAllEvents(test, env.store)
	after := countByType(events)
	delta := func(key domain.EventType) int { return after[key] - baseline[key] }
	if delta(domain.EventRelationAdded) != 1 {
		test.Fatalf("expected +1 relation_added, got +%d", delta(domain.EventRelationAdded))
	}

	event := firstEventOfType(test, events, domain.EventRelationAdded)
	if event.EntityID != rel.ID.String() {
		test.Fatalf("entity_id: got %q, want %q", event.EntityID, rel.ID.String())
	}
	payload := event.Payload.(domain.RelationAddedPayload)
	if payload.RelationKind != "blocks" {
		test.Fatalf("relation_kind: got %q, want blocks", payload.RelationKind)
	}
}

func TestEvents_RelationAdd_BlocksCycle_EmitsNoEvent(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	// Seed the first edge so a second edge in the reverse direction forms a cycle.
	if _, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks"); err != nil {
		test.Fatalf("seed A blocks B: %v", err)
	}

	baseline := countByType(listAllEvents(test, env.store))
	_, cycleErr := env.relationSvc.Add(ctx, taskB.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(cycleErr, domain.ErrCyclicBlock) {
		test.Fatalf("expected ErrCyclicBlock, got: %v", cycleErr)
	}

	after := countByType(listAllEvents(test, env.store))
	delta := func(key domain.EventType) int { return after[key] - baseline[key] }
	if delta(domain.EventRelationAdded) != 0 {
		test.Fatalf("expected 0 relation_added on cycle rejection, got +%d", delta(domain.EventRelationAdded))
	}
}

func TestEvents_RelationRemove_EmitsRelationRemoved(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := WithActor(context.Background(), "german")

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	rel, addErr := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")

	if addErr != nil {
		test.Fatalf("Add: %v", addErr)
	}

	baseline := countByType(listAllEvents(test, env.store))
	if err := env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks"); err != nil {
		test.Fatalf("Remove: %v", err)
	}

	events := listAllEvents(test, env.store)
	after := countByType(events)
	delta := func(key domain.EventType) int { return after[key] - baseline[key] }
	if delta(domain.EventRelationRemoved) != 1 {
		test.Fatalf("expected +1 relation_removed, got +%d", delta(domain.EventRelationRemoved))
	}

	event := firstEventOfType(test, events, domain.EventRelationRemoved)
	if event.EntityID != rel.ID.String() {
		test.Fatalf("entity_id: got %q, want %q", event.EntityID, rel.ID.String())
	}
	if event.EntityKind != domain.EntityRelation {
		test.Fatalf("entity_kind: got %q, want %q", event.EntityKind, domain.EntityRelation)
	}
	if event.PlayerID == nil || *event.PlayerID != "german" {
		test.Fatalf("player_id: got %v, want *\"german\"", event.PlayerID)
	}
	payload, ok := event.Payload.(domain.RelationRemovedPayload)
	if !ok {
		test.Fatalf("payload: got %T, want RelationRemovedPayload", event.Payload)
	}
	if payload.SourceShortID != taskA.ShortID {
		test.Fatalf("source_short_id: got %q, want %q", payload.SourceShortID, taskA.ShortID)
	}
	if payload.TargetShortID != taskB.ShortID {
		test.Fatalf("target_short_id: got %q, want %q", payload.TargetShortID, taskB.ShortID)
	}
	if payload.RelationKind != "blocks" {
		test.Fatalf("relation_kind: got %q, want blocks", payload.RelationKind)
	}
}

func TestEvents_RelationRemove_NotFound_EmitsNoEvent(test *testing.T) {
	env := newTestRelationEnv(test)
	ctx := context.Background()

	taskA := env.createTask(test, "Task A")
	taskB := env.createTask(test, "Task B")

	baseline := countByType(listAllEvents(test, env.store))
	err := env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got: %v", err)
	}

	after := countByType(listAllEvents(test, env.store))
	delta := func(key domain.EventType) int { return after[key] - baseline[key] }
	if delta(domain.EventRelationRemoved) != 0 {
		test.Fatalf("expected 0 relation_removed on missing relation, got +%d", delta(domain.EventRelationRemoved))
	}
}

func TestEvents_Relation_ActorPropagation(test *testing.T) {
	cases := []struct {
		name  string
		actor string
	}{
		{name: "with_actor", actor: "german"},
		{name: "no_actor", actor: ""},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			env := newTestRelationEnv(test)
			ctx := WithActor(context.Background(), testCase.actor)

			taskA := env.createTask(test, "Task A")
			taskB := env.createTask(test, "Task B")

			if _, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to"); err != nil {
				test.Fatalf("Add: %v", err)
			}

			event := firstEventOfType(test, listAllEvents(test, env.store), domain.EventRelationAdded)
			if testCase.actor == "" {
				if event.PlayerID != nil {
					test.Fatalf("player_id: got %v, want nil", *event.PlayerID)
				}
			} else {
				if event.PlayerID == nil || *event.PlayerID != testCase.actor {
					test.Fatalf("player_id: got %v, want %q", event.PlayerID, testCase.actor)
				}
			}
		})
	}
}
