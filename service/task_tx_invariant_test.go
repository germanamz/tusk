package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
)

var errInjectedEventFailure = errors.New("inject: event record failed")

// failingEvents wraps an EventRepository so Record always returns a sentinel
// error. Read methods delegate to the real repo so callers that inspect counts
// or list events still work.
type failingEvents struct {
	inner repository.EventRepository
}

func (f *failingEvents) Record(ctx context.Context, evt *domain.Event) error {
	return errInjectedEventFailure
}

func (f *failingEvents) List(ctx context.Context, ff repository.EventFilter) ([]*domain.Event, error) {
	return f.inner.List(ctx, ff)
}

func (f *failingEvents) Count(ctx context.Context) (int64, error) {
	return f.inner.Count(ctx)
}

func (f *failingEvents) PruneToSize(ctx context.Context, maxRows int) (int64, error) {
	return f.inner.PruneToSize(ctx, maxRows)
}

type failingWriteTx struct {
	inner  WriteTx
	events repository.EventRepository
}

func (w *failingWriteTx) Tasks() repository.TaskRepository         { return w.inner.Tasks() }
func (w *failingWriteTx) Relations() repository.RelationRepository { return w.inner.Relations() }
func (w *failingWriteTx) Events() repository.EventRepository       { return w.events }

type failingProvider struct {
	real WriteTxProvider
}

func (p *failingProvider) WithTx(ctx context.Context, fn func(tx WriteTx) error) error {
	return p.real.WithTx(ctx, func(inner WriteTx) error {
		wrapped := &failingWriteTx{
			inner:  inner,
			events: &failingEvents{inner: inner.Events()},
		}
		return fn(wrapped)
	})
}

