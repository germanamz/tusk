package service

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

func TestResequence_EmptyGroup_NoOp(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := context.Background()

	baselineEvents := len(listAllEvents(test, env.store))

	// Pick an arbitrary non-existent parent UUID — Resequence should return
	// a clean (0, nil) and emit zero events for an empty sibling group.
	parentID := uuid.New()

	rewritten, err := env.taskSvc.Resequence(ctx, &parentID, nil)

	if err != nil {
		test.Fatalf("Resequence: %v", err)
	}

	if rewritten != 0 {
		test.Fatalf("rewritten: got %d, want 0", rewritten)
	}

	if after := len(listAllEvents(test, env.store)); after != baselineEvents {
		test.Fatalf("events: got +%d, want 0", after-baselineEvents)
	}
}

func TestResequence_AlreadySequential_ZeroRewrites(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := context.Background()

	parent := makeTask(test, env, "p", nil)
	makeTask(test, env, "a", &parent.ID) // order 1 via Create default
	makeTask(test, env, "b", &parent.ID) // order 2
	makeTask(test, env, "c", &parent.ID) // order 3

	baseline := countByType(listAllEvents(test, env.store))

	rewritten, err := env.taskSvc.Resequence(ctx, &parent.ID, nil)

	if err != nil {
		test.Fatalf("Resequence: %v", err)
	}

	if rewritten != 0 {
		test.Fatalf("rewritten: got %d, want 0", rewritten)
	}

	after := countByType(listAllEvents(test, env.store))

	if after[domain.EventTaskModified]-baseline[domain.EventTaskModified] != 0 {
		test.Fatalf("task_modified delta: got %d, want 0",
			after[domain.EventTaskModified]-baseline[domain.EventTaskModified])
	}
}

func TestResequence_OutOfOrder_RewritesAll(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := WithActor(context.Background(), "german")

	parent := makeTask(test, env, "p", nil)
	taskA := makeTask(test, env, "a", &parent.ID)
	taskB := makeTask(test, env, "b", &parent.ID)
	taskC := makeTask(test, env, "c", &parent.ID)

	// Seed sparse orders a=30, b=10, c=20 through the repo directly. The sort
	// (order ASC) yields b(10), c(20), a(30) — Resequence must rewrite all
	// three to 1, 2, 3 preserving that order.
	bundle, resolveErr := env.taskSvc.resolve(ctx, domain.DefaultProjectUUID)

	if resolveErr != nil {
		test.Fatalf("resolve: %v", resolveErr)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)

	if _, seedAErr := bundle.Tasks.UpdateOrderAndParent(ctx, taskA.ID, &parent.ID, 30, taskA.Version, now); seedAErr != nil {
		test.Fatalf("seed a=30: %v", seedAErr)
	}

	if _, seedBErr := bundle.Tasks.UpdateOrderAndParent(ctx, taskB.ID, &parent.ID, 10, taskB.Version, now); seedBErr != nil {
		test.Fatalf("seed b=10: %v", seedBErr)
	}

	if _, seedCErr := bundle.Tasks.UpdateOrderAndParent(ctx, taskC.ID, &parent.ID, 20, taskC.Version, now); seedCErr != nil {
		test.Fatalf("seed c=20: %v", seedCErr)
	}

	baseline := countByType(listAllEvents(test, env.store))

	rewritten, err := env.taskSvc.Resequence(ctx, &parent.ID, nil)

	if err != nil {
		test.Fatalf("Resequence: %v", err)
	}

	if rewritten != 3 {
		test.Fatalf("rewritten: got %d, want 3", rewritten)
	}

	after := countByType(listAllEvents(test, env.store))
	modDelta := after[domain.EventTaskModified] - baseline[domain.EventTaskModified]

	if modDelta != 3 {
		test.Fatalf("task_modified delta: got %d, want 3", modDelta)
	}

	if after[domain.EventTaskMoved] != 0 {
		test.Fatalf("task_moved should never be emitted by Resequence, got %d", after[domain.EventTaskMoved])
	}

	// Final orders: b=1, c=2, a=3.
	aFresh, _ := env.taskSvc.GetByID(ctx, taskA.ID)
	bFresh, _ := env.taskSvc.GetByID(ctx, taskB.ID)
	cFresh, _ := env.taskSvc.GetByID(ctx, taskC.ID)

	if aFresh.Order == nil || *aFresh.Order != 3 {
		test.Fatalf("a final order: got %v, want 3", aFresh.Order)
	}

	if bFresh.Order == nil || *bFresh.Order != 1 {
		test.Fatalf("b final order: got %v, want 1", bFresh.Order)
	}

	if cFresh.Order == nil || *cFresh.Order != 2 {
		test.Fatalf("c final order: got %v, want 2", cFresh.Order)
	}
}

