package repository

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

type TagRepository interface {
	Create(ctx context.Context, tag *domain.Tag) error
	GetByName(ctx context.Context, name string) (*domain.Tag, error)
	List(ctx context.Context) ([]*domain.Tag, error)
	AssignToTask(ctx context.Context, taskID, tagID uuid.UUID) error
	RemoveFromTask(ctx context.Context, taskID, tagID uuid.UUID) error
	GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error)
}