func TestTxInvariant_MutationsRollBackOnEventFailure(t *testing.T) {
	t.Run("Create", func(t *testing.T) {
		env := testTaskEnv(t)
		env.installFailingEvents(t)

		task := newMinimalTask("create fail")
		err := env.taskSvc.Create(context.Background(), task)
		if !errors.Is(err, errInjectedEventFailure) {
			t.Fatalf("Create: got %v, want wrapped injected failure", err)
		}

		// Task.ID is assigned before Create opens the tx, so the service layer
		// still has the value. Look up by ShortID via the repo directly.
		_, err = getRepoTaskByShortID(t, env, task.ShortID)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected task row to be absent after rollback, got err=%v", err)
		}
	})

	t.Run("Update_NonStatus", func(t *testing.T) {
		env := testTaskEnv(t)
		task := newMinimalTask("update nonstatus")
		mustCreateTask(t, env.taskSvc, task)
		env.installFailingEvents(t)

		newTitle := "mutated"
		_, err := env.taskSvc.Update(context.Background(), domain.TaskUpdate{
			ShortID: task.ShortID,
			Version: task.Version,
			Title:   &newTitle,
		})
		if !errors.Is(err, errInjectedEventFailure) {
			t.Fatalf("Update: got %v, want wrapped injected failure", err)
		}
		assertTaskUnchanged(t, env, task.ShortID, task)
	})

	t.Run("Update_Status", func(t *testing.T) {
		env := testTaskEnv(t)
		task := newMinimalTask("update status")
		mustCreateTask(t, env.taskSvc, task)
		env.installFailingEvents(t)

		newStatus := "active"
		_, err := env.taskSvc.Update(context.Background(), domain.TaskUpdate{
			ShortID: task.ShortID,
			Version: task.Version,
			Status:  &newStatus,
		})
		if !errors.Is(err, errInjectedEventFailure) {
			t.Fatalf("Update: got %v, want wrapped injected failure", err)
		}
		assertTaskUnchanged(t, env, task.ShortID, task)
	})

	t.Run("Claim", func(t *testing.T) {
		env := testTaskEnv(t)
		registerTestPlayer(t, env, "agent-1")
		task := newMinimalTask("claim fail")
		mustCreateTask(t, env.taskSvc, task)
		env.installFailingEvents(t)

		_, err := env.taskSvc.Claim(context.Background(), task.ShortID, "agent-1", task.Version)
		if !errors.Is(err, errInjectedEventFailure) {
			t.Fatalf("Claim: got %v, want wrapped injected failure", err)
		}
		assertTaskUnchanged(t, env, task.ShortID, task)
	})

	t.Run("Release", func(t *testing.T) {
		env := testTaskEnv(t)
		registerTestPlayer(t, env, "agent-1")
		task := newMinimalTask("release fail")
		mustCreateTask(t, env.taskSvc, task)
		claimed, err := env.taskSvc.Claim(context.Background(), task.ShortID, "agent-1", task.Version)
		if err != nil {
			t.Fatalf("Claim setup: %v", err)
		}
		env.installFailingEvents(t)

		_, err = env.taskSvc.Release(context.Background(), claimed.ShortID, "agent-1", claimed.Version)
		if !errors.Is(err, errInjectedEventFailure) {
			t.Fatalf("Release: got %v, want wrapped injected failure", err)
		}
		assertTaskUnchanged(t, env, claimed.ShortID, claimed)
	})

	t.Run("Complete", func(t *testing.T) {
		env := testTaskEnv(t)
		task := newMinimalTask("complete fail")
		mustCreateTask(t, env.taskSvc, task)
		started, err := env.taskSvc.Start(context.Background(), task.ShortID, task.Version, "")
		if err != nil {
			t.Fatalf("Start setup: %v", err)
		}
		env.installFailingEvents(t)

		_, err = env.taskSvc.Complete(context.Background(), started.ShortID, started.Version)
		if !errors.Is(err, errInjectedEventFailure) {
			t.Fatalf("Complete: got %v, want wrapped injected failure", err)
		}
		assertTaskUnchanged(t, env, started.ShortID, started)
	})

	t.Run("Delete", func(t *testing.T) {
		env := testTaskEnv(t)
		task := newMinimalTask("delete fail")
		mustCreateTask(t, env.taskSvc, task)
		env.installFailingEvents(t)

		_, err := env.taskSvc.Delete(context.Background(), task.ShortID, task.Version)
		if !errors.Is(err, errInjectedEventFailure) {
			t.Fatalf("Delete: got %v, want wrapped injected failure", err)
		}
		assertTaskUnchanged(t, env, task.ShortID, task)
	})

	t.Run("Start", func(t *testing.T) {
		env := testTaskEnv(t)
		registerTestPlayer(t, env, "agent-1")
		task := newMinimalTask("start fail")
		mustCreateTask(t, env.taskSvc, task)
		env.installFailingEvents(t)

		_, err := env.taskSvc.Start(context.Background(), task.ShortID, task.Version, "agent-1")
		if !errors.Is(err, errInjectedEventFailure) {
			t.Fatalf("Start: got %v, want wrapped injected failure", err)
		}
		assertTaskUnchanged(t, env, task.ShortID, task)
	})

	t.Run("Pop", func(t *testing.T) {
		env := testTaskEnv(t)
		registerTestPlayer(t, env, "agent-1")
		task := newMinimalTask("pop fail")
		mustCreateTask(t, env.taskSvc, task)
		env.installFailingEvents(t)

		_, err := env.taskSvc.Pop(context.Background(), "agent-1", nil)
		if !errors.Is(err, errInjectedEventFailure) {
			t.Fatalf("Pop: got %v, want wrapped injected failure", err)
		}
		assertTaskUnchanged(t, env, task.ShortID, task)
	})

	t.Run("RelationAdd", func(t *testing.T) {
		env := testTaskEnv(t)
		relSvc := NewRelationService(env.taskSvc.resolve, env.taskSvc.projects)
		taskA := newMinimalTask("rel add A")
		mustCreateTask(t, env.taskSvc, taskA)
		taskB := newMinimalTask("rel add B")
		mustCreateTask(t, env.taskSvc, taskB)
		env.installFailingEvents(t)

		_, err := relSvc.Add(context.Background(), taskA.ShortID, taskB.ShortID, "blocks")
		if !errors.Is(err, errInjectedEventFailure) {
			t.Fatalf("RelationAdd: got %v, want wrapped injected failure", err)
		}

		// The relation row must not exist after rollback.
		bundle, err := env.taskSvc.resolve(context.Background(), domain.DefaultProjectUUID)
		if err != nil {
			t.Fatalf("resolve bundle: %v", err)
		}
		_, err = bundle.Relations.GetByFields(context.Background(), taskA.ID, taskB.ID, "blocks")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected relation row to be absent after rollback, got err=%v", err)
		}
	})

	t.Run("RelationRemove", func(t *testing.T) {
		env := testTaskEnv(t)
		relSvc := NewRelationService(env.taskSvc.resolve, env.taskSvc.projects)
		taskA := newMinimalTask("rel rm A")
		mustCreateTask(t, env.taskSvc, taskA)
		taskB := newMinimalTask("rel rm B")
		mustCreateTask(t, env.taskSvc, taskB)
		if _, err := relSvc.Add(context.Background(), taskA.ShortID, taskB.ShortID, "blocks"); err != nil {
			t.Fatalf("Add setup: %v", err)
		}
		env.installFailingEvents(t)

		err := relSvc.Remove(context.Background(), taskA.ShortID, taskB.ShortID, "blocks")
		if !errors.Is(err, errInjectedEventFailure) {
			t.Fatalf("RelationRemove: got %v, want wrapped injected failure", err)
		}

		// The relation row must still exist after rollback.
		bundle, err := env.taskSvc.resolve(context.Background(), domain.DefaultProjectUUID)
		if err != nil {
			t.Fatalf("resolve bundle: %v", err)
		}
		if _, err := bundle.Relations.GetByFields(context.Background(), taskA.ID, taskB.ID, "blocks"); err != nil {
			t.Fatalf("expected relation row to be present after rollback, got err=%v", err)
		}
	})
}