func TestResequence_WithNullOrder_SortsLastAndGetsTopIndex(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := context.Background()

	parent := makeTask(test, env, "p", nil)
	nullish := makeTask(test, env, "null", &parent.ID)
	taskA := makeTask(test, env, "a", &parent.ID)
	taskB := makeTask(test, env, "b", &parent.ID)

	bundle, resolveErr := env.taskSvc.resolve(ctx, domain.DefaultProjectUUID)

	if resolveErr != nil {
		test.Fatalf("resolve: %v", resolveErr)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)

	// Directly clear nullish.Order via raw SQL — UpdateOrderAndParent cannot
	// write a SQL NULL because its order parameter is a plain float64.
	if _, nullErr := bundle.Store.DB().Exec(
		`UPDATE tasks SET "order" = NULL, version = version + 1 WHERE id = ?`,
		nullish.ID.String(),
	); nullErr != nil {
		test.Fatalf("clear nullish order: %v", nullErr)
	}

	// Also force a/b to 2/3 so the sort yields (a=2, b=3, null=NULL-last).
	if _, seedAErr := bundle.Tasks.UpdateOrderAndParent(ctx, taskA.ID, &parent.ID, 2, taskA.Version, now); seedAErr != nil {
		test.Fatalf("seed a=2: %v", seedAErr)
	}

	if _, seedBErr := bundle.Tasks.UpdateOrderAndParent(ctx, taskB.ID, &parent.ID, 3, taskB.Version, now); seedBErr != nil {
		test.Fatalf("seed b=3: %v", seedBErr)
	}

	baseline := countByType(listAllEvents(test, env.store))

	rewritten, err := env.taskSvc.Resequence(ctx, &parent.ID, nil)

	if err != nil {
		test.Fatalf("Resequence: %v", err)
	}

	if rewritten != 3 {
		test.Fatalf("rewritten: got %d, want 3 (null→3.0, a shifts 2→1, b shifts 3→2)", rewritten)
	}

	after := countByType(listAllEvents(test, env.store))

	if after[domain.EventTaskModified]-baseline[domain.EventTaskModified] != 3 {
		test.Fatalf("task_modified delta: got %d, want 3",
			after[domain.EventTaskModified]-baseline[domain.EventTaskModified])
	}

	aFresh, _ := env.taskSvc.GetByID(ctx, taskA.ID)
	bFresh, _ := env.taskSvc.GetByID(ctx, taskB.ID)
	nFresh, _ := env.taskSvc.GetByID(ctx, nullish.ID)

	if aFresh.Order == nil || *aFresh.Order != 1 {
		test.Fatalf("a final order: got %v, want 1", aFresh.Order)
	}

	if bFresh.Order == nil || *bFresh.Order != 2 {
		test.Fatalf("b final order: got %v, want 2", bFresh.Order)
	}

	if nFresh.Order == nil || *nFresh.Order != 3 {
		test.Fatalf("null final order: got %v, want 3", nFresh.Order)
	}
}

func TestResequence_ActorThreadsIntoEvents(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := WithActor(context.Background(), "german")

	parent := makeTask(test, env, "p", nil)
	taskA := makeTask(test, env, "a", &parent.ID)
	taskB := makeTask(test, env, "b", &parent.ID)

	bundle, resolveErr := env.taskSvc.resolve(ctx, domain.DefaultProjectUUID)

	if resolveErr != nil {
		test.Fatalf("resolve: %v", resolveErr)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)

	if _, seedAErr := bundle.Tasks.UpdateOrderAndParent(ctx, taskA.ID, &parent.ID, 9, taskA.Version, now); seedAErr != nil {
		test.Fatalf("seed a=9: %v", seedAErr)
	}

	if _, seedBErr := bundle.Tasks.UpdateOrderAndParent(ctx, taskB.ID, &parent.ID, 8, taskB.Version, now); seedBErr != nil {
		test.Fatalf("seed b=8: %v", seedBErr)
	}

	rewritten, err := env.taskSvc.Resequence(ctx, &parent.ID, nil)

	if err != nil {
		test.Fatalf("Resequence: %v", err)
	}

	if rewritten != 2 {
		test.Fatalf("rewritten: got %d, want 2", rewritten)
	}

	events := listAllEvents(test, env.store)
	// The two most recent task_modified events came from Resequence; both
	// must carry the actor.
	var seen int

	for index := len(events) - 1; index >= 0 && seen < 2; index-- {
		if events[index].Type != domain.EventTaskModified {
			continue
		}

		if events[index].PlayerID == nil || *events[index].PlayerID != "german" {
			test.Fatalf("event PlayerID: got %v, want *\"german\"", events[index].PlayerID)
		}

		seen++
	}

	if seen != 2 {
		test.Fatalf("expected 2 recent task_modified events, saw %d", seen)
	}
}
