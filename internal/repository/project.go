package repository

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

type ProjectRepository interface {
	Create(ctx context.Context, project *domain.Project) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)
	GetByName(ctx context.Context, name string) (*domain.Project, error)
	List(ctx context.Context) ([]*domain.Project, error)
	Update(ctx context.Context, project *domain.Project) error
	Delete(ctx context.Context, id uuid.UUID) error
}
