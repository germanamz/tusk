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

// ModifyOptions specifies what to change on a project.
// All fields are optional — nil means "don't change".
type ModifyOptions struct {
	Description *string           // New description (never nullable, just changeable)
	Sets        map[string]string // Dot-path key → value for settings
	Unsets      []string          // Dot-path keys to nil out in settings
}

// isEmpty returns true if no modifications are specified.
func (o ModifyOptions) isEmpty() bool {
	return o.Description == nil && len(o.Sets) == 0 && len(o.Unsets) == 0
}

// Modify updates a project's fields and/or settings. It fetches the project
// by name, applies the changes, and persists via optimistic-locked update.
// Returns the updated project as read back from the database.
func (s *ProjectService) Modify(ctx context.Context, name string, opts ModifyOptions) (*domain.Project, error) {
	if opts.isEmpty() {
		return nil, fmt.Errorf("no modifications specified")
	}

	project, err := s.projectRepo.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("looking up project %q: %w", name, err)
	}

	if opts.Description != nil {
		project.Description = *opts.Description
	}

	if err := applySettingsChanges(&project.Settings, opts.Sets, opts.Unsets); err != nil {
		return nil, err
	}

	if err := s.projectRepo.Update(ctx, project); err != nil {
		return nil, fmt.Errorf("updating project %q: %w", name, err)
	}

	// Re-read to get the incremented version
	return s.projectRepo.GetByName(ctx, name)
}

// validSetPaths lists all dot-paths that can be used with --set.
// This is NOT a generic JSON walker — it maps a known set of paths
// to ProjectSettings struct fields.
var validSetPaths = map[string]bool{
	"auto_complete_parent.trigger_status": true,
	"auto_complete_parent.target_status":  true,
	"auto_revert_parent.trigger_status":   true,
	"auto_revert_parent.target_status":    true,
}

// validUnsetPaths lists all top-level keys that can be used with --unset.
var validUnsetPaths = map[string]bool{
	"auto_complete_parent": true,
	"auto_revert_parent":   true,
}

// applySettingsChanges applies dot-path --set and --unset operations to settings.
// Returns an error for unknown dot-paths.
func applySettingsChanges(settings *domain.ProjectSettings, sets map[string]string, unsets []string) error {
	for _, key := range unsets {
		if !validUnsetPaths[key] {
			return fmt.Errorf("unknown settings key %q (valid: auto_complete_parent, auto_revert_parent)", key)
		}
		switch key {
		case "auto_complete_parent":
			settings.AutoCompleteParent = nil
		case "auto_revert_parent":
			settings.AutoRevertParent = nil
		}
	}

	for path, value := range sets {
		if !validSetPaths[path] {
			return fmt.Errorf("unknown settings path %q", path)
		}
		switch path {
		case "auto_complete_parent.trigger_status":
			if settings.AutoCompleteParent == nil {
				settings.AutoCompleteParent = &domain.AutoCompleteConfig{}
			}
			settings.AutoCompleteParent.TriggerStatus = value
		case "auto_complete_parent.target_status":
			if settings.AutoCompleteParent == nil {
				settings.AutoCompleteParent = &domain.AutoCompleteConfig{}
			}
			settings.AutoCompleteParent.TargetStatus = value
		case "auto_revert_parent.trigger_status":
			if settings.AutoRevertParent == nil {
				settings.AutoRevertParent = &domain.AutoRevertConfig{}
			}
			settings.AutoRevertParent.TriggerStatus = value
		case "auto_revert_parent.target_status":
			if settings.AutoRevertParent == nil {
				settings.AutoRevertParent = &domain.AutoRevertConfig{}
			}
			settings.AutoRevertParent.TargetStatus = value
		}
	}

	return nil
}
