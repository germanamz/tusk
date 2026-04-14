package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// newTestBundle creates an in-memory SQLite store and returns a
// RepoBundle wrapping real repositories against it. The store is
// closed via t.Cleanup.
func newTestBundle(t *testing.T) *RepoBundle {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.New(filepath.Join(dir, "test.db"), migrations.FS)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { store.Close() })
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
