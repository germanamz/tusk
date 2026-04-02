package service

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// WorkflowService validates status transitions against project workflows.
type WorkflowService struct {
	workflowRepo repository.WorkflowRepository
}

// NewWorkflowService creates a new WorkflowService.
func NewWorkflowService(wr repository.WorkflowRepository) *WorkflowService {
	return &WorkflowService{workflowRepo: wr}
}

// IsTransitionAllowed checks whether a status transition is permitted by the
// workflow identified by projectID and workflowName.
func (s *WorkflowService) IsTransitionAllowed(ctx context.Context, projectID uuid.UUID, workflowName string, from string, to string) (bool, error) {
	wf, err := s.workflowRepo.GetByProjectAndName(ctx, projectID, workflowName)
	if err != nil {
		return false, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	transitions, err := s.workflowRepo.GetTransitions(ctx, wf.ID)
	if err != nil {
		return false, fmt.Errorf("loading transitions for workflow %q: %w", workflowName, err)
	}

	for _, t := range transitions {
		if t.FromStatus == from && t.ToStatus == to {
			return true, nil
		}
	}
	return false, nil
}

// GetStatuses returns the ordered list of valid statuses for the workflow
// identified by projectID and workflowName.
func (s *WorkflowService) GetStatuses(ctx context.Context, projectID uuid.UUID, workflowName string) ([]string, error) {
	wf, err := s.workflowRepo.GetByProjectAndName(ctx, projectID, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Statuses, nil
}
