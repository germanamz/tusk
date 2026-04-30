package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// ProjectRepository provides access to projects.
type ProjectRepository interface {
	// GetByID returns a project by its typed UUID.
	// Returns domain.ErrNotFound if the project doesn't exist.
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)

	// GetByName returns a project by its human-readable name (e.g. "default", "backend").
	// Returns domain.ErrNotFound if the project doesn't exist.
	GetByName(ctx context.Context, name string) (*domain.Project, error)

	// List returns all projects, sorted by name.
	List(ctx context.Context) ([]*domain.Project, error)

	// Create inserts a new project. Returns domain.ErrConflict on name collision.
	Create(ctx context.Context, project *domain.Project) error

	// Update persists changes to a project with optimistic locking.
	// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
	Update(ctx context.Context, project *domain.Project) error

	// Delete removes a project with optimistic locking on version.
	// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
	Delete(ctx context.Context, id uuid.UUID, expectedVersion int) error

	// CountProjectsByWorkflow returns how many projects reference the given workflow.
	CountProjectsByWorkflow(ctx context.Context, workflowID uuid.UUID) (int, error)
}
