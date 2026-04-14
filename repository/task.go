package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

type TaskRepository interface {
	Create(ctx context.Context, task *domain.Task) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error)
	GetByShortID(ctx context.Context, shortID string) (*domain.Task, error)
	Update(ctx context.Context, task *domain.Task) error
	Delete(ctx context.Context, id uuid.UUID, version int) error
	List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error)
	GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error)
	GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error)

	// CountByProject returns how many tasks reference the given project.
	CountByProject(ctx context.Context, projectID uuid.UUID) (int, error)

	// ReassignProject bulk-updates tasks.project_id. Used by ProjectService.Delete
	// under --force to migrate tasks off a project being removed. Returns the
	// number of rows affected. Does not modify version or modified_at for the
	// individual tasks — this is a migration operation, not a user mutation.
	ReassignProject(ctx context.Context, fromID, toID uuid.UUID) (int, error)
}
