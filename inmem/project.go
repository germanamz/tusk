package inmem

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// Compile-time check that ProjectRepository implements the interface.
var _ repository.ProjectRepository = (*ProjectRepository)(nil)

// ProjectRepository is a read-only, in-memory implementation of
// repository.ProjectRepository backed by config data.
type ProjectRepository struct {
	mu       sync.RWMutex
	projects map[string]*domain.Project
}

// NewProjectRepository builds an in-memory project repository from config.
func NewProjectRepository(cfgProjects map[string]config.ProjectConfig) *ProjectRepository {
	return &ProjectRepository{projects: buildProjectMap(cfgProjects)}
}

// Reload atomically replaces the project set. Safe for concurrent readers.
func (r *ProjectRepository) Reload(cfgProjects map[string]config.ProjectConfig) {
	next := buildProjectMap(cfgProjects)
	r.mu.Lock()
	r.projects = next
	r.mu.Unlock()
}

func buildProjectMap(cfgProjects map[string]config.ProjectConfig) map[string]*domain.Project {
	now := time.Now().UTC().Truncate(time.Millisecond)
	projects := make(map[string]*domain.Project, len(cfgProjects))
	for name, cfg := range cfgProjects {
		p := &domain.Project{
			ID:         uuid.NewSHA1(uuid.Nil, []byte("project:"+name)),
			Name:       name,
			WorkflowID: uuid.NewSHA1(uuid.Nil, []byte("workflow:"+cfg.Workflow)),
			Workflow:   cfg.Workflow,
			Version:    1,
			CreatedAt:  now,
			UpdatedAt:  now,
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
		projects[name] = p
	}
	return projects
}

// GetByID returns a defensive copy of the project matched by UUID.
// Returns domain.ErrNotFound if not found.
func (r *ProjectRepository) GetByID(_ context.Context, id uuid.UUID) (*domain.Project, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.projects {
		if p.ID == id {
			return copyProject(p), nil
		}
	}
	return nil, domain.ErrNotFound
}

// GetByName returns a defensive copy of the project. Returns domain.ErrNotFound if not found.
func (r *ProjectRepository) GetByName(_ context.Context, name string) (*domain.Project, error) {
	r.mu.RLock()
	p, ok := r.projects[name]
	r.mu.RUnlock()
	if !ok {
		return nil, domain.ErrNotFound
	}
	return copyProject(p), nil
}

// List returns all projects sorted by name. Each project is a defensive copy.
func (r *ProjectRepository) List(_ context.Context) ([]*domain.Project, error) {
	r.mu.RLock()
	result := make([]*domain.Project, 0, len(r.projects))
	for _, p := range r.projects {
		result = append(result, copyProject(p))
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// copyProject returns a deep copy of a Project, including pointer fields in Settings.
func copyProject(p *domain.Project) *domain.Project {
	cp := &domain.Project{
		ID:         p.ID,
		Name:       p.Name,
		WorkflowID: p.WorkflowID,
		Workflow:   p.Workflow,
		Settings:   p.Settings,
		Version:    p.Version,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
	}
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
	return cp
}