// installFailingEvents swaps the env's resolver bundle's WriteTxProvider for a
// failingProvider wrapping the real one. Must be called after any setup
// mutations (Create, Claim, etc.) that should succeed.
func (e *testEnv) installFailingEvents(t *testing.T) {
	t.Helper()
	real := e.getBundleWriteTx(t)
	e.setBundleWriteTx(t, &failingProvider{real: real})
}

// getBundleWriteTx/setBundleWriteTx reach into the bundle via the resolver.
// testEnv hides the bundle, so use the resolver result directly.
func (e *testEnv) getBundleWriteTx(t *testing.T) WriteTxProvider {
	t.Helper()
	bundle, err := e.taskSvc.resolve(context.Background(), domain.DefaultProjectUUID)
	if err != nil {
		t.Fatalf("resolve default bundle: %v", err)
	}
	return bundle.WriteTx
}

func (e *testEnv) setBundleWriteTx(t *testing.T, provider WriteTxProvider) {
	t.Helper()
	bundle, err := e.taskSvc.resolve(context.Background(), domain.DefaultProjectUUID)
	if err != nil {
		t.Fatalf("resolve default bundle: %v", err)
	}
	bundle.WriteTx = provider
}

// getRepoTaskByShortID looks up a task via the bundle's repo (not the service).
// Used to check for absence after a rolled-back Create.
func getRepoTaskByShortID(t *testing.T, env *testEnv, shortID string) (*domain.Task, error) {
	t.Helper()
	bundle, err := env.taskSvc.resolve(context.Background(), domain.DefaultProjectUUID)
	if err != nil {
		t.Fatalf("resolve default bundle: %v", err)
	}
	return bundle.Tasks.GetByShortID(context.Background(), shortID)
}

// assertTaskUnchanged reads the current row and checks that version, status,
// claimed_by, claimed_at, and title match the expected pre-call snapshot.
func assertTaskUnchanged(t *testing.T, env *testEnv, shortID string, want *domain.Task) {
	t.Helper()
	got, err := getRepoTaskByShortID(t, env, shortID)
	if err != nil {
		t.Fatalf("reloading task %q after failed mutation: %v", shortID, err)
	}
	if got.Version != want.Version {
		t.Fatalf("version: got %d, want %d (mutation not rolled back)", got.Version, want.Version)
	}
	if got.Status != want.Status {
		t.Fatalf("status: got %q, want %q", got.Status, want.Status)
	}
	if got.Title != want.Title {
		t.Fatalf("title: got %q, want %q", got.Title, want.Title)
	}
	if !stringPtrEqual(got.ClaimedBy, want.ClaimedBy) {
		t.Fatalf("claimed_by: got %v, want %v", got.ClaimedBy, want.ClaimedBy)
	}
	if !timePtrEqual(got.ClaimedAt, want.ClaimedAt) {
		t.Fatalf("claimed_at: got %v, want %v", got.ClaimedAt, want.ClaimedAt)
	}
}
