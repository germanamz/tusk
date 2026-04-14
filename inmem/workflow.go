package inmem

import (
	"context"
	"sort"
	"sync"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

var _ repository.WorkflowRepository = (*WorkflowRepository)(nil)

// WorkflowRepository is a read-only, in-memory implementation of
// repository.WorkflowRepository backed by config data.
type WorkflowRepository struct {
	mu        sync.RWMutex
	workflows map[string]*domain.Workflow
}

// NewWorkflowRepository builds an in-memory workflow repository from config.
func NewWorkflowRepository(cfgWorkflows map[string]config.WorkflowConfig) *WorkflowRepository {
	return &WorkflowRepository{workflows: buildWorkflowMap(cfgWorkflows)}
}

// Reload atomically replaces the workflow set. Safe for concurrent readers.
func (r *WorkflowRepository) Reload(cfgWorkflows map[string]config.WorkflowConfig) {
	next := buildWorkflowMap(cfgWorkflows)
	r.mu.Lock()
	r.workflows = next
	r.mu.Unlock()
}

func buildWorkflowMap(cfgWorkflows map[string]config.WorkflowConfig) map[string]*domain.Workflow {
	workflows := make(map[string]*domain.Workflow, len(cfgWorkflows))
	for name, cfg := range cfgWorkflows {
		wf, err := config.WorkflowFromConfig(name, cfg)
		if err != nil {
			continue
		}
		workflows[name] = wf
	}
	return workflows
}

// GetByID returns a defensive copy of the workflow matched by UUID.
// Returns domain.ErrNotFound if not found.
func (r *WorkflowRepository) GetByID(_ context.Context, id uuid.UUID) (*domain.Workflow, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, wf := range r.workflows {
		if wf.ID == id {
			return copyWorkflow(wf), nil
		}
	}
	return nil, domain.ErrNotFound
}

// GetByName returns a defensive copy of the workflow. Returns domain.ErrNotFound if not found.
func (r *WorkflowRepository) GetByName(_ context.Context, name string) (*domain.Workflow, error) {
	r.mu.RLock()
	wf, ok := r.workflows[name]
	r.mu.RUnlock()
	if !ok {
		return nil, domain.ErrNotFound
	}
	return copyWorkflow(wf), nil
}

// List returns all workflows sorted alphabetically by name. Each is a defensive copy.
func (r *WorkflowRepository) List(_ context.Context) ([]*domain.Workflow, error) {
	r.mu.RLock()
	result := make([]*domain.Workflow, 0, len(r.workflows))
	for _, wf := range r.workflows {
		result = append(result, copyWorkflow(wf))
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// Bridge stubs: inmem is read-only. These exist so the package satisfies
// the expanded repository.WorkflowRepository interface until inmem is deleted
// in Phase 5 of the service layer migration.

func (r *WorkflowRepository) Create(context.Context, *domain.Workflow) error {
	return domain.ErrReadOnlyRepository
}

func (r *WorkflowRepository) Update(context.Context, *domain.Workflow) error {
	return domain.ErrReadOnlyRepository
}

func (r *WorkflowRepository) Delete(context.Context, uuid.UUID, int) error {
	return domain.ErrReadOnlyRepository
}

// copyWorkflow returns a deep copy of a Workflow, including the statuses map and slices.
func copyWorkflow(wf *domain.Workflow) *domain.Workflow {
	cp := &domain.Workflow{
		ID:          wf.ID,
		Name:        wf.Name,
		Statuses:    make(map[string]domain.StatusConfig, len(wf.Statuses)),
		Transitions: make([]domain.WorkflowTransition, len(wf.Transitions)),
		Version:     wf.Version,
		CreatedAt:   wf.CreatedAt,
		UpdatedAt:   wf.UpdatedAt,
	}
	for name, sc := range wf.Statuses {
		roles := make([]domain.StatusRole, len(sc.Roles))
		copy(roles, sc.Roles)
		cp.Statuses[name] = domain.StatusConfig{Roles: roles}
	}
	copy(cp.Transitions, wf.Transitions)
	return cp
}
