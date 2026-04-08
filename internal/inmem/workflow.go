package inmem

import (
	"context"
	"sort"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/repository"
)

// Compile-time check that WorkflowRepository implements the interface.
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
			Statuses:    make([]string, len(cfg.Statuses)),
			Transitions: make([]domain.WorkflowTransition, len(cfg.Transitions)),
		}
		copy(wf.Statuses, cfg.Statuses)
		for i, t := range cfg.Transitions {
			wf.Transitions[i] = domain.WorkflowTransition{
				FromStatus: t.From,
				ToStatus:   t.To,
			}
		}
		wf.HighlightStatuses = make([]string, len(cfg.HighlightStatuses))
		copy(wf.HighlightStatuses, cfg.HighlightStatuses)
		wf.DimStatuses = make([]string, len(cfg.DimStatuses))
		copy(wf.DimStatuses, cfg.DimStatuses)
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

// copyWorkflow returns a deep copy of a Workflow, including slices.
func copyWorkflow(wf *domain.Workflow) *domain.Workflow {
	cp := *wf
	cp.Statuses = make([]string, len(wf.Statuses))
	copy(cp.Statuses, wf.Statuses)
	cp.Transitions = make([]domain.WorkflowTransition, len(wf.Transitions))
	copy(cp.Transitions, wf.Transitions)
	cp.HighlightStatuses = make([]string, len(wf.HighlightStatuses))
	copy(cp.HighlightStatuses, wf.HighlightStatuses)
	cp.DimStatuses = make([]string, len(wf.DimStatuses))
	copy(cp.DimStatuses, wf.DimStatuses)
	return &cp
}
