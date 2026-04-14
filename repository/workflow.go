package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// WorkflowRepository provides read-only access to workflow definitions.
// Implementations are backed by config, not a database.
type WorkflowRepository interface {
	// GetByID returns a workflow by its typed UUID.
	// Returns domain.ErrNotFound if no workflow with that id exists.
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Workflow, error)

	// GetByName returns the workflow with the given name.
	// Returns domain.ErrNotFound if no workflow with that name exists.
	GetByName(ctx context.Context, name string) (*domain.Workflow, error)

	// List returns all workflows, sorted alphabetically by name.
	List(ctx context.Context) ([]*domain.Workflow, error)
}
