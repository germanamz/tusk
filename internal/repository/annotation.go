package repository

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

type AnnotationRepository interface {
	Create(ctx context.Context, ann *domain.Annotation) error
	GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Annotation, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
