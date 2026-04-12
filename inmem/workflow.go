package inmem

import (
	"context"
	"sort"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
)

var _ repository.WorkflowRepository = (*WorkflowRepository)(nil)

// WorkflowRepository is a read-only, in-memory implementation of
// repository.WorkflowRepository backed by config data.
type WorkflowRepository struct {
	workflows map[string]*domain.Workflow
}

// NewWorkflowRepository builds an in-memory workflow repository from config.
func NewWorkflowRepository(cfgWorkflows map[string]config.WorkflowConfig) *WorkflowRepository {
	workflows := make(map[string]*domain.Workflow, len(cfgWorkflows))
	for name, cfg := range cfgWorkflows {
		wf := &domain.Workflow{
			Name:        name,
			Statuses:    make(map[string]domain.StatusConfig, len(cfg.Statuses)),
			Transitions: make([]domain.WorkflowTransition, len(cfg.Transitions)),
		}
		for statusName, sc := range cfg.Statuses {
			roles := make([]domain.StatusRole, len(sc.Roles))
			for i, r := range sc.Roles {
				roles[i] = domain.StatusRole(r)
			}
			wf.Statuses[statusName] = domain.StatusConfig{Roles: roles}
		}
		for i, t := range cfg.Transitions {
			wf.Transitions[i] = domain.WorkflowTransition{
				FromStatus: t.From,
				ToStatus:   t.To,
			}
		}
		workflows[name] = wf
	}
	return &WorkflowRepository{workflows: workflows}
}

// GetByName returns a defensive copy of the workflow. Returns domain.ErrNotFound if not found.
func (r *WorkflowRepository) GetByName(_ context.Context, name string) (*domain.Workflow, error) {
	wf, ok := r.workflows[name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return copyWorkflow(wf), nil
}

// List returns all workflows sorted alphabetically by name. Each is a defensive copy.
func (r *WorkflowRepository) List(_ context.Context) ([]*domain.Workflow, error) {
	result := make([]*domain.Workflow, 0, len(r.workflows))
	for _, wf := range r.workflows {
		result = append(result, copyWorkflow(wf))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// copyWorkflow returns a deep copy of a Workflow, including the statuses map and slices.
func copyWorkflow(wf *domain.Workflow) *domain.Workflow {
	cp := &domain.Workflow{
		Name:        wf.Name,
		Statuses:    make(map[string]domain.StatusConfig, len(wf.Statuses)),
		Transitions: make([]domain.WorkflowTransition, len(wf.Transitions)),
	}
	for name, sc := range wf.Statuses {
		roles := make([]domain.StatusRole, len(sc.Roles))
		copy(roles, sc.Roles)
		cp.Statuses[name] = domain.StatusConfig{Roles: roles}
	}
	copy(cp.Transitions, wf.Transitions)
	return cp
}
