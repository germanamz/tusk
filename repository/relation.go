package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

type RelationRepository interface {
	Create(ctx context.Context, rel *domain.Relation) error
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteByFields(ctx context.Context, sourceID, targetID uuid.UUID, relType string) error
	GetByFields(ctx context.Context, sourceID, targetID uuid.UUID, relType string) (*domain.Relation, error)
	GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
	GetBlocking(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
	GetBlockedBy(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
	Exists(ctx context.Context, sourceID, targetID uuid.UUID, relType string) (bool, error)
	CountBlockingByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
	CountBlockedByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
	CountBlockedByIncompleteTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
}
