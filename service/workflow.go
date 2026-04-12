package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
)

// WorkflowService validates status transitions and provides read access
// to workflow definitions from config.
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

// GetTransitions returns all allowed transitions for the named workflow.
func (s *WorkflowService) GetTransitions(ctx context.Context, workflowName string) ([]domain.WorkflowTransition, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Transitions, nil
}

// List returns all workflows from config.
func (s *WorkflowService) List(ctx context.Context) ([]*domain.Workflow, error) {
	return s.workflowRepo.List(ctx)
}

// GetByName returns a single workflow by name.
// Returns domain.ErrNotFound if the workflow does not exist.
func (s *WorkflowService) GetByName(ctx context.Context, name string) (*domain.Workflow, error) {
	return s.workflowRepo.GetByName(ctx, name)
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
		if p.Workflow == name {
			projectIDs = append(projectIDs, p.ID)
		}
	}
	sort.Strings(projectIDs)
	return wf, projectIDs, nil
}
