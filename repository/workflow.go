package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// WorkflowRepository provides access to workflow definitions.
type WorkflowRepository interface {
	// GetByID returns a workflow by its typed UUID.
	// Returns domain.ErrNotFound if no workflow with that id exists.
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Workflow, error)

	// GetByName returns the workflow with the given name.
	// Returns domain.ErrNotFound if no workflow with that name exists.
	GetByName(ctx context.Context, name string) (*domain.Workflow, error)

	// List returns all workflows, sorted alphabetically by name.
	List(ctx context.Context) ([]*domain.Workflow, error)

	// Create inserts a new workflow. Returns domain.ErrConflict on name collision.
	Create(ctx context.Context, workflow *domain.Workflow) error

	// Update persists changes to a workflow with optimistic locking.
	// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
	Update(ctx context.Context, workflow *domain.Workflow) error

	// Delete removes a workflow with optimistic locking on version.
	// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
	Delete(ctx context.Context, id uuid.UUID, expectedVersion int) error
}
