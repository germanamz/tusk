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

	// NextOrder returns max("order") + 1.0 for siblings under parentID. parentID == nil
	// scopes to root-level siblings. Returns 1.0 when the group is empty.
	NextOrder(ctx context.Context, parentID *uuid.UUID) (float64, error)

	// FirstOrder returns min("order") - 1.0 for siblings under parentID. parentID == nil
	// scopes to root-level siblings. Returns 1.0 when the group is empty.
	FirstOrder(ctx context.Context, parentID *uuid.UUID) (float64, error)

	// NeighborOrders returns the nearest ordered neighbors of pivot within the sibling
	// group under parentID. prev is the largest order < pivot (nil if none); next is
	// the smallest order > pivot (nil if none). parentID == nil scopes to root.
	NeighborOrders(ctx context.Context, parentID *uuid.UUID, pivot float64) (prev, next *float64, err error)
}
