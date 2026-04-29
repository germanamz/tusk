package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// moveEnv returns a ready-to-use TaskService env; thin wrapper around
// testTaskEnv so move tests read linearly.
func moveEnv(test *testing.T) *testEnv {
	test.Helper()
	return testTaskEnv(test)
}

// makeTask creates a task through the service and returns the persisted row.
func makeTask(test *testing.T, env *testEnv, title string, parent *uuid.UUID) *domain.Task {
	test.Helper()
	task := &domain.Task{Title: title}

	if parent != nil {
		task.ParentID = parent
	}

	if err := env.taskSvc.Create(context.Background(), task); err != nil {
		test.Fatalf("Create %q: %v", title, err)
	}

	return task
}

func lastMovedEvent(test *testing.T, env *testEnv) *domain.Event {
	test.Helper()
	events := listAllEvents(test, env.store)

	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Type == domain.EventTaskMoved {
			return events[index]
		}
	}

	test.Fatalf("no task_moved event found among %d events", len(events))
	return nil
}

func TestMove_Before_SameParent_UsesMidpoint(test *testing.T) {
	test.Parallel()
	env := moveEnv(test)
	ctx := WithActor(context.Background(), "german")

	taskA := makeTask(test, env, "a", nil) // order 1
	taskB := makeTask(test, env, "b", nil) // order 2
	taskC := makeTask(test, env, "c", nil) // order 3

	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   taskC.ID,
		Version:  taskC.Version,
		Position: MovePositionBefore,
		TargetID: &taskB.ID,
	})

	if err != nil {
		test.Fatalf("Move: %v", err)
	}

	if moved.Order == nil || *moved.Order != 1.5 {
		test.Fatalf("order: got %v, want 1.5", moved.Order)
	}

	event := lastMovedEvent(test, env)
	payload := event.Payload.(domain.TaskMovedPayload)

	if payload.OldOrder == nil || *payload.OldOrder != 3.0 {
		test.Fatalf("old_order: got %v, want 3.0", payload.OldOrder)
	}

	if payload.NewOrder == nil || *payload.NewOrder != 1.5 {
		test.Fatalf("new_order: got %v, want 1.5", payload.NewOrder)
	}

	if payload.OldParentID != nil || payload.NewParentID != nil {
		test.Fatalf("parents: got old=%v new=%v, want nil/nil", payload.OldParentID, payload.NewParentID)
	}

	_ = taskA
}

func TestMove_After_SameParent_UsesMidpoint(test *testing.T) {
	test.Parallel()
	env := moveEnv(test)
	ctx := context.Background()

	taskA := makeTask(test, env, "a", nil) // order 1
	taskB := makeTask(test, env, "b", nil) // order 2
	taskC := makeTask(test, env, "c", nil) // order 3

	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   taskA.ID,
		Version:  taskA.Version,
		Position: MovePositionAfter,
		TargetID: &taskB.ID,
	})

	if err != nil {
		test.Fatalf("Move: %v", err)
	}

	if moved.Order == nil || *moved.Order != 2.5 {
		test.Fatalf("order: got %v, want 2.5", moved.Order)
	}

	_ = taskC
}

func TestMove_Before_CrossParent_ReparentsAndRecordsParents(test *testing.T) {
	test.Parallel()
	env := moveEnv(test)
	ctx := WithActor(context.Background(), "german")

	parentA := makeTask(test, env, "A", nil)
	parentB := makeTask(test, env, "B", nil)
	childA1 := makeTask(test, env, "A1", &parentA.ID) // under A, order 1
	childB1 := makeTask(test, env, "B1", &parentB.ID) // under B, order 1

	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   childA1.ID,
		Version:  childA1.Version,
		Position: MovePositionBefore,
		TargetID: &childB1.ID,
	})

	if err != nil {
		test.Fatalf("Move: %v", err)
	}

	if moved.ParentID == nil || *moved.ParentID != parentB.ID {
		test.Fatalf("parent: got %v, want parentB %v", moved.ParentID, parentB.ID)
	}

	if moved.Order == nil || *moved.Order != 0.0 {
		test.Fatalf("order: got %v, want 0.0", moved.Order)
	}

	event := lastMovedEvent(test, env)
	payload := event.Payload.(domain.TaskMovedPayload)

	if payload.OldParentID == nil || *payload.OldParentID != parentA.ID {
		test.Fatalf("old_parent_id: got %v, want parentA %v", payload.OldParentID, parentA.ID)
	}

	if payload.NewParentID == nil || *payload.NewParentID != parentB.ID {
		test.Fatalf("new_parent_id: got %v, want parentB %v", payload.NewParentID, parentB.ID)
	}
}

