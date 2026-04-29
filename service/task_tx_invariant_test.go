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

func (events *failingEvents) Record(_ context.Context, _ *domain.Event) error {
	return errInjectedEventFailure
}

func (events *failingEvents) List(ctx context.Context, filter repository.EventFilter) ([]*domain.Event, error) {
	return events.inner.List(ctx, filter)
}

func (events *failingEvents) Count(ctx context.Context) (int64, error) {
	return events.inner.Count(ctx)
}

func (events *failingEvents) PruneToSize(ctx context.Context, maxRows int) (int64, error) {
	return events.inner.PruneToSize(ctx, maxRows)
}

type failingWriteTx struct {
	inner  WriteTx
	events repository.EventRepository
}

func (w *failingWriteTx) Tasks() repository.TaskRepository         { return w.inner.Tasks() }
func (w *failingWriteTx) Relations() repository.RelationRepository { return w.inner.Relations() }
func (w *failingWriteTx) Events() repository.EventRepository       { return w.events }

func (w *failingWriteTx) Projects() repository.ProjectRepository       { return w.inner.Projects() }
func (w *failingWriteTx) Workflows() repository.WorkflowRepository     { return w.inner.Workflows() }
func (w *failingWriteTx) Players() repository.PlayerRepository         { return w.inner.Players() }
func (w *failingWriteTx) Tags() repository.TagRepository               { return w.inner.Tags() }
func (w *failingWriteTx) Annotations() repository.AnnotationRepository { return w.inner.Annotations() }
func (w *failingWriteTx) Notes() repository.NoteRepository             { return w.inner.Notes() }

func (w *failingWriteTx) TruncateAll(ctx context.Context) error { return w.inner.TruncateAll(ctx) }

type failingProvider struct {
	real WriteTxProvider
}

func (provider *failingProvider) WithTx(ctx context.Context, fn func(tx WriteTx) error) error {
	return provider.real.WithTx(ctx, func(inner WriteTx) error {
		wrapped := &failingWriteTx{
			inner:  inner,
			events: &failingEvents{inner: inner.Events()},
		}
		return fn(wrapped)
	})
}

