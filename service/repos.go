// Package service — repos.go declares the RepoBundle struct and resolver
// types used to route per-project operations to the SQLite store that owns
// each project's data. Phase 3 ships the types only; cmd/tusk/main.go wires
// real implementations in Phase 4.
package service

import (
	"context"

	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/sqlite"
)

// RepoBundle groups the repositories and the underlying store (used as a
// transaction provider) for a single SQLite database. Every project resolves
// to exactly one bundle.
type RepoBundle struct {
	Store       *sqlite.Store
	Tasks       repository.TaskRepository
	Annotations repository.AnnotationRepository
	Relations   repository.RelationRepository
	Tags        repository.TagRepository
	Players     repository.PlayerRepository
}

// BundleResolver returns the RepoBundle that owns the given project.
// Implementations are wired in cmd/tusk/main.go.
type BundleResolver func(ctx context.Context, projectID string) (*RepoBundle, error)

// ProjectLister returns every project ID currently known to the resolver.
// Used by fan-out reads in Phase 4.
type ProjectLister func(ctx context.Context) ([]string, error)
