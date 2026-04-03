package repository_test

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// Minimal stubs — just enough to prove the interfaces compile.

type stubTaskRepo struct{}

func (s *stubTaskRepo) Create(_ context.Context, _ *domain.Task) error               { return nil }
func (s *stubTaskRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Task, error) { return nil, nil }
func (s *stubTaskRepo) GetByShortID(_ context.Context, _ string) (*domain.Task, error) {
	return nil, nil
}
func (s *stubTaskRepo) Update(_ context.Context, _ *domain.Task) error     { return nil }
func (s *stubTaskRepo) Delete(_ context.Context, _ uuid.UUID, _ int) error { return nil }
func (s *stubTaskRepo) List(_ context.Context, _ domain.TaskFilter) ([]*domain.Task, error) {
	return nil, nil
}
func (s *stubTaskRepo) GetChildren(_ context.Context, _ uuid.UUID) ([]*domain.Task, error) {
	return nil, nil
}
func (s *stubTaskRepo) GetDescendants(_ context.Context, _ uuid.UUID) ([]*domain.Task, error) {
	return nil, nil
}

type stubRelationRepo struct{}

func (s *stubRelationRepo) Create(_ context.Context, _ *domain.Relation) error { return nil }
func (s *stubRelationRepo) Delete(_ context.Context, _ uuid.UUID) error        { return nil }
func (s *stubRelationRepo) DeleteByFields(_ context.Context, _, _ uuid.UUID, _ string) error {
	return nil
}
func (s *stubRelationRepo) GetByTask(_ context.Context, _ uuid.UUID) ([]*domain.Relation, error) {
	return nil, nil
}
func (s *stubRelationRepo) GetBlocking(_ context.Context, _ uuid.UUID) ([]*domain.Relation, error) {
	return nil, nil
}
func (s *stubRelationRepo) GetBlockedBy(_ context.Context, _ uuid.UUID) ([]*domain.Relation, error) {
	return nil, nil
}
func (s *stubRelationRepo) Exists(_ context.Context, _, _ uuid.UUID, _ string) (bool, error) {
	return false, nil
}

type stubProjectRepo struct{}

func (s *stubProjectRepo) Create(_ context.Context, _ *domain.Project) error { return nil }
func (s *stubProjectRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Project, error) {
	return nil, nil
}
func (s *stubProjectRepo) GetByName(_ context.Context, _ string) (*domain.Project, error) {
	return nil, nil
}
func (s *stubProjectRepo) List(_ context.Context) ([]*domain.Project, error) { return nil, nil }
func (s *stubProjectRepo) Update(_ context.Context, _ *domain.Project) error { return nil }
func (s *stubProjectRepo) Delete(_ context.Context, _ uuid.UUID) error       { return nil }

type stubTagRepo struct{}

func (s *stubTagRepo) Create(_ context.Context, _ *domain.Tag) error              { return nil }
func (s *stubTagRepo) GetByName(_ context.Context, _ string) (*domain.Tag, error) { return nil, nil }
func (s *stubTagRepo) List(_ context.Context) ([]*domain.Tag, error)              { return nil, nil }
func (s *stubTagRepo) AssignToTask(_ context.Context, _, _ uuid.UUID) error       { return nil }
func (s *stubTagRepo) RemoveFromTask(_ context.Context, _, _ uuid.UUID) error     { return nil }
func (s *stubTagRepo) GetTaskTags(_ context.Context, _ uuid.UUID) ([]*domain.Tag, error) {
	return nil, nil
}
func (s *stubTagRepo) GetTaskTagsBatch(_ context.Context, _ []uuid.UUID) (map[uuid.UUID][]*domain.Tag, error) {
	return nil, nil
}

type stubWorkflowRepo struct{}

func (s *stubWorkflowRepo) GetByProjectAndName(_ context.Context, _ uuid.UUID, _ string) (*domain.Workflow, error) {
	return nil, nil
}
func (s *stubWorkflowRepo) GetTransitions(_ context.Context, _ uuid.UUID) ([]*domain.WorkflowTransition, error) {
	return nil, nil
}
func (s *stubWorkflowRepo) Create(_ context.Context, _ *domain.Workflow) error { return nil }
func (s *stubWorkflowRepo) AddTransition(_ context.Context, _ *domain.WorkflowTransition) error {
	return nil
}

type stubAnnotationRepo struct{}

func (s *stubAnnotationRepo) Create(_ context.Context, _ *domain.Annotation) error { return nil }
func (s *stubAnnotationRepo) GetByTask(_ context.Context, _ uuid.UUID) ([]*domain.Annotation, error) {
	return nil, nil
}
func (s *stubAnnotationRepo) Delete(_ context.Context, _ uuid.UUID) error { return nil }

func TestInterfaceSatisfaction(t *testing.T) {
	var _ repository.TaskRepository = (*stubTaskRepo)(nil)
	var _ repository.RelationRepository = (*stubRelationRepo)(nil)
	var _ repository.ProjectRepository = (*stubProjectRepo)(nil)
	var _ repository.TagRepository = (*stubTagRepo)(nil)
	var _ repository.WorkflowRepository = (*stubWorkflowRepo)(nil)
	var _ repository.AnnotationRepository = (*stubAnnotationRepo)(nil)
}