func TestMove_First_KeepCurrentParent(test *testing.T) {
	test.Parallel()
	env := moveEnv(test)
	ctx := context.Background()

	parent := makeTask(test, env, "P", nil)
	child1 := makeTask(test, env, "c1", &parent.ID) // order 1
	child2 := makeTask(test, env, "c2", &parent.ID) // order 2
	child3 := makeTask(test, env, "c3", &parent.ID) // order 3

	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   child3.ID,
		Version:  child3.Version,
		Position: MovePositionFirst,
	})

	if err != nil {
		test.Fatalf("Move: %v", err)
	}

	if moved.ParentID == nil || *moved.ParentID != parent.ID {
		test.Fatalf("parent: got %v, want p %v", moved.ParentID, parent.ID)
	}

	if moved.Order == nil || *moved.Order != 0.0 {
		test.Fatalf("order: got %v, want 0.0 (min(1,2)-1)", moved.Order)
	}

	_ = child1
	_ = child2
}

func TestMove_First_ToRoot_FromNested(test *testing.T) {
	test.Parallel()
	env := moveEnv(test)
	ctx := context.Background()

	rootA := makeTask(test, env, "rootA", nil) // order 1
	rootB := makeTask(test, env, "rootB", nil) // order 2
	nested := makeTask(test, env, "nested", &rootA.ID)

	nilParent := (*uuid.UUID)(nil)
	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   nested.ID,
		Version:  nested.Version,
		Position: MovePositionFirst,
		ParentID: &nilParent,
	})

	if err != nil {
		test.Fatalf("Move: %v", err)
	}

	if moved.ParentID != nil {
		test.Fatalf("parent: got %v, want nil (root)", moved.ParentID)
	}

	if moved.Order == nil || *moved.Order != 0.0 {
		test.Fatalf("order: got %v, want 0.0 (min(1,2)-1)", moved.Order)
	}

	_ = rootB
}

func TestMove_Last_ExplicitParent(test *testing.T) {
	test.Parallel()
	env := moveEnv(test)
	ctx := context.Background()

	parentA := makeTask(test, env, "A", nil)
	parentB := makeTask(test, env, "B", nil)
	// three children under B so Last is well-defined: order 1,2,3
	makeTask(test, env, "b1", &parentB.ID)
	makeTask(test, env, "b2", &parentB.ID)
	makeTask(test, env, "b3", &parentB.ID)
	// subject starts under A
	subject := makeTask(test, env, "sub", &parentA.ID)

	pbID := parentB.ID
	pbPtr := &pbID
	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   subject.ID,
		Version:  subject.Version,
		Position: MovePositionLast,
		ParentID: &pbPtr,
	})

	if err != nil {
		test.Fatalf("Move: %v", err)
	}

	if moved.ParentID == nil || *moved.ParentID != parentB.ID {
		test.Fatalf("parent: got %v, want pB %v", moved.ParentID, parentB.ID)
	}

	if moved.Order == nil || *moved.Order != 4.0 {
		test.Fatalf("order: got %v, want 4.0 (max(1,2,3)+1)", moved.Order)
	}
}

