package repository

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
)

// WorkflowRepository provides read-only access to workflow definitions.
// Implementations are backed by config, not a database.
type WorkflowRepository interface {
	// GetByName returns the workflow with the given name.
	// Returns domain.ErrNotFound if no workflow with that name exists.
	GetByName(ctx context.Context, name string) (*domain.Workflow, error)

	// List returns all workflows, sorted alphabetically by name.
	List(ctx context.Context) ([]*domain.Workflow, error)
}