func TestTxInvariant_MutationsRollBackOnEventFailure(test *testing.T) {
	test.Run("Create", func(test *testing.T) {
		env := testTaskEnv(test)
		env.installFailingEvents(test)

		task := newMinimalTask("create fail")
		err := env.taskSvc.Create(context.Background(), task)

		if !errors.Is(err, errInjectedEventFailure) {
			test.Fatalf("Create: got %v, want wrapped injected failure", err)
		}

		// Task.ID is assigned before Create opens the tx, so the service layer
		// still has the value. Look up by ShortID via the repo directly.
		_, err = getRepoTaskByShortID(test, env, task.ShortID)

		if !errors.Is(err, domain.ErrNotFound) {
			test.Fatalf("expected task row to be absent after rollback, got err=%v", err)
		}
	})

	test.Run("Update_NonStatus", func(test *testing.T) {
		env := testTaskEnv(test)
		task := newMinimalTask("update nonstatus")
		mustCreateTask(test, env.taskSvc, task)
		env.installFailingEvents(test)

		newTitle := "mutated"
		_, err := env.taskSvc.Update(context.Background(), domain.TaskUpdate{
			ShortID: task.ShortID,
			Version: task.Version,
			Title:   &newTitle,
		})

		if !errors.Is(err, errInjectedEventFailure) {
			test.Fatalf("Update: got %v, want wrapped injected failure", err)
		}

		assertTaskUnchanged(test, env, task.ShortID, task)
	})

	test.Run("Update_Status", func(test *testing.T) {
		env := testTaskEnv(test)
		task := newMinimalTask("update status")
		mustCreateTask(test, env.taskSvc, task)
		env.installFailingEvents(test)

		newStatus := "active"
		_, err := env.taskSvc.Update(context.Background(), domain.TaskUpdate{
			ShortID: task.ShortID,
			Version: task.Version,
			Status:  &newStatus,
		})

		if !errors.Is(err, errInjectedEventFailure) {
			test.Fatalf("Update: got %v, want wrapped injected failure", err)
		}

		assertTaskUnchanged(test, env, task.ShortID, task)
	})

	test.Run("Claim", func(test *testing.T) {
		env := testTaskEnv(test)
		registerTestPlayer(test, env, "agent-1")
		task := newMinimalTask("claim fail")
		mustCreateTask(test, env.taskSvc, task)
		env.installFailingEvents(test)

		_, err := env.taskSvc.Claim(context.Background(), task.ShortID, "agent-1", task.Version)

		if !errors.Is(err, errInjectedEventFailure) {
			test.Fatalf("Claim: got %v, want wrapped injected failure", err)
		}

		assertTaskUnchanged(test, env, task.ShortID, task)
	})

	test.Run("Release", func(test *testing.T) {
		env := testTaskEnv(test)
		registerTestPlayer(test, env, "agent-1")
		task := newMinimalTask("release fail")
		mustCreateTask(test, env.taskSvc, task)

		claimed, claimErr := env.taskSvc.Claim(context.Background(), task.ShortID, "agent-1", task.Version)

		if claimErr != nil {
			test.Fatalf("Claim setup: %v", claimErr)
		}

		env.installFailingEvents(test)

		_, releaseErr := env.taskSvc.Release(context.Background(), claimed.ShortID, "agent-1", claimed.Version)

		if !errors.Is(releaseErr, errInjectedEventFailure) {
			test.Fatalf("Release: got %v, want wrapped injected failure", releaseErr)
		}

		assertTaskUnchanged(test, env, claimed.ShortID, claimed)
	})

	test.Run("Complete", func(test *testing.T) {
		env := testTaskEnv(test)
		task := newMinimalTask("complete fail")
		mustCreateTask(test, env.taskSvc, task)

		started, startErr := env.taskSvc.Start(context.Background(), task.ShortID, task.Version, "")

		if startErr != nil {
			test.Fatalf("Start setup: %v", startErr)
		}

		env.installFailingEvents(test)

		_, completeErr := env.taskSvc.Complete(context.Background(), started.ShortID, started.Version)

		if !errors.Is(completeErr, errInjectedEventFailure) {
			test.Fatalf("Complete: got %v, want wrapped injected failure", completeErr)
		}

		assertTaskUnchanged(test, env, started.ShortID, started)
	})

	test.Run("Delete", func(test *testing.T) {
		env := testTaskEnv(test)
		task := newMinimalTask("delete fail")
		mustCreateTask(test, env.taskSvc, task)
		env.installFailingEvents(test)

		_, err := env.taskSvc.Delete(context.Background(), task.ShortID, task.Version)

		if !errors.Is(err, errInjectedEventFailure) {
			test.Fatalf("Delete: got %v, want wrapped injected failure", err)
		}

		assertTaskUnchanged(test, env, task.ShortID, task)
	})

	test.Run("Start", func(test *testing.T) {
		env := testTaskEnv(test)
		registerTestPlayer(test, env, "agent-1")
		task := newMinimalTask("start fail")
		mustCreateTask(test, env.taskSvc, task)
		env.installFailingEvents(test)

		_, err := env.taskSvc.Start(context.Background(), task.ShortID, task.Version, "agent-1")

		if !errors.Is(err, errInjectedEventFailure) {
			test.Fatalf("Start: got %v, want wrapped injected failure", err)
		}

		assertTaskUnchanged(test, env, task.ShortID, task)
	})

	test.Run("Pop", func(test *testing.T) {
		env := testTaskEnv(test)
		registerTestPlayer(test, env, "agent-1")
		task := newMinimalTask("pop fail")
		mustCreateTask(test, env.taskSvc, task)
		env.installFailingEvents(test)

		_, err := env.taskSvc.Pop(context.Background(), "agent-1", nil)

		if !errors.Is(err, errInjectedEventFailure) {
			test.Fatalf("Pop: got %v, want wrapped injected failure", err)
		}

		assertTaskUnchanged(test, env, task.ShortID, task)
	})

	test.Run("RelationAdd", func(test *testing.T) {
		env := testTaskEnv(test)
		relSvc := NewRelationService(env.taskSvc.resolve, env.taskSvc.projects)
		taskA := newMinimalTask("rel add A")
		mustCreateTask(test, env.taskSvc, taskA)
		taskB := newMinimalTask("rel add B")
		mustCreateTask(test, env.taskSvc, taskB)
		env.installFailingEvents(test)

		_, err := relSvc.Add(context.Background(), taskA.ShortID, taskB.ShortID, "blocks")

		if !errors.Is(err, errInjectedEventFailure) {
			test.Fatalf("RelationAdd: got %v, want wrapped injected failure", err)
		}

		// The relation row must not exist after rollback.
		bundle, resolveErr := env.taskSvc.resolve(context.Background(), domain.DefaultProjectUUID)

		if resolveErr != nil {
			test.Fatalf("resolve bundle: %v", resolveErr)
		}

		_, lookupErr := bundle.Relations.GetByFields(context.Background(), taskA.ID, taskB.ID, "blocks")

		if !errors.Is(lookupErr, domain.ErrNotFound) {
			test.Fatalf("expected relation row to be absent after rollback, got err=%v", lookupErr)
		}
	})

	test.Run("RelationRemove", func(test *testing.T) {
		env := testTaskEnv(test)
		relSvc := NewRelationService(env.taskSvc.resolve, env.taskSvc.projects)
		taskA := newMinimalTask("rel rm A")
		mustCreateTask(test, env.taskSvc, taskA)
		taskB := newMinimalTask("rel rm B")
		mustCreateTask(test, env.taskSvc, taskB)

		if _, addErr := relSvc.Add(context.Background(), taskA.ShortID, taskB.ShortID, "blocks"); addErr != nil {
			test.Fatalf("Add setup: %v", addErr)
		}

		env.installFailingEvents(test)

		removeErr := relSvc.Remove(context.Background(), taskA.ShortID, taskB.ShortID, "blocks")

		if !errors.Is(removeErr, errInjectedEventFailure) {
			test.Fatalf("RelationRemove: got %v, want wrapped injected failure", removeErr)
		}

		// The relation row must still exist after rollback.
		bundle, resolveErr := env.taskSvc.resolve(context.Background(), domain.DefaultProjectUUID)

		if resolveErr != nil {
			test.Fatalf("resolve bundle: %v", resolveErr)
		}

		if _, lookupErr := bundle.Relations.GetByFields(context.Background(), taskA.ID, taskB.ID, "blocks"); lookupErr != nil {
			test.Fatalf("expected relation row to be present after rollback, got err=%v", lookupErr)
		}
	})
}

