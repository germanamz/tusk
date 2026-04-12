package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
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
func singleBundleResolver(bundle *RepoBundle, projectIDs ...string) (BundleResolver, ProjectLister) {
	ids := append([]string(nil), projectIDs...)
	resolver := func(_ context.Context, _ string) (*RepoBundle, error) {
		return bundle, nil
	}
	lister := func(context.Context) ([]string, error) {
		return ids, nil
	}
	return resolver, lister
}

// multiBundleResolver returns a resolver and project lister that map
// each project ID to a distinct bundle.
func multiBundleResolver(t *testing.T, bundles map[string]*RepoBundle) (BundleResolver, ProjectLister) {
	t.Helper()
	ids := make([]string, 0, len(bundles))
	for k := range bundles {
		ids = append(ids, k)
	}
	resolver := func(_ context.Context, projectID string) (*RepoBundle, error) {
		b, ok := bundles[projectID]
		if !ok {
			t.Fatalf("multiBundleResolver: unknown project %q", projectID)
		}
		return b, nil
	}
	lister := func(context.Context) ([]string, error) {
		return ids, nil
	}
	return resolver, lister
}
