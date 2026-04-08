package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
)

// ProjectRepository provides read-only access to projects.
// Projects are config-driven and immutable at runtime.
type ProjectRepository interface {
	// GetByID returns a project by its human-readable ID (e.g. "default", "backend").
	// Returns domain.ErrNotFound if the project doesn't exist.
	GetByID(ctx context.Context, id string) (*domain.Project, error)

	// List returns all projects, sorted by ID.
	List(ctx context.Context) ([]*domain.Project, error)
}
