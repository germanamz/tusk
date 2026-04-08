package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

type TagRepository interface {
	Create(ctx context.Context, tag *domain.Tag) error
	GetByName(ctx context.Context, name string) (*domain.Tag, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Tag, error)
	List(ctx context.Context) ([]*domain.Tag, error)
	ListWithUsage(ctx context.Context) ([]domain.TagWithUsage, error)
	Update(ctx context.Context, tag *domain.Tag) error
	Delete(ctx context.Context, id uuid.UUID) error
	CountTasksByTagID(ctx context.Context, id uuid.UUID) (int, error)
	AssignToTask(ctx context.Context, taskID, tagID uuid.UUID) error
	RemoveFromTask(ctx context.Context, taskID, tagID uuid.UUID) error
	GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error)
	GetTaskTagsBatch(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID][]*domain.Tag, error)
}
