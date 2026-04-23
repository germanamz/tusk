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
func newTestRelationEnv(t *testing.T) *testRelationEnv {
	t.Helper()
	bundle, projectRepo, workflowRepo := newSeededBundle(t)

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
func (e *testRelationEnv) createTask(t *testing.T, title string) *domain.Task {
	t.Helper()
	task := &domain.Task{Title: title}
	if err := e.taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("creating task %q: %v", title, err)
	}
	return task
}

func TestRelationAdd(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	rel, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if rel.SourceID != taskA.ID {
		t.Errorf("SourceID = %v, want %v", rel.SourceID, taskA.ID)
	}
	if rel.TargetID != taskB.ID {
		t.Errorf("TargetID = %v, want %v", rel.TargetID, taskB.ID)
	}
	if rel.RelationType != "relates_to" {
		t.Errorf("RelationType = %q, want %q", rel.RelationType, "relates_to")
	}
	if rel.ID.String() == "" {
		t.Error("expected non-empty ID")
	}
	if rel.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestRelationAddInvalidType(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "depends_on")
	if err == nil {
		t.Fatal("expected error for invalid relation type")
	}
}

func TestRelationAddTaskNotFound(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, "nonexist", "relates_to")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRelationAddDuplicate(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}

	_, err = env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if !errors.Is(err, domain.ErrDuplicateRelation) {
		t.Fatalf("expected ErrDuplicateRelation, got: %v", err)
	}
}

func TestRelationAddBlocksSelfReference(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCyclicBlock) {
		t.Fatalf("expected ErrCyclicBlock for self-reference, got: %v", err)
	}
}

func TestRelationAddBlocksDirectCycle(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// A blocks B — should succeed
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// B blocks A — should fail (cycle: A->B->A)
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCyclicBlock) {
		t.Fatalf("expected ErrCyclicBlock for B blocks A, got: %v", err)
	}
}

func TestRelationAddBlocksTransitiveCycle(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")
	taskC := env.createTask(t, "Task C")

	// A blocks B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// B blocks C
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskC.ShortID, "blocks")
	if err != nil {
		t.Fatalf("B blocks C: %v", err)
	}

	// C blocks A — should fail (cycle: A->B->C->A)
	_, err = env.relationSvc.Add(ctx, taskC.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCyclicBlock) {
		t.Fatalf("expected ErrCyclicBlock for C blocks A, got: %v", err)
	}
}

func TestRelationAddBlocksNoCycle(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")
	taskC := env.createTask(t, "Task C")

	// A blocks B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// B blocks C — should succeed (A->B->C is a chain, not a cycle)
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskC.ShortID, "blocks")
	if err != nil {
		t.Fatalf("B blocks C: %v", err)
	}
}

func TestRelationNonBlocksAllowsBidirectional(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// A relates_to B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("A relates_to B: %v", err)
	}

	// B relates_to A — should succeed (no cycle check for non-blocks)
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskA.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("B relates_to A: %v", err)
	}
}

func TestRelationRemove(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// Create a relation
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Remove it
	err = env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Verify it's gone
	rels, err := env.relationSvc.GetByTask(ctx, taskA.ShortID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("expected 0 relations after remove, got %d", len(rels))
	}
}