// installFailingEvents swaps the env's resolver bundle's WriteTxProvider for a
// failingProvider wrapping the real one. Must be called after any setup
// mutations (Create, Claim, etc.) that should succeed.
func (env *testEnv) installFailingEvents(test *testing.T) {
	test.Helper()
	real := env.getBundleWriteTx(test)
	env.setBundleWriteTx(test, &failingProvider{real: real})
}

// getBundleWriteTx/setBundleWriteTx reach into the bundle via the resolver.
// testEnv hides the bundle, so use the resolver result directly.
func (env *testEnv) getBundleWriteTx(test *testing.T) WriteTxProvider {
	test.Helper()
	bundle, err := env.taskSvc.resolve(context.Background(), domain.DefaultProjectUUID)

	if err != nil {
		test.Fatalf("resolve default bundle: %v", err)
	}

	return bundle.WriteTx
}

func (env *testEnv) setBundleWriteTx(test *testing.T, provider WriteTxProvider) {
	test.Helper()
	bundle, err := env.taskSvc.resolve(context.Background(), domain.DefaultProjectUUID)

	if err != nil {
		test.Fatalf("resolve default bundle: %v", err)
	}

	bundle.WriteTx = provider
}

// getRepoTaskByShortID looks up a task via the bundle's repo (not the service).
// Used to check for absence after a rolled-back Create.
func getRepoTaskByShortID(test *testing.T, env *testEnv, shortID string) (*domain.Task, error) {
	test.Helper()
	bundle, err := env.taskSvc.resolve(context.Background(), domain.DefaultProjectUUID)

	if err != nil {
		test.Fatalf("resolve default bundle: %v", err)
	}

	return bundle.Tasks.GetByShortID(context.Background(), shortID)
}

// assertTaskUnchanged reads the current row and checks that version, status,
// claimed_by, claimed_at, and title match the expected pre-call snapshot.
func assertTaskUnchanged(test *testing.T, env *testEnv, shortID string, want *domain.Task) {
	test.Helper()
	got, err := getRepoTaskByShortID(test, env, shortID)

	if err != nil {
		test.Fatalf("reloading task %q after failed mutation: %v", shortID, err)
	}

	if got.Version != want.Version {
		test.Fatalf("version: got %d, want %d (mutation not rolled back)", got.Version, want.Version)
	}

	if got.Status != want.Status {
		test.Fatalf("status: got %q, want %q", got.Status, want.Status)
	}

	if got.Title != want.Title {
		test.Fatalf("title: got %q, want %q", got.Title, want.Title)
	}

	if !stringPtrEqual(got.ClaimedBy, want.ClaimedBy) {
		test.Fatalf("claimed_by: got %v, want %v", got.ClaimedBy, want.ClaimedBy)
	}

	if !timePtrEqual(got.ClaimedAt, want.ClaimedAt) {
		test.Fatalf("claimed_at: got %v, want %v", got.ClaimedAt, want.ClaimedAt)
	}
}
