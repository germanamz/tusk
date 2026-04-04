package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// ProjectService encapsulates project business logic including CRUD
// operations and settings management.
type ProjectService struct {
	projectRepo repository.ProjectRepository
}

// NewProjectService creates a new ProjectService.
func NewProjectService(projectRepo repository.ProjectRepository) *ProjectService {
	return &ProjectService{projectRepo: projectRepo}
}

// Create creates a new project with the given name and description.
// It generates a UUID, sets the default workflow to "default", and
// initializes version to 1 with empty settings.
func (s *ProjectService) Create(ctx context.Context, name, description string) (*domain.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("project name must not be empty")
	}

	p := &domain.Project{
		ID:              uuid.New(),
		Name:            name,
		Description:     description,
		DefaultWorkflow: "default",
		Settings:        domain.ProjectSettings{},
		Version:         1,
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.projectRepo.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("creating project %q: %w", name, err)
	}
	return p, nil
}

// List returns all projects.
func (s *ProjectService) List(ctx context.Context) ([]*domain.Project, error) {
	return s.projectRepo.List(ctx)
}

// GetByName retrieves a project by its unique name.
// Returns domain.ErrNotFound if not found.
func (s *ProjectService) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	return s.projectRepo.GetByName(ctx, name)
}

// GetByID retrieves a project by its UUID.
// Returns domain.ErrNotFound if not found.
func (s *ProjectService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	return s.projectRepo.GetByID(ctx, id)
}
