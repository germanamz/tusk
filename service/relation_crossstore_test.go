package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// newCrossStoreRelationSvc builds a RelationService over two disjoint
// bundles and seeds one task into each, so Add can be exercised against
// cross-store endpoints.
func newCrossStoreRelationSvc(t *testing.T) (*RelationService, *domain.Task, *domain.Task) {
	t.Helper()
	ctx := context.Background()
	defaultBundle := newTestBundle(t)
	backendBundle := newTestBundle(t)

	resolver, projects := multiBundleResolver(t, map[string]*RepoBundle{
		"default": defaultBundle,
		"backend": backendBundle,
	})

	defaultTask := &domain.Task{
		ID:        uuid.New(),
		ShortID:   "aaaa1111",
		Title:     "default task",
		ProjectID: "default",
		Status:    "pending",
		Version:   1,
	}
	if err := defaultBundle.Tasks.Create(ctx, defaultTask); err != nil {
		t.Fatalf("seeding default task: %v", err)
	}
	backendTask := &domain.Task{
		ID:        uuid.New(),
		ShortID:   "bbbb2222",
		Title:     "backend task",
		ProjectID: "backend",
		Status:    "pending",
		Version:   1,
	}
	if err := backendBundle.Tasks.Create(ctx, backendTask); err != nil {
		t.Fatalf("seeding backend task: %v", err)
	}

	svc := NewRelationService(resolver, projects)
	return svc, defaultTask, backendTask
}

func TestRelationService_RejectsCrossStore(t *testing.T) {
	ctx := context.Background()
	svc, src, dst := newCrossStoreRelationSvc(t)

	_, err := svc.Add(ctx, src.ShortID, dst.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCrossStoreRelation) {
		t.Fatalf("expected ErrCrossStoreRelation, got %v", err)
	}
}

func TestRelationService_SameStoreAllowed(t *testing.T) {
	ctx := context.Background()
	bundle := newTestBundle(t)
	resolver, projects := singleBundleResolver(bundle, "default")
	svc := NewRelationService(resolver, projects)

	src := &domain.Task{
		ID:        uuid.New(),
		ShortID:   "aaaa1111",
		Title:     "s",
		ProjectID: "default",
		Status:    "pending",
		Version:   1,
	}
	dst := &domain.Task{
		ID:        uuid.New(),
		ShortID:   "bbbb2222",
		Title:     "d",
		ProjectID: "default",
		Status:    "pending",
		Version:   1,
	}
	if err := bundle.Tasks.Create(ctx, src); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Tasks.Create(ctx, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Add(ctx, src.ShortID, dst.ShortID, "blocks"); err != nil {
		t.Fatalf("same-store Add failed: %v", err)
	}
}
