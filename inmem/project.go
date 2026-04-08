package inmem

import (
	"context"
	"sort"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
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
		if cfg.Settings.Urgency != nil {
			p.Settings.Urgency = &domain.UrgencyOverrides{
				PriorityWeight:    cfg.Settings.Urgency.PriorityWeight,
				DueWeight:         cfg.Settings.Urgency.DueWeight,
				AgeWeight:         cfg.Settings.Urgency.AgeWeight,
				ActiveWeight:      cfg.Settings.Urgency.ActiveWeight,
				BlockingWeight:    cfg.Settings.Urgency.BlockingWeight,
				BlockedWeight:     cfg.Settings.Urgency.BlockedWeight,
				TagsWeight:        cfg.Settings.Urgency.TagsWeight,
				ProjectWeight:     cfg.Settings.Urgency.ProjectWeight,
				AnnotationsWeight: cfg.Settings.Urgency.AnnotationsWeight,
				WaitingWeight:     cfg.Settings.Urgency.WaitingWeight,
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
	return copyProject(p), nil
}

// List returns all projects sorted by ID. Each project is a defensive copy.
func (r *ProjectRepository) List(_ context.Context) ([]*domain.Project, error) {
	result := make([]*domain.Project, 0, len(r.projects))
	for _, p := range r.projects {
		result = append(result, copyProject(p))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

// copyProject returns a deep copy of a Project, including pointer fields in Settings.
func copyProject(p *domain.Project) *domain.Project {
	cp := *p
	if p.Settings.AutoCompleteParent != nil {
		acc := *p.Settings.AutoCompleteParent
		cp.Settings.AutoCompleteParent = &acc
	}
	if p.Settings.AutoRevertParent != nil {
		arc := *p.Settings.AutoRevertParent
		cp.Settings.AutoRevertParent = &arc
	}
	if p.Settings.Urgency != nil {
		uo := *p.Settings.Urgency
		cp.Settings.Urgency = &uo
	}
	return &cp
}