func TestRelationRemoveNotFound(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// Try to remove a relation that doesn't exist
	err := env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRelationGetByTask(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")
	taskC := env.createTask(t, "Task C")

	// A blocks B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// C relates_to A
	_, err = env.relationSvc.Add(ctx, taskC.ShortID, taskA.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("C relates_to A: %v", err)
	}

	// GetByTask for A should return both relations
	rels, err := env.relationSvc.GetByTask(ctx, taskA.ShortID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if len(rels) != 2 {
		t.Fatalf("expected 2 relations, got %d", len(rels))
	}
}

func TestRelationGetByTaskNotFound(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	_, err := env.relationSvc.GetByTask(ctx, "nonexist")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRelationGetByTaskEmpty(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")

	rels, err := env.relationSvc.GetByTask(ctx, taskA.ShortID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("expected 0 relations, got %d", len(rels))
	}
}

func TestEvents_RelationAdd_RelatesTo_EmitsRelationAdded(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := WithActor(context.Background(), "german")

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	baseline := countByType(listAllEvents(t, env.store))
	rel, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	events := listAllEvents(t, env.store)
	after := countByType(events)
	delta := func(k domain.EventType) int { return after[k] - baseline[k] }
	if delta(domain.EventRelationAdded) != 1 {
		t.Fatalf("expected +1 relation_added, got +%d", delta(domain.EventRelationAdded))
	}

	evt := firstEventOfType(t, events, domain.EventRelationAdded)
	if evt.EntityID != rel.ID.String() {
		t.Fatalf("entity_id: got %q, want %q", evt.EntityID, rel.ID.String())
	}
	if evt.EntityKind != domain.EntityRelation {
		t.Fatalf("entity_kind: got %q, want %q", evt.EntityKind, domain.EntityRelation)
	}
	if evt.PlayerID == nil || *evt.PlayerID != "german" {
		t.Fatalf("player_id: got %v, want *\"german\"", evt.PlayerID)
	}
	payload, ok := evt.Payload.(domain.RelationAddedPayload)
	if !ok {
		t.Fatalf("payload: got %T, want RelationAddedPayload", evt.Payload)
	}
	if payload.SourceShortID != taskA.ShortID {
		t.Fatalf("source_short_id: got %q, want %q", payload.SourceShortID, taskA.ShortID)
	}
	if payload.TargetShortID != taskB.ShortID {
		t.Fatalf("target_short_id: got %q, want %q", payload.TargetShortID, taskB.ShortID)
	}
	if payload.RelationKind != "relates_to" {
		t.Fatalf("relation_kind: got %q, want %q", payload.RelationKind, "relates_to")
	}
}

func TestEvents_RelationAdd_Blocks_EmitsRelationAdded(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := WithActor(context.Background(), "german")

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	baseline := countByType(listAllEvents(t, env.store))
	rel, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("Add blocks: %v", err)
	}

	events := listAllEvents(t, env.store)
	after := countByType(events)
	delta := func(k domain.EventType) int { return after[k] - baseline[k] }
	if delta(domain.EventRelationAdded) != 1 {
		t.Fatalf("expected +1 relation_added, got +%d", delta(domain.EventRelationAdded))
	}

	evt := firstEventOfType(t, events, domain.EventRelationAdded)
	if evt.EntityID != rel.ID.String() {
		t.Fatalf("entity_id: got %q, want %q", evt.EntityID, rel.ID.String())
	}
	payload := evt.Payload.(domain.RelationAddedPayload)
	if payload.RelationKind != "blocks" {
		t.Fatalf("relation_kind: got %q, want blocks", payload.RelationKind)
	}
}

func TestEvents_RelationAdd_BlocksCycle_EmitsNoEvent(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// Seed the first edge so a second edge in the reverse direction forms a cycle.
	if _, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks"); err != nil {
		t.Fatalf("seed A blocks B: %v", err)
	}

	baseline := countByType(listAllEvents(t, env.store))
	_, err := env.relationSvc.Add(ctx, taskB.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCyclicBlock) {
		t.Fatalf("expected ErrCyclicBlock, got: %v", err)
	}

	after := countByType(listAllEvents(t, env.store))
	delta := func(k domain.EventType) int { return after[k] - baseline[k] }
	if delta(domain.EventRelationAdded) != 0 {
		t.Fatalf("expected 0 relation_added on cycle rejection, got +%d", delta(domain.EventRelationAdded))
	}
}

func TestEvents_RelationRemove_EmitsRelationRemoved(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := WithActor(context.Background(), "german")

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	rel, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	baseline := countByType(listAllEvents(t, env.store))
	if err := env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	events := listAllEvents(t, env.store)
	after := countByType(events)
	delta := func(k domain.EventType) int { return after[k] - baseline[k] }
	if delta(domain.EventRelationRemoved) != 1 {
		t.Fatalf("expected +1 relation_removed, got +%d", delta(domain.EventRelationRemoved))
	}

	evt := firstEventOfType(t, events, domain.EventRelationRemoved)
	if evt.EntityID != rel.ID.String() {
		t.Fatalf("entity_id: got %q, want %q", evt.EntityID, rel.ID.String())
	}
	if evt.EntityKind != domain.EntityRelation {
		t.Fatalf("entity_kind: got %q, want %q", evt.EntityKind, domain.EntityRelation)
	}
	if evt.PlayerID == nil || *evt.PlayerID != "german" {
		t.Fatalf("player_id: got %v, want *\"german\"", evt.PlayerID)
	}
	payload, ok := evt.Payload.(domain.RelationRemovedPayload)
	if !ok {
		t.Fatalf("payload: got %T, want RelationRemovedPayload", evt.Payload)
	}
	if payload.SourceShortID != taskA.ShortID {
		t.Fatalf("source_short_id: got %q, want %q", payload.SourceShortID, taskA.ShortID)
	}
	if payload.TargetShortID != taskB.ShortID {
		t.Fatalf("target_short_id: got %q, want %q", payload.TargetShortID, taskB.ShortID)
	}
	if payload.RelationKind != "blocks" {
		t.Fatalf("relation_kind: got %q, want blocks", payload.RelationKind)
	}
}

func TestEvents_RelationRemove_NotFound_EmitsNoEvent(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	baseline := countByType(listAllEvents(t, env.store))
	err := env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}

	after := countByType(listAllEvents(t, env.store))
	delta := func(k domain.EventType) int { return after[k] - baseline[k] }
	if delta(domain.EventRelationRemoved) != 0 {
		t.Fatalf("expected 0 relation_removed on missing relation, got +%d", delta(domain.EventRelationRemoved))
	}
}

func TestEvents_Relation_ActorPropagation(t *testing.T) {
	cases := []struct {
		name  string
		actor string
	}{
		{name: "with_actor", actor: "german"},
		{name: "no_actor", actor: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestRelationEnv(t)
			ctx := WithActor(context.Background(), tc.actor)

			taskA := env.createTask(t, "Task A")
			taskB := env.createTask(t, "Task B")

			if _, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to"); err != nil {
				t.Fatalf("Add: %v", err)
			}

			evt := firstEventOfType(t, listAllEvents(t, env.store), domain.EventRelationAdded)
			if tc.actor == "" {
				if evt.PlayerID != nil {
					t.Fatalf("player_id: got %v, want nil", *evt.PlayerID)
				}
			} else {
				if evt.PlayerID == nil || *evt.PlayerID != tc.actor {
					t.Fatalf("player_id: got %v, want %q", evt.PlayerID, tc.actor)
				}
			}
		})
	}
}
