package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

// testWriteTxProvider wraps a *sqlite.Store so service tests can exercise
// WriteTx without depending on cmd/tusk/main.go's adapter wiring.
type testWriteTxProvider struct {
	store      *sqlite.Store
	maxEvents  int
	pruneSlack int
}

type testWriteTx struct {
	tx         *sqlite.Tx
	maxEvents  int
	pruneSlack int
}

func (w *testWriteTx) Tasks() repository.TaskRepository         { return w.tx.Tasks() }
func (w *testWriteTx) Relations() repository.RelationRepository { return w.tx.Relations() }
func (w *testWriteTx) Events() repository.EventRepository {
	return w.tx.Events(w.maxEvents, w.pruneSlack)
}

func (w *testWriteTx) Projects() repository.ProjectRepository       { return w.tx.Projects() }
func (w *testWriteTx) Workflows() repository.WorkflowRepository     { return w.tx.Workflows() }
func (w *testWriteTx) Players() repository.PlayerRepository         { return w.tx.Players() }
func (w *testWriteTx) Tags() repository.TagRepository               { return w.tx.Tags() }
func (w *testWriteTx) Annotations() repository.AnnotationRepository { return w.tx.Annotations() }
func (w *testWriteTx) Notes() repository.NoteRepository             { return w.tx.Notes() }

func (w *testWriteTx) TruncateAll(ctx context.Context) error { return w.tx.TruncateAll(ctx) }

func (p *testWriteTxProvider) WithTx(ctx context.Context, fn func(tx WriteTx) error) error {
	return p.store.WithTx(ctx, func(stx *sqlite.Tx) error {
		return fn(&testWriteTx{tx: stx, maxEvents: p.maxEvents, pruneSlack: p.pruneSlack})
	})
}

// newTestBundle creates an in-memory SQLite store and returns a
// RepoBundle wrapping real repositories against it. The store is
// closed via t.Cleanup.
func newTestBundle(test *testing.T) *RepoBundle {
	test.Helper()
	store, _, _ := sqlitetest.NewStore(test)
	return bundleFromStore(store)
}

// newSeededBundle builds a RepoBundle plus SQLite project/workflow repos
// backed by a fresh store. Migrations seed the builtin kanban workflow and
// default project; additional projects can be added via
// sqlitetest.SeedProject. All repos share the same store so task ↔ project
// FKs are satisfied.
func newSeededBundle(test *testing.T) (*RepoBundle, *sqlite.ProjectRepo, *sqlite.WorkflowRepo) {
	test.Helper()
	store, projRepo, wfRepo := sqlitetest.NewStore(test)
	return bundleFromStore(store), projRepo, wfRepo
}

func bundleFromStore(store *sqlite.Store) *RepoBundle {
	db := store.DB()
	return &RepoBundle{
		Store:       store,
		WriteTx:     &testWriteTxProvider{store: store, maxEvents: 10000, pruneSlack: 1000},
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
		Notes:       sqlite.NewNoteRepo(db),
		Relations:   sqlite.NewRelationRepo(db),
		Tags:        sqlite.NewTagRepo(db),
		Players:     sqlite.NewPlayerRepo(db),
	}
}

// singleBundleResolver returns a resolver and project lister backed by
// a single bundle that answers every project ID with the same bundle.
// Projects must still be registered in the ProjectRepository for
// project-lookup validation to succeed.
func singleBundleResolver(bundle *RepoBundle, projectIDs ...uuid.UUID) (BundleResolver, ProjectLister) {
	ids := append([]uuid.UUID(nil), projectIDs...)
	resolver := func(_ context.Context, _ uuid.UUID) (*RepoBundle, error) {
		return bundle, nil
	}
	lister := func(context.Context) ([]uuid.UUID, error) {
		return ids, nil
	}
	return resolver, lister
}
