package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// Minimal stubs — just enough to prove the interfaces compile.

type stubTaskRepo struct{}

func (stub *stubTaskRepo) Create(_ context.Context, _ *domain.Task) error { return nil }
func (stub *stubTaskRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Task, error) {
	return nil, nil
}
func (stub *stubTaskRepo) GetByShortID(_ context.Context, _ string) (*domain.Task, error) {
	return nil, nil
}
func (stub *stubTaskRepo) Update(_ context.Context, _ *domain.Task) error     { return nil }
func (stub *stubTaskRepo) Delete(_ context.Context, _ uuid.UUID, _ int) error { return nil }
func (stub *stubTaskRepo) List(_ context.Context, _ domain.FilterExpr) ([]*domain.Task, error) {
	return nil, nil
}
func (stub *stubTaskRepo) GetChildren(_ context.Context, _ uuid.UUID) ([]*domain.Task, error) {
	return nil, nil
}
func (stub *stubTaskRepo) GetDescendants(_ context.Context, _ uuid.UUID) ([]*domain.Task, error) {
	return nil, nil
}
func (stub *stubTaskRepo) CountByProject(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}
func (stub *stubTaskRepo) ReassignProject(_ context.Context, _, _ uuid.UUID) (int, error) {
	return 0, nil
}
func (stub *stubTaskRepo) NextOrder(_ context.Context, _ *uuid.UUID) (float64, error) {
	return 0, nil
}
func (stub *stubTaskRepo) FirstOrder(_ context.Context, _ *uuid.UUID) (float64, error) {
	return 0, nil
}
func (stub *stubTaskRepo) NeighborOrders(_ context.Context, _ *uuid.UUID, _ float64) (*float64, *float64, error) {
	return nil, nil, nil
}
func (stub *stubTaskRepo) UpdateOrderAndParent(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ float64, _ int, _ time.Time) (int, error) {
	return 0, nil
}
func (stub *stubTaskRepo) GetAncestorOverrides(_ context.Context, _ []uuid.UUID) ([]repository.AncestorOverride, error) {
	return nil, nil
}

type stubRelationRepo struct{}

func (stub *stubRelationRepo) Create(_ context.Context, _ *domain.Relation) error { return nil }
func (stub *stubRelationRepo) Delete(_ context.Context, _ uuid.UUID) error        { return nil }
func (stub *stubRelationRepo) DeleteByFields(_ context.Context, _, _ uuid.UUID, _ string) error {
	return nil
}
func (stub *stubRelationRepo) GetByFields(_ context.Context, _, _ uuid.UUID, _ string) (*domain.Relation, error) {
	return nil, nil
}
func (stub *stubRelationRepo) GetByTask(_ context.Context, _ uuid.UUID) ([]*domain.Relation, error) {
	return nil, nil
}
func (stub *stubRelationRepo) GetBlocking(_ context.Context, _ uuid.UUID) ([]*domain.Relation, error) {
	return nil, nil
}
func (stub *stubRelationRepo) GetBlockedBy(_ context.Context, _ uuid.UUID) ([]*domain.Relation, error) {
	return nil, nil
}
func (stub *stubRelationRepo) Exists(_ context.Context, _, _ uuid.UUID, _ string) (bool, error) {
	return false, nil
}
func (stub *stubRelationRepo) CountBlockingByTasks(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]int, error) {
	return nil, nil
}
func (stub *stubRelationRepo) CountBlockedByTasks(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]int, error) {
	return nil, nil
}
func (stub *stubRelationRepo) CountBlockedByIncompleteTasks(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]int, error) {
	return map[uuid.UUID]int{}, nil
}

type stubProjectRepo struct{}

