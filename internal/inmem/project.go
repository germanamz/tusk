package inmem

import (
	"context"
	"sort"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
)

// Compile-time interface satisfaction check.
var _ repository.ProjectRepository = (*ProjectRepository)(nil)

// ProjectRepository is a read-only, in-memory implementation of
// repository.ProjectRepository backed by config data.
type ProjectRepository struct {
	projects map[string]*domain.Project
}

// NewProjectRepository builds an in-memory project repository from config.
// The constructor converts config types to domain types. The resulting
// repository is immutable — no locking is needed.
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

// GetByID returns a project by its human-readable ID.
// Returns domain.ErrNotFound if the ID doesn't match any config entry.
func (r *ProjectRepository) GetByID(_ context.Context, id string) (*domain.Project, error) {
	p, ok := r.projects[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copy := *p
	return &copy, nil
}

// List returns all projects sorted by ID for deterministic output.
func (r *ProjectRepository) List(_ context.Context) ([]*domain.Project, error) {
	result := make([]*domain.Project, 0, len(r.projects))
	for _, p := range r.projects {
		copy := *p
		result = append(result, &copy)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}
