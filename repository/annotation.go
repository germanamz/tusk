package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

type AnnotationRepository interface {
	Create(ctx context.Context, ann *domain.Annotation) error
	GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Annotation, error)
	GetByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID][]*domain.Annotation, error)
	Delete(ctx context.Context, id uuid.UUID) error
	CountByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
}
