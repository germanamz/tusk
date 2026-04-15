package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

// newTestBundle creates an in-memory SQLite store and returns a
// RepoBundle wrapping real repositories against it. The store is
// closed via t.Cleanup.
func newTestBundle(t *testing.T) *RepoBundle {
	t.Helper()
	store, _, _ := sqlitetest.NewStore(t)
	return bundleFromStore(store)
}

// newSeededBundle builds a RepoBundle plus SQLite project/workflow repos
// backed by a fresh store. Migrations seed the builtin kanban workflow and
// default project; additional projects can be added via
// sqlitetest.SeedProject. All repos share the same store so task ↔ project
// FKs are satisfied.
func newSeededBundle(t *testing.T) (*RepoBundle, *sqlite.ProjectRepo, *sqlite.WorkflowRepo) {
	t.Helper()
	store, projRepo, wfRepo := sqlitetest.NewStore(t)
	return bundleFromStore(store), projRepo, wfRepo
}

func bundleFromStore(store *sqlite.Store) *RepoBundle {
	db := store.DB()
	return &RepoBundle{
		Store:       store,
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
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
