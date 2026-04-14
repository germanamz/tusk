package service

import (
	"context"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
)

// ProjectService provides read-only access to projects.
// Projects are config-driven — there are no create/update/delete operations.
type ProjectService struct {
	projectRepo repository.ProjectRepository
}

// NewProjectService creates a new ProjectService.
func NewProjectService(projectRepo repository.ProjectRepository) *ProjectService {
	return &ProjectService{projectRepo: projectRepo}
}

// GetByName retrieves a project by its human-readable name.
// Returns domain.ErrNotFound if not found.
func (s *ProjectService) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	return s.projectRepo.GetByName(ctx, name)
}

// List returns all projects from config.
func (s *ProjectService) List(ctx context.Context) ([]*domain.Project, error) {
	return s.projectRepo.List(ctx)
}
