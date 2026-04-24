package service

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

func TestResequence_EmptyGroup_NoOp(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	ctx := context.Background()

	baselineEvents := len(listAllEvents(t, env.store))

	// Pick an arbitrary non-existent parent UUID — Resequence should return
	// a clean (0, nil) and emit zero events for an empty sibling group.
	parentID := uuid.New()
	n, err := env.taskSvc.Resequence(ctx, &parentID, nil)
	if err != nil {
		t.Fatalf("Resequence: %v", err)
	}
	if n != 0 {
		t.Fatalf("rewritten: got %d, want 0", n)
	}
	if after := len(listAllEvents(t, env.store)); after != baselineEvents {
		t.Fatalf("events: got +%d, want 0", after-baselineEvents)
	}
}

func TestResequence_AlreadySequential_ZeroRewrites(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	ctx := context.Background()

	p := makeTask(t, env, "p", nil)
	makeTask(t, env, "a", &p.ID) // order 1 via Create default
	makeTask(t, env, "b", &p.ID) // order 2
	makeTask(t, env, "c", &p.ID) // order 3

	baseline := countByType(listAllEvents(t, env.store))

	n, err := env.taskSvc.Resequence(ctx, &p.ID, nil)
	if err != nil {
		t.Fatalf("Resequence: %v", err)
	}
	if n != 0 {
		t.Fatalf("rewritten: got %d, want 0", n)
	}
	after := countByType(listAllEvents(t, env.store))
	if after[domain.EventTaskModified]-baseline[domain.EventTaskModified] != 0 {
		t.Fatalf("task_modified delta: got %d, want 0",
			after[domain.EventTaskModified]-baseline[domain.EventTaskModified])
	}
}

