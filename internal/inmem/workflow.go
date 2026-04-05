package inmem

import (
	"context"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// Compile-time check that WorkflowRepository implements the interface.
var _ repository.WorkflowRepository = (*WorkflowRepository)(nil)

// WorkflowRepository is a read-only, in-memory implementation of
// repository.WorkflowRepository backed by config data.
// This is a bridge implementation that satisfies the current interface
// (with UUID-keyed lookups). It will be simplified when the interface
// is updated to name-keyed lookups in a later phase.
type WorkflowRepository struct {
	byName      map[string]*domain.Workflow
	transitions map[uuid.UUID][]*domain.WorkflowTransition
}

// NewWorkflowRepository builds an in-memory workflow repository from config.
func NewWorkflowRepository(cfgWorkflows map[string]config.WorkflowConfig) *WorkflowRepository {
	r := &WorkflowRepository{
		byName:      make(map[string]*domain.Workflow, len(cfgWorkflows)),
		transitions: make(map[uuid.UUID][]*domain.WorkflowTransition, len(cfgWorkflows)),
	}
	for name, cfg := range cfgWorkflows {
		id := uuid.New()
		wf := &domain.Workflow{
			ID:       id,
			Name:     name,
			Statuses: make([]string, len(cfg.Statuses)),
		}
		copy(wf.Statuses, cfg.Statuses)
		r.byName[name] = wf

		for _, t := range cfg.Transitions {
			r.transitions[id] = append(r.transitions[id], &domain.WorkflowTransition{
				ID:         uuid.New(),
				WorkflowID: id,
				FromStatus: t.From,
				ToStatus:   t.To,
			})
		}
	}
	return r
}

// GetByProjectAndName returns the workflow with the given name.
// projectID is accepted for interface compatibility but ignored —
// workflows are global in config, not per-project.
func (r *WorkflowRepository) GetByProjectAndName(_ context.Context, _ string, name string) (*domain.Workflow, error) {
	wf, ok := r.byName[name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *wf
	cp.Statuses = make([]string, len(wf.Statuses))
	copy(cp.Statuses, wf.Statuses)
	return &cp, nil
}

// GetTransitions returns the transitions for the workflow with the given ID.
func (r *WorkflowRepository) GetTransitions(_ context.Context, workflowID uuid.UUID) ([]*domain.WorkflowTransition, error) {
	ts, ok := r.transitions[workflowID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return ts, nil
}

// Create is a no-op. Workflows are defined in config.
func (r *WorkflowRepository) Create(_ context.Context, _ *domain.Workflow) error {
	return nil
}

// AddTransition is a no-op. Transitions are defined in config.
func (r *WorkflowRepository) AddTransition(_ context.Context, _ *domain.WorkflowTransition) error {
	return nil
}
