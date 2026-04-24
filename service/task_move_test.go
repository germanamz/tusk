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
func moveEnv(t *testing.T) *testEnv {
	t.Helper()
	return testTaskEnv(t)
}

// makeTask creates a task through the service and returns the persisted row.
func makeTask(t *testing.T, env *testEnv, title string, parent *uuid.UUID) *domain.Task {
	t.Helper()
	task := &domain.Task{Title: title}
	if parent != nil {
		task.ParentID = parent
	}
	if err := env.taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("Create %q: %v", title, err)
	}
	return task
}

func lastMovedEvent(t *testing.T, env *testEnv) *domain.Event {
	t.Helper()
	events := listAllEvents(t, env.store)
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == domain.EventTaskMoved {
			return events[i]
		}
	}
	t.Fatalf("no task_moved event found among %d events", len(events))
	return nil
}

func TestMove_Before_SameParent_UsesMidpoint(t *testing.T) {
	t.Parallel()
	env := moveEnv(t)
	ctx := WithActor(context.Background(), "german")

	a := makeTask(t, env, "a", nil) // order 1
	b := makeTask(t, env, "b", nil) // order 2
	c := makeTask(t, env, "c", nil) // order 3

	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   c.ID,
		Version:  c.Version,
		Position: MovePositionBefore,
		TargetID: &b.ID,
	})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.Order == nil || *moved.Order != 1.5 {
		t.Fatalf("order: got %v, want 1.5", moved.Order)
	}

	evt := lastMovedEvent(t, env)
	payload := evt.Payload.(domain.TaskMovedPayload)
	if payload.OldOrder == nil || *payload.OldOrder != 3.0 {
		t.Fatalf("old_order: got %v, want 3.0", payload.OldOrder)
	}
	if payload.NewOrder == nil || *payload.NewOrder != 1.5 {
		t.Fatalf("new_order: got %v, want 1.5", payload.NewOrder)
	}
	if payload.OldParentID != nil || payload.NewParentID != nil {
		t.Fatalf("parents: got old=%v new=%v, want nil/nil", payload.OldParentID, payload.NewParentID)
	}
	_ = a
}

func TestMove_After_SameParent_UsesMidpoint(t *testing.T) {
	t.Parallel()
	env := moveEnv(t)
	ctx := context.Background()

	a := makeTask(t, env, "a", nil) // order 1
	b := makeTask(t, env, "b", nil) // order 2
	c := makeTask(t, env, "c", nil) // order 3

	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   a.ID,
		Version:  a.Version,
		Position: MovePositionAfter,
		TargetID: &b.ID,
	})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.Order == nil || *moved.Order != 2.5 {
		t.Fatalf("order: got %v, want 2.5", moved.Order)
	}
	_ = c
}

func TestMove_Before_CrossParent_ReparentsAndRecordsParents(t *testing.T) {
	t.Parallel()
	env := moveEnv(t)
	ctx := WithActor(context.Background(), "german")

	parentA := makeTask(t, env, "A", nil)
	parentB := makeTask(t, env, "B", nil)
	childA1 := makeTask(t, env, "A1", &parentA.ID) // under A, order 1
	childB1 := makeTask(t, env, "B1", &parentB.ID) // under B, order 1

	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   childA1.ID,
		Version:  childA1.Version,
		Position: MovePositionBefore,
		TargetID: &childB1.ID,
	})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != parentB.ID {
		t.Fatalf("parent: got %v, want parentB %v", moved.ParentID, parentB.ID)
	}
	if moved.Order == nil || *moved.Order != 0.0 {
		t.Fatalf("order: got %v, want 0.0", moved.Order)
	}

	evt := lastMovedEvent(t, env)
	p := evt.Payload.(domain.TaskMovedPayload)
	if p.OldParentID == nil || *p.OldParentID != parentA.ID {
		t.Fatalf("old_parent_id: got %v, want parentA %v", p.OldParentID, parentA.ID)
	}
	if p.NewParentID == nil || *p.NewParentID != parentB.ID {
		t.Fatalf("new_parent_id: got %v, want parentB %v", p.NewParentID, parentB.ID)
	}
}

func TestMove_First_KeepCurrentParent(t *testing.T) {
	t.Parallel()
	env := moveEnv(t)
	ctx := context.Background()

	p := makeTask(t, env, "P", nil)
	c1 := makeTask(t, env, "c1", &p.ID) // order 1
	c2 := makeTask(t, env, "c2", &p.ID) // order 2
	c3 := makeTask(t, env, "c3", &p.ID) // order 3

	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   c3.ID,
		Version:  c3.Version,
		Position: MovePositionFirst,
	})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != p.ID {
		t.Fatalf("parent: got %v, want p %v", moved.ParentID, p.ID)
	}
	if moved.Order == nil || *moved.Order != 0.0 {
		t.Fatalf("order: got %v, want 0.0 (min(1,2)-1)", moved.Order)
	}
	_ = c1
	_ = c2
}

func TestMove_First_ToRoot_FromNested(t *testing.T) {
	t.Parallel()
	env := moveEnv(t)
	ctx := context.Background()

	rootA := makeTask(t, env, "rootA", nil) // order 1
	rootB := makeTask(t, env, "rootB", nil) // order 2
	nested := makeTask(t, env, "nested", &rootA.ID)

	nilParent := (*uuid.UUID)(nil)
	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   nested.ID,
		Version:  nested.Version,
		Position: MovePositionFirst,
		ParentID: &nilParent,
	})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.ParentID != nil {
		t.Fatalf("parent: got %v, want nil (root)", moved.ParentID)
	}
	if moved.Order == nil || *moved.Order != 0.0 {
		t.Fatalf("order: got %v, want 0.0 (min(1,2)-1)", moved.Order)
	}
	_ = rootB
}

