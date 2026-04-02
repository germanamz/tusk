package repository

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

type TaskRepository interface {
	Create(ctx context.Context, task *domain.Task) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error)
	GetByShortID(ctx context.Context, shortID string) (*domain.Task, error)
	Update(ctx context.Context, task *domain.Task) error
	Delete(ctx context.Context, id uuid.UUID, version int) error
	List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error)
	GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error)
	GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error)
}
