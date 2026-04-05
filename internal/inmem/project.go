package inmem

import (
	"context"
	"sort"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
)

// Compile-time check that ProjectRepository implements the interface.
var _ repository.ProjectRepository = (*ProjectRepository)(nil)

// ProjectRepository is a read-only, in-memory implementation of
// repository.ProjectRepository backed by config data.
type ProjectRepository struct {
	projects map[string]*domain.Project
}

// NewProjectRepository builds an in-memory project repository from config.
func NewProjectRepository(cfgProjects map[string]config.ProjectConfig) *ProjectRepository {
	projects := make(map[string]*domain.Project, len(cfgProjects))
	for id, cfg := range cfgProjects {
		p := &domain.Project{
			ID:       id,
			Workflow: cfg.Workflow,
		}
		if cfg.Settings.AutoCompleteParent != nil {
			p.Settings.AutoCompleteParent = &domain.AutoCompleteConfig{
				TriggerStatus: cfg.Settings.AutoCompleteParent.TriggerStatus,
				TargetStatus:  cfg.Settings.AutoCompleteParent.TargetStatus,
			}
		}
		if cfg.Settings.AutoRevertParent != nil {
			p.Settings.AutoRevertParent = &domain.AutoRevertConfig{
				TriggerStatus: cfg.Settings.AutoRevertParent.TriggerStatus,
				TargetStatus:  cfg.Settings.AutoRevertParent.TargetStatus,
			}
		}
		projects[id] = p
	}
	return &ProjectRepository{projects: projects}
}

// GetByID returns a defensive copy of the project. Returns domain.ErrNotFound if not found.
func (r *ProjectRepository) GetByID(_ context.Context, id string) (*domain.Project, error) {
	p, ok := r.projects[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	// Return a copy so callers can't mutate our internal state
	cp := *p
	return &cp, nil
}

// List returns all projects sorted by ID. Each project is a defensive copy.
func (r *ProjectRepository) List(_ context.Context) ([]*domain.Project, error) {
	result := make([]*domain.Project, 0, len(r.projects))
	for _, p := range r.projects {
		cp := *p
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}
