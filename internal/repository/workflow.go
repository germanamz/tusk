package repository

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

type WorkflowRepository interface {
	GetByProjectAndName(ctx context.Context, projectID string, name string) (*domain.Workflow, error)
	GetTransitions(ctx context.Context, workflowID uuid.UUID) ([]*domain.WorkflowTransition, error)
	Create(ctx context.Context, wf *domain.Workflow) error
	AddTransition(ctx context.Context, t *domain.WorkflowTransition) error
}