func TestMove_Cycle_Rejected(test *testing.T) {
	test.Parallel()
	env := moveEnv(test)
	ctx := context.Background()

	parent := makeTask(test, env, "p", nil)
	child := makeTask(test, env, "c", &parent.ID)

	cid := child.ID
	cPtr := &cid
	_, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   parent.ID,
		Version:  parent.Version,
		Position: MovePositionLast,
		ParentID: &cPtr,
	})

	if !errors.Is(err, domain.ErrCyclicParent) {
		test.Fatalf("err: got %v, want ErrCyclicParent", err)
	}

	// DB untouched: p still has no parent.
	got, lookupErr := env.taskSvc.GetByID(ctx, parent.ID)

	if lookupErr != nil {
		test.Fatalf("GetByID: %v", lookupErr)
	}

	if got.ParentID != nil {
		test.Fatalf("parent changed on rejected move: got %v, want nil", got.ParentID)
	}

	if got.Version != parent.Version {
		test.Fatalf("version changed on rejected move: got %d, want %d", got.Version, parent.Version)
	}
}

// TestMove_Underflow_WrapsErrOrderGapExhausted forces the midpoint to collapse
// by setting two sibling orders to adjacent float64 values and asking Move to
// slot a third task between them. Orders are rewritten through the bundle's
// repo directly so the test does not depend on the TaskUpdate.Order path.
func TestMove_Underflow_WrapsErrOrderGapExhausted(test *testing.T) {
	test.Parallel()
	env := moveEnv(test)
	ctx := context.Background()

	parent := makeTask(test, env, "P", nil)
	childA := makeTask(test, env, "A", &parent.ID)
	childB := makeTask(test, env, "B", &parent.ID)
	subject := makeTask(test, env, "S", &parent.ID)

	bundle, resolveErr := env.taskSvc.resolve(ctx, domain.DefaultProjectUUID)

	if resolveErr != nil {
		test.Fatalf("resolve bundle: %v", resolveErr)
	}

	hi := math.Nextafter(1.0, 2.0)
	lo := 1.0
	now := time.Now().UTC().Truncate(time.Millisecond)

	if _, seedAErr := bundle.Tasks.UpdateOrderAndParent(ctx, childA.ID, &parent.ID, lo, childA.Version, now); seedAErr != nil {
		test.Fatalf("seed A order: %v", seedAErr)
	}

	if _, seedBErr := bundle.Tasks.UpdateOrderAndParent(ctx, childB.ID, &parent.ID, hi, childB.Version, now); seedBErr != nil {
		test.Fatalf("seed B order: %v", seedBErr)
	}

	subject, reloadErr := env.taskSvc.GetByID(ctx, subject.ID)

	if reloadErr != nil {
		test.Fatalf("re-read subject: %v", reloadErr)
	}

	_, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   subject.ID,
		Version:  subject.Version,
		Position: MovePositionBefore,
		TargetID: &childB.ID,
	})

	if !errors.Is(err, domain.ErrOrderGapExhausted) {
		test.Fatalf("err: got %v, want wrapping ErrOrderGapExhausted", err)
	}

	short := strings.ReplaceAll(parent.ID.String(), "-", "")[:8]

	if !strings.Contains(err.Error(), short) {
		test.Fatalf("err message missing parent short id %q: %v", short, err)
	}
}

func TestMove_VersionMismatch_ReturnsConflict(test *testing.T) {
	test.Parallel()
	env := moveEnv(test)
	ctx := context.Background()

	taskA := makeTask(test, env, "a", nil)
	taskB := makeTask(test, env, "b", nil)

	_, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   taskA.ID,
		Version:  taskA.Version + 5, // stale
		Position: MovePositionAfter,
		TargetID: &taskB.ID,
	})

	if !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("err: got %v, want ErrConflict", err)
	}
}

func TestMove_SubjectNotFound_ReturnsNotFound(test *testing.T) {
	test.Parallel()
	env := moveEnv(test)
	ctx := context.Background()

	_, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   uuid.New(),
		Version:  1,
		Position: MovePositionFirst,
	})

	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("err: got %v, want ErrNotFound", err)
	}
}

func TestMove_TargetNotFound_ReturnsNotFound(test *testing.T) {
	test.Parallel()
	env := moveEnv(test)
	ctx := context.Background()

	taskA := makeTask(test, env, "a", nil)
	missing := uuid.New()

	_, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   taskA.ID,
		Version:  taskA.Version,
		Position: MovePositionBefore,
		TargetID: &missing,
	})

	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("err: got %v, want ErrNotFound", err)
	}
}