func TestResequence_OutOfOrder_RewritesAll(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	ctx := WithActor(context.Background(), "german")

	p := makeTask(t, env, "p", nil)
	a := makeTask(t, env, "a", &p.ID)
	b := makeTask(t, env, "b", &p.ID)
	c := makeTask(t, env, "c", &p.ID)

	// Seed sparse orders a=30, b=10, c=20 through the repo directly. The sort
	// (order ASC) yields b(10), c(20), a(30) — Resequence must rewrite all
	// three to 1, 2, 3 preserving that order.
	bundle, err := env.taskSvc.resolve(ctx, domain.DefaultProjectUUID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := bundle.Tasks.UpdateOrderAndParent(ctx, a.ID, &p.ID, 30, a.Version, now); err != nil {
		t.Fatalf("seed a=30: %v", err)
	}
	if _, err := bundle.Tasks.UpdateOrderAndParent(ctx, b.ID, &p.ID, 10, b.Version, now); err != nil {
		t.Fatalf("seed b=10: %v", err)
	}
	if _, err := bundle.Tasks.UpdateOrderAndParent(ctx, c.ID, &p.ID, 20, c.Version, now); err != nil {
		t.Fatalf("seed c=20: %v", err)
	}

	baseline := countByType(listAllEvents(t, env.store))
	n, err := env.taskSvc.Resequence(ctx, &p.ID, nil)
	if err != nil {
		t.Fatalf("Resequence: %v", err)
	}
	if n != 3 {
		t.Fatalf("rewritten: got %d, want 3", n)
	}
	after := countByType(listAllEvents(t, env.store))
	modDelta := after[domain.EventTaskModified] - baseline[domain.EventTaskModified]
	if modDelta != 3 {
		t.Fatalf("task_modified delta: got %d, want 3", modDelta)
	}
	if after[domain.EventTaskMoved] != 0 {
		t.Fatalf("task_moved should never be emitted by Resequence, got %d", after[domain.EventTaskMoved])
	}

	// Final orders: b=1, c=2, a=3.
	aFresh, _ := env.taskSvc.GetByID(ctx, a.ID)
	bFresh, _ := env.taskSvc.GetByID(ctx, b.ID)
	cFresh, _ := env.taskSvc.GetByID(ctx, c.ID)
	if aFresh.Order == nil || *aFresh.Order != 3 {
		t.Fatalf("a final order: got %v, want 3", aFresh.Order)
	}
	if bFresh.Order == nil || *bFresh.Order != 1 {
		t.Fatalf("b final order: got %v, want 1", bFresh.Order)
	}
	if cFresh.Order == nil || *cFresh.Order != 2 {
		t.Fatalf("c final order: got %v, want 2", cFresh.Order)
	}
}

func TestResequence_WithNullOrder_SortsLastAndGetsTopIndex(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	ctx := context.Background()

	p := makeTask(t, env, "p", nil)
	nullish := makeTask(t, env, "null", &p.ID)
	a := makeTask(t, env, "a", &p.ID)
	b := makeTask(t, env, "b", &p.ID)

	bundle, err := env.taskSvc.resolve(ctx, domain.DefaultProjectUUID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	// Directly clear nullish.Order via raw SQL — UpdateOrderAndParent cannot
	// write a SQL NULL because its order parameter is a plain float64.
	if _, err := bundle.Store.DB().Exec(
		`UPDATE tasks SET "order" = NULL, version = version + 1 WHERE id = ?`,
		nullish.ID.String(),
	); err != nil {
		t.Fatalf("clear nullish order: %v", err)
	}
	// Also force a/b to 2/3 so the sort yields (a=2, b=3, null=NULL-last).
	if _, err := bundle.Tasks.UpdateOrderAndParent(ctx, a.ID, &p.ID, 2, a.Version, now); err != nil {
		t.Fatalf("seed a=2: %v", err)
	}
	if _, err := bundle.Tasks.UpdateOrderAndParent(ctx, b.ID, &p.ID, 3, b.Version, now); err != nil {
		t.Fatalf("seed b=3: %v", err)
	}

	baseline := countByType(listAllEvents(t, env.store))
	n, err := env.taskSvc.Resequence(ctx, &p.ID, nil)
	if err != nil {
		t.Fatalf("Resequence: %v", err)
	}
	if n != 3 {
		t.Fatalf("rewritten: got %d, want 3 (null→3.0, a shifts 2→1, b shifts 3→2)", n)
	}
	after := countByType(listAllEvents(t, env.store))
	if after[domain.EventTaskModified]-baseline[domain.EventTaskModified] != 3 {
		t.Fatalf("task_modified delta: got %d, want 3",
			after[domain.EventTaskModified]-baseline[domain.EventTaskModified])
	}

	aFresh, _ := env.taskSvc.GetByID(ctx, a.ID)
	bFresh, _ := env.taskSvc.GetByID(ctx, b.ID)
	nFresh, _ := env.taskSvc.GetByID(ctx, nullish.ID)
	if aFresh.Order == nil || *aFresh.Order != 1 {
		t.Fatalf("a final order: got %v, want 1", aFresh.Order)
	}
	if bFresh.Order == nil || *bFresh.Order != 2 {
		t.Fatalf("b final order: got %v, want 2", bFresh.Order)
	}
	if nFresh.Order == nil || *nFresh.Order != 3 {
		t.Fatalf("null final order: got %v, want 3", nFresh.Order)
	}
}

func TestResequence_ActorThreadsIntoEvents(t *testing.T) {
	t.Parallel()
	env := testTaskEnv(t)
	ctx := WithActor(context.Background(), "german")

	p := makeTask(t, env, "p", nil)
	a := makeTask(t, env, "a", &p.ID)
	b := makeTask(t, env, "b", &p.ID)

	bundle, err := env.taskSvc.resolve(ctx, domain.DefaultProjectUUID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := bundle.Tasks.UpdateOrderAndParent(ctx, a.ID, &p.ID, 9, a.Version, now); err != nil {
		t.Fatalf("seed a=9: %v", err)
	}
	if _, err := bundle.Tasks.UpdateOrderAndParent(ctx, b.ID, &p.ID, 8, b.Version, now); err != nil {
		t.Fatalf("seed b=8: %v", err)
	}

	n, err := env.taskSvc.Resequence(ctx, &p.ID, nil)
	if err != nil {
		t.Fatalf("Resequence: %v", err)
	}
	if n != 2 {
		t.Fatalf("rewritten: got %d, want 2", n)
	}

	events := listAllEvents(t, env.store)
	// The two most recent task_modified events came from Resequence; both
	// must carry the actor.
	var seen int
	for i := len(events) - 1; i >= 0 && seen < 2; i-- {
		if events[i].Type != domain.EventTaskModified {
			continue
		}
		if events[i].PlayerID == nil || *events[i].PlayerID != "german" {
			t.Fatalf("event PlayerID: got %v, want *\"german\"", events[i].PlayerID)
		}
		seen++
	}
	if seen != 2 {
		t.Fatalf("expected 2 recent task_modified events, saw %d", seen)
	}
}
