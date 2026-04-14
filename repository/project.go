package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
)

// ProjectRepository provides read access to projects.
// Write operations (Create/Update/Delete) are exposed as concrete methods
// on sqlite.ProjectRepo in Phase 2 and are not yet part of this interface;
// they will be promoted to the interface in the v0.11 Service Layer Migration.
type ProjectRepository interface {
	// GetByName returns a project by its human-readable name (e.g. "default", "backend").
	// Returns domain.ErrNotFound if the project doesn't exist.
	GetByName(ctx context.Context, name string) (*domain.Project, error)

	// List returns all projects, sorted by name.
	List(ctx context.Context) ([]*domain.Project, error)
}
