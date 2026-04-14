package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// WorkflowService validates status transitions and provides read + write access
// to workflow definitions backed by a repository.
type WorkflowService struct {
	workflowRepo repository.WorkflowRepository
	projectRepo  repository.ProjectRepository
}

// NewWorkflowService creates a new WorkflowService.
func NewWorkflowService(wr repository.WorkflowRepository, pr repository.ProjectRepository) *WorkflowService {
	return &WorkflowService{workflowRepo: wr, projectRepo: pr}
}

// IsTransitionAllowed checks whether a status transition is permitted
// by the named workflow.
func (s *WorkflowService) IsTransitionAllowed(ctx context.Context, workflowName string, from string, to string) (bool, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return false, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	for _, t := range wf.Transitions {
		if t.FromStatus == from && t.ToStatus == to {
			return true, nil
		}
	}
	return false, nil
}

// GetStatuses returns the ordered list of valid statuses for the named workflow.
func (s *WorkflowService) GetStatuses(ctx context.Context, workflowName string) ([]string, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.StatusNames(), nil
}

// GetStatusByRole returns the name of the status with the given role in the named workflow.
// Returns an error if the workflow doesn't exist or no status has the role.
func (s *WorkflowService) GetStatusByRole(ctx context.Context, workflowName string, role domain.StatusRole) (string, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return "", fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	name, ok := wf.StatusByRole(role)
	if !ok {
		return "", fmt.Errorf("workflow %q has no status with role %q", workflowName, role)
	}
	return name, nil
}

// GetNonTerminalStatuses returns status names without the terminal role, sorted.
func (s *WorkflowService) GetNonTerminalStatuses(ctx context.Context, workflowName string) ([]string, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.NonTerminalStatuses(), nil
}

// GetDeleteStatus returns the name of the status with the delete role.
func (s *WorkflowService) GetDeleteStatus(ctx context.Context, workflowName string) (string, error) {
	return s.GetStatusByRole(ctx, workflowName, domain.RoleDelete)
}

// GetTransitions returns all allowed transitions for the named workflow.
func (s *WorkflowService) GetTransitions(ctx context.Context, workflowName string) ([]domain.WorkflowTransition, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Transitions, nil
}

// List returns all workflows from the repository.
func (s *WorkflowService) List(ctx context.Context) ([]*domain.Workflow, error) {
	return s.workflowRepo.List(ctx)
}

// GetByName returns a single workflow by name.
// Returns domain.ErrNotFound if the workflow does not exist.
func (s *WorkflowService) GetByName(ctx context.Context, name string) (*domain.Workflow, error) {
	return s.workflowRepo.GetByName(ctx, name)
}

// GetByID returns a single workflow by typed UUID.
// Returns domain.ErrNotFound if the workflow does not exist.
func (s *WorkflowService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Workflow, error) {
	return s.workflowRepo.GetByID(ctx, id)
}

// GetWorkflowWithProjects returns a workflow and the sorted list of project IDs
// that reference it. Returns domain.ErrNotFound if the workflow does not exist.
func (s *WorkflowService) GetWorkflowWithProjects(ctx context.Context, name string) (*domain.Workflow, []string, error) {
	wf, err := s.workflowRepo.GetByName(ctx, name)
	if err != nil {
		return nil, nil, err
	}

	projects, err := s.projectRepo.List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("listing projects: %w", err)
	}

	var projectIDs []string
	for _, p := range projects {
		if p.WorkflowID == wf.ID {
			projectIDs = append(projectIDs, p.Name)
		}
	}
	sort.Strings(projectIDs)
	return wf, projectIDs, nil
}

// CreateWorkflowInput describes a new workflow to be persisted.
type CreateWorkflowInput struct {
	Name        string
	Statuses    map[string]domain.StatusConfig
	Transitions []domain.WorkflowTransition
}

// ModifyWorkflowInput describes a mutation against an existing workflow.
// ExpectedVersion drives optimistic locking — a mismatch returns
// domain.ErrConflict.
type ModifyWorkflowInput struct {
	Name              string
	ExpectedVersion   int
	AddStatuses       map[string]domain.StatusConfig
	SetStatuses       map[string]domain.StatusConfig
	RemoveStatuses    []string
	AddTransitions    []domain.WorkflowTransition
	RemoveTransitions []domain.WorkflowTransition
}

