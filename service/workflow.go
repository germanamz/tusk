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
func (service *WorkflowService) IsTransitionAllowed(ctx context.Context, workflowName string, from string, to string) (bool, error) {
	workflow, err := service.workflowRepo.GetByName(ctx, workflowName)

	if err != nil {
		return false, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	for _, transition := range workflow.Transitions {
		if transition.FromStatus == from && transition.ToStatus == to {
			return true, nil
		}
	}
	return false, nil
}

// GetStatuses returns the ordered list of valid statuses for the named workflow.
func (service *WorkflowService) GetStatuses(ctx context.Context, workflowName string) ([]string, error) {
	workflow, err := service.workflowRepo.GetByName(ctx, workflowName)

	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	return workflow.StatusNames(), nil
}

// GetStatusByRole returns the name of the status with the given role in the named workflow.
// Returns an error if the workflow doesn't exist or no status has the role.
func (service *WorkflowService) GetStatusByRole(ctx context.Context, workflowName string, role domain.StatusRole) (string, error) {
	workflow, err := service.workflowRepo.GetByName(ctx, workflowName)

	if err != nil {
		return "", fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	name, ok := workflow.StatusByRole(role)
	if !ok {
		return "", fmt.Errorf("workflow %q has no status with role %q", workflowName, role)
	}
	return name, nil
}

// GetStatusRoles returns the roles attached to the named status in the named
// workflow. Returns an empty slice if the status exists but has no roles, and
// an error if the workflow or status does not exist.
func (service *WorkflowService) GetStatusRoles(ctx context.Context, workflowName, status string) ([]string, error) {
	workflow, err := service.workflowRepo.GetByName(ctx, workflowName)

	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	statusConfig, ok := workflow.Statuses[status]
	if !ok {
		return nil, fmt.Errorf("workflow %q has no status %q", workflowName, status)
	}
	roles := make([]string, 0, len(statusConfig.Roles))
	for _, role := range statusConfig.Roles {
		roles = append(roles, string(role))
	}
	return roles, nil
}

// GetNonTerminalStatuses returns status names without the terminal role, sorted.
func (service *WorkflowService) GetNonTerminalStatuses(ctx context.Context, workflowName string) ([]string, error) {
	workflow, err := service.workflowRepo.GetByName(ctx, workflowName)

	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	return workflow.NonTerminalStatuses(), nil
}

// GetDeleteStatus returns the name of the status with the delete role.
func (service *WorkflowService) GetDeleteStatus(ctx context.Context, workflowName string) (string, error) {
	return service.GetStatusByRole(ctx, workflowName, domain.RoleDelete)
}

// GetTransitions returns all allowed transitions for the named workflow.
func (service *WorkflowService) GetTransitions(ctx context.Context, workflowName string) ([]domain.WorkflowTransition, error) {
	workflow, err := service.workflowRepo.GetByName(ctx, workflowName)

	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	return workflow.Transitions, nil
}

// List returns all workflows from the repository.
func (service *WorkflowService) List(ctx context.Context) ([]*domain.Workflow, error) {
	return service.workflowRepo.List(ctx)
}

// GetByName returns a single workflow by name.
// Returns domain.ErrNotFound if the workflow does not exist.
func (service *WorkflowService) GetByName(ctx context.Context, name string) (*domain.Workflow, error) {
	return service.workflowRepo.GetByName(ctx, name)
}

// GetByID returns a single workflow by typed UUID.
// Returns domain.ErrNotFound if the workflow does not exist.
func (service *WorkflowService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Workflow, error) {
	return service.workflowRepo.GetByID(ctx, id)
}

// GetWorkflowWithProjects returns a workflow and the sorted list of project IDs
// that reference it. Returns domain.ErrNotFound if the workflow does not exist.
func (service *WorkflowService) GetWorkflowWithProjects(ctx context.Context, name string) (*domain.Workflow, []string, error) {
	workflow, lookupErr := service.workflowRepo.GetByName(ctx, name)

	if lookupErr != nil {
		return nil, nil, lookupErr
	}

	projects, listErr := service.projectRepo.List(ctx)

	if listErr != nil {
		return nil, nil, fmt.Errorf("listing projects: %w", listErr)
	}

	var projectIDs []string
	for _, project := range projects {
		if project.WorkflowID == workflow.ID {
			projectIDs = append(projectIDs, project.Name)
		}
	}
	sort.Strings(projectIDs)
	return workflow, projectIDs, nil
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
func (service *WorkflowService) Create(ctx context.Context, input CreateWorkflowInput) (*domain.Workflow, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: workflow name is empty", domain.ErrInvalidWorkflow)
	}

	existing, lookupErr := service.workflowRepo.GetByName(ctx, input.Name)

	if lookupErr != nil && !errors.Is(lookupErr, domain.ErrNotFound) {
		return nil, fmt.Errorf("checking existing workflow %q: %w", input.Name, lookupErr)
	}

	if existing != nil {
		return nil, fmt.Errorf("workflow %q already exists: %w", input.Name, domain.ErrConflict)
	}

	statuses := make(map[string]domain.StatusConfig, len(input.Statuses))
	for statusName, statusConfig := range input.Statuses {
		roles := make([]domain.StatusRole, len(statusConfig.Roles))
		copy(roles, statusConfig.Roles)
		statuses[statusName] = domain.StatusConfig{Roles: roles}
	}
	transitions := make([]domain.WorkflowTransition, len(input.Transitions))
	copy(transitions, input.Transitions)

	now := time.Now().UTC().Truncate(time.Millisecond)
	workflow := &domain.Workflow{
		ID:          uuid.New(),
		Name:        input.Name,
		Statuses:    statuses,
		Transitions: transitions,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if validateErr := domain.ValidateWorkflow(workflow); validateErr != nil {
		return nil, validateErr
	}

	if createErr := service.workflowRepo.Create(ctx, workflow); createErr != nil {
		return nil, fmt.Errorf("creating workflow %q: %w", input.Name, createErr)
	}

	return workflow, nil
}

// Modify applies a mutation to an existing workflow under optimistic locking.
// Removed statuses cause referencing transitions to be pruned. The mutated
// workflow is re-validated before being written.
func (service *WorkflowService) Modify(ctx context.Context, input ModifyWorkflowInput) (*domain.Workflow, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: workflow name is empty", domain.ErrInvalidWorkflow)
	}

	workflow, err := service.workflowRepo.GetByName(ctx, input.Name)

	if err != nil {
		return nil, err
	}

	if workflow.Version != input.ExpectedVersion {
		return nil, fmt.Errorf("workflow %q: expected version %d, got %d: %w",
			input.Name, input.ExpectedVersion, workflow.Version, domain.ErrConflict)
	}

	if workflow.Statuses == nil {
		workflow.Statuses = make(map[string]domain.StatusConfig)
	}

	removed := make(map[string]bool, len(input.RemoveStatuses))
	for _, name := range input.RemoveStatuses {
		delete(workflow.Statuses, name)
		removed[name] = true
	}
	if len(removed) > 0 {
		kept := workflow.Transitions[:0:0]
		for _, transition := range workflow.Transitions {
			if !removed[transition.FromStatus] && !removed[transition.ToStatus] {
				kept = append(kept, transition)
			}
		}
		workflow.Transitions = kept
	}

	for name, statusConfig := range input.AddStatuses {
		if _, exists := workflow.Statuses[name]; exists {
			return nil, fmt.Errorf("status %q already exists in workflow %q", name, input.Name)
		}
		roles := make([]domain.StatusRole, len(statusConfig.Roles))
		copy(roles, statusConfig.Roles)
		workflow.Statuses[name] = domain.StatusConfig{Roles: roles}
	}

	for name, statusConfig := range input.SetStatuses {
		roles := make([]domain.StatusRole, len(statusConfig.Roles))
		copy(roles, statusConfig.Roles)
		workflow.Statuses[name] = domain.StatusConfig{Roles: roles}
	}

	for _, remove := range input.RemoveTransitions {
		kept := workflow.Transitions[:0:0]
		for _, transition := range workflow.Transitions {
			if transition.FromStatus != remove.FromStatus || transition.ToStatus != remove.ToStatus {
				kept = append(kept, transition)
			}
		}
		workflow.Transitions = kept
	}

	workflow.Transitions = append(workflow.Transitions, input.AddTransitions...)

	if validateErr := domain.ValidateWorkflow(workflow); validateErr != nil {
		return nil, validateErr
	}

	if updateErr := service.workflowRepo.Update(ctx, workflow); updateErr != nil {
		return nil, fmt.Errorf("updating workflow %q: %w", input.Name, updateErr)
	}

	return workflow, nil
}

// Delete removes a workflow with optimistic locking. Refuses to delete the
// built-in kanban workflow or any workflow currently referenced by a project.
// Project-reference is checked before the built-in guard so the error message
// matches the user's intent when both conditions hold.
func (service *WorkflowService) Delete(ctx context.Context, id uuid.UUID, expectedVersion int) error {
	count, err := service.projectRepo.CountProjectsByWorkflow(ctx, id)

	if err != nil {
		return fmt.Errorf("counting projects for workflow %s: %w", id, err)
	}

	if count > 0 {
		return fmt.Errorf("workflow %s referenced by %d project(s): %w", id, count, domain.ErrWorkflowInUse)
	}
	if id == domain.KanbanWorkflowUUID {
		return fmt.Errorf("kanban: %w", domain.ErrBuiltInWorkflow)
	}

	if deleteErr := service.workflowRepo.Delete(ctx, id, expectedVersion); deleteErr != nil {
		if errors.Is(deleteErr, domain.ErrConflict) {
			return fmt.Errorf("workflow %s: version conflict: %w", id, domain.ErrConflict)
		}
		return fmt.Errorf("deleting workflow %s: %w", id, deleteErr)
	}

	return nil
}