func (stub *stubProjectRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Project, error) {
	return nil, nil
}
func (stub *stubProjectRepo) GetByName(_ context.Context, _ string) (*domain.Project, error) {
	return nil, nil
}
func (stub *stubProjectRepo) List(_ context.Context) ([]*domain.Project, error) { return nil, nil }
func (stub *stubProjectRepo) Create(_ context.Context, _ *domain.Project) error { return nil }
func (stub *stubProjectRepo) Update(_ context.Context, _ *domain.Project) error { return nil }
func (stub *stubProjectRepo) Delete(_ context.Context, _ uuid.UUID, _ int) error {
	return nil
}
func (stub *stubProjectRepo) CountProjectsByWorkflow(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

type stubTagRepo struct{}

func (stub *stubTagRepo) Create(_ context.Context, _ *domain.Tag) error              { return nil }
func (stub *stubTagRepo) GetByName(_ context.Context, _ string) (*domain.Tag, error) { return nil, nil }
func (stub *stubTagRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Tag, error) {
	return nil, nil
}
func (stub *stubTagRepo) List(_ context.Context) ([]*domain.Tag, error) { return nil, nil }
func (stub *stubTagRepo) ListWithUsage(_ context.Context) ([]domain.TagWithUsage, error) {
	return nil, nil
}
func (stub *stubTagRepo) Update(_ context.Context, _ *domain.Tag) error { return nil }
func (stub *stubTagRepo) Delete(_ context.Context, _ uuid.UUID) error   { return nil }
func (stub *stubTagRepo) CountTasksByTagID(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}
func (stub *stubTagRepo) AssignToTask(_ context.Context, _, _ uuid.UUID) error   { return nil }
func (stub *stubTagRepo) RemoveFromTask(_ context.Context, _, _ uuid.UUID) error { return nil }
func (stub *stubTagRepo) GetTaskTags(_ context.Context, _ uuid.UUID) ([]*domain.Tag, error) {
	return nil, nil
}
func (stub *stubTagRepo) GetTaskTagsBatch(_ context.Context, _ []uuid.UUID) (map[uuid.UUID][]*domain.Tag, error) {
	return nil, nil
}

type stubWorkflowRepo struct{}

func (stub *stubWorkflowRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Workflow, error) {
	return nil, nil
}
func (stub *stubWorkflowRepo) GetByName(_ context.Context, _ string) (*domain.Workflow, error) {
	return nil, nil
}
func (stub *stubWorkflowRepo) List(_ context.Context) ([]*domain.Workflow, error) {
	return nil, nil
}
func (stub *stubWorkflowRepo) Create(_ context.Context, _ *domain.Workflow) error { return nil }
func (stub *stubWorkflowRepo) Update(_ context.Context, _ *domain.Workflow) error { return nil }
func (stub *stubWorkflowRepo) Delete(_ context.Context, _ uuid.UUID, _ int) error {
	return nil
}

type stubAnnotationRepo struct{}

func (stub *stubAnnotationRepo) Create(_ context.Context, _ *domain.Annotation) error { return nil }
func (stub *stubAnnotationRepo) GetByTask(_ context.Context, _ uuid.UUID) ([]*domain.Annotation, error) {
	return nil, nil
}
func (stub *stubAnnotationRepo) GetByTasks(_ context.Context, _ []uuid.UUID) (map[uuid.UUID][]*domain.Annotation, error) {
	return map[uuid.UUID][]*domain.Annotation{}, nil
}
func (stub *stubAnnotationRepo) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (stub *stubAnnotationRepo) CountByTasks(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]int, error) {
	return nil, nil
}

func TestInterfaceSatisfaction(test *testing.T) {
	var _ repository.TaskRepository = (*stubTaskRepo)(nil)
	var _ repository.RelationRepository = (*stubRelationRepo)(nil)
	var _ repository.ProjectRepository = (*stubProjectRepo)(nil)
	var _ repository.TagRepository = (*stubTagRepo)(nil)
	var _ repository.WorkflowRepository = (*stubWorkflowRepo)(nil)
	var _ repository.AnnotationRepository = (*stubAnnotationRepo)(nil)
}