func TestMove_Last_ExplicitParent(t *testing.T) {
	t.Parallel()
	env := moveEnv(t)
	ctx := context.Background()

	pA := makeTask(t, env, "A", nil)
	pB := makeTask(t, env, "B", nil)
	// three children under B so Last is well-defined: order 1,2,3
	makeTask(t, env, "b1", &pB.ID)
	makeTask(t, env, "b2", &pB.ID)
	makeTask(t, env, "b3", &pB.ID)
	// subject starts under A
	subject := makeTask(t, env, "sub", &pA.ID)

	pbID := pB.ID
	pbPtr := &pbID
	moved, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   subject.ID,
		Version:  subject.Version,
		Position: MovePositionLast,
		ParentID: &pbPtr,
	})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != pB.ID {
		t.Fatalf("parent: got %v, want pB %v", moved.ParentID, pB.ID)
	}
	if moved.Order == nil || *moved.Order != 4.0 {
		t.Fatalf("order: got %v, want 4.0 (max(1,2,3)+1)", moved.Order)
	}
}

func TestMove_Cycle_Rejected(t *testing.T) {
	t.Parallel()
	env := moveEnv(t)
	ctx := context.Background()

	p := makeTask(t, env, "p", nil)
	c := makeTask(t, env, "c", &p.ID)

	cid := c.ID
	cPtr := &cid
	_, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   p.ID,
		Version:  p.Version,
		Position: MovePositionLast,
		ParentID: &cPtr,
	})
	if !errors.Is(err, domain.ErrCyclicParent) {
		t.Fatalf("err: got %v, want ErrCyclicParent", err)
	}

	// DB untouched: p still has no parent.
	got, err := env.taskSvc.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ParentID != nil {
		t.Fatalf("parent changed on rejected move: got %v, want nil", got.ParentID)
	}
	if got.Version != p.Version {
		t.Fatalf("version changed on rejected move: got %d, want %d", got.Version, p.Version)
	}
}

// TestMove_Underflow_WrapsErrOrderGapExhausted forces the midpoint to collapse
// by setting two sibling orders to adjacent float64 values and asking Move to
// slot a third task between them. Orders are rewritten through the bundle's
// repo directly so the test does not depend on the TaskUpdate.Order path.
func TestMove_Underflow_WrapsErrOrderGapExhausted(t *testing.T) {
	t.Parallel()
	env := moveEnv(t)
	ctx := context.Background()

	parent := makeTask(t, env, "P", nil)
	childA := makeTask(t, env, "A", &parent.ID)
	childB := makeTask(t, env, "B", &parent.ID)
	subject := makeTask(t, env, "S", &parent.ID)

	bundle, err := env.taskSvc.resolve(ctx, domain.DefaultProjectUUID)
	if err != nil {
		t.Fatalf("resolve bundle: %v", err)
	}
	hi := math.Nextafter(1.0, 2.0)
	lo := 1.0
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := bundle.Tasks.UpdateOrderAndParent(ctx, childA.ID, &parent.ID, lo, childA.Version, now); err != nil {
		t.Fatalf("seed A order: %v", err)
	}
	if _, err := bundle.Tasks.UpdateOrderAndParent(ctx, childB.ID, &parent.ID, hi, childB.Version, now); err != nil {
		t.Fatalf("seed B order: %v", err)
	}

	subject, err = env.taskSvc.GetByID(ctx, subject.ID)
	if err != nil {
		t.Fatalf("re-read subject: %v", err)
	}

	_, err = env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   subject.ID,
		Version:  subject.Version,
		Position: MovePositionBefore,
		TargetID: &childB.ID,
	})
	if !errors.Is(err, domain.ErrOrderGapExhausted) {
		t.Fatalf("err: got %v, want wrapping ErrOrderGapExhausted", err)
	}
	short := strings.ReplaceAll(parent.ID.String(), "-", "")[:8]
	if !strings.Contains(err.Error(), short) {
		t.Fatalf("err message missing parent short id %q: %v", short, err)
	}
}

func TestMove_VersionMismatch_ReturnsConflict(t *testing.T) {
	t.Parallel()
	env := moveEnv(t)
	ctx := context.Background()

	a := makeTask(t, env, "a", nil)
	b := makeTask(t, env, "b", nil)

	_, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   a.ID,
		Version:  a.Version + 5, // stale
		Position: MovePositionAfter,
		TargetID: &b.ID,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err: got %v, want ErrConflict", err)
	}
}

func TestMove_SubjectNotFound_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	env := moveEnv(t)
	ctx := context.Background()

	_, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   uuid.New(),
		Version:  1,
		Position: MovePositionFirst,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err: got %v, want ErrNotFound", err)
	}
}

func TestMove_TargetNotFound_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	env := moveEnv(t)
	ctx := context.Background()

	a := makeTask(t, env, "a", nil)
	missing := uuid.New()
	_, err := env.taskSvc.Move(ctx, MoveRequest{
		TaskID:   a.ID,
		Version:  a.Version,
		Position: MovePositionBefore,
		TargetID: &missing,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err: got %v, want ErrNotFound", err)
	}
}