// Create inserts a new workflow after running role-schema validation.
func (s *WorkflowService) Create(ctx context.Context, input CreateWorkflowInput) (*domain.Workflow, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: workflow name is empty", domain.ErrInvalidWorkflow)
	}

	existing, err := s.workflowRepo.GetByName(ctx, input.Name)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("checking existing workflow %q: %w", input.Name, err)
	}
	if existing != nil {
		return nil, fmt.Errorf("workflow %q already exists: %w", input.Name, domain.ErrConflict)
	}

	statuses := make(map[string]domain.StatusConfig, len(input.Statuses))
	for n, sc := range input.Statuses {
		roles := make([]domain.StatusRole, len(sc.Roles))
		copy(roles, sc.Roles)
		statuses[n] = domain.StatusConfig{Roles: roles}
	}
	transitions := make([]domain.WorkflowTransition, len(input.Transitions))
	copy(transitions, input.Transitions)

	now := time.Now().UTC().Truncate(time.Millisecond)
	wf := &domain.Workflow{
		ID:          uuid.New(),
		Name:        input.Name,
		Statuses:    statuses,
		Transitions: transitions,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := domain.ValidateWorkflow(wf); err != nil {
		return nil, err
	}
	if err := s.workflowRepo.Create(ctx, wf); err != nil {
		return nil, fmt.Errorf("creating workflow %q: %w", input.Name, err)
	}
	return wf, nil
}

// Modify applies a mutation to an existing workflow under optimistic locking.
// Removed statuses cause referencing transitions to be pruned. The mutated
// workflow is re-validated before being written.
func (s *WorkflowService) Modify(ctx context.Context, input ModifyWorkflowInput) (*domain.Workflow, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: workflow name is empty", domain.ErrInvalidWorkflow)
	}

	wf, err := s.workflowRepo.GetByName(ctx, input.Name)
	if err != nil {
		return nil, err
	}
	if wf.Version != input.ExpectedVersion {
		return nil, fmt.Errorf("workflow %q: expected version %d, got %d: %w",
			input.Name, input.ExpectedVersion, wf.Version, domain.ErrConflict)
	}

	if wf.Statuses == nil {
		wf.Statuses = make(map[string]domain.StatusConfig)
	}

	removed := make(map[string]bool, len(input.RemoveStatuses))
	for _, name := range input.RemoveStatuses {
		delete(wf.Statuses, name)
		removed[name] = true
	}
	if len(removed) > 0 {
		kept := wf.Transitions[:0:0]
		for _, t := range wf.Transitions {
			if !removed[t.FromStatus] && !removed[t.ToStatus] {
				kept = append(kept, t)
			}
		}
		wf.Transitions = kept
	}

	for name, sc := range input.AddStatuses {
		if _, exists := wf.Statuses[name]; exists {
			return nil, fmt.Errorf("status %q already exists in workflow %q", name, input.Name)
		}
		roles := make([]domain.StatusRole, len(sc.Roles))
		copy(roles, sc.Roles)
		wf.Statuses[name] = domain.StatusConfig{Roles: roles}
	}

	for name, sc := range input.SetStatuses {
		roles := make([]domain.StatusRole, len(sc.Roles))
		copy(roles, sc.Roles)
		wf.Statuses[name] = domain.StatusConfig{Roles: roles}
	}

	for _, rm := range input.RemoveTransitions {
		kept := wf.Transitions[:0:0]
		for _, t := range wf.Transitions {
			if t.FromStatus != rm.FromStatus || t.ToStatus != rm.ToStatus {
				kept = append(kept, t)
			}
		}
		wf.Transitions = kept
	}

	wf.Transitions = append(wf.Transitions, input.AddTransitions...)

	if err := domain.ValidateWorkflow(wf); err != nil {
		return nil, err
	}

	if err := s.workflowRepo.Update(ctx, wf); err != nil {
		return nil, fmt.Errorf("updating workflow %q: %w", input.Name, err)
	}
	return wf, nil
}

// Delete removes a workflow with optimistic locking. Refuses to delete the
// built-in kanban workflow or any workflow currently referenced by a project.
// Project-reference is checked before the built-in guard so the error message
// matches the user's intent when both conditions hold.
func (s *WorkflowService) Delete(ctx context.Context, id uuid.UUID, expectedVersion int) error {
	count, err := s.projectRepo.CountProjectsByWorkflow(ctx, id)
	if err != nil {
		return fmt.Errorf("counting projects for workflow %s: %w", id, err)
	}
	if count > 0 {
		return fmt.Errorf("workflow %s referenced by %d project(s): %w", id, count, domain.ErrWorkflowInUse)
	}
	if id == uuid.Nil {
		return fmt.Errorf("kanban: %w", domain.ErrBuiltInWorkflow)
	}
	if err := s.workflowRepo.Delete(ctx, id, expectedVersion); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return fmt.Errorf("workflow %s: version conflict: %w", id, domain.ErrConflict)
		}
		return fmt.Errorf("deleting workflow %s: %w", id, err)
	}
	return nil
}
