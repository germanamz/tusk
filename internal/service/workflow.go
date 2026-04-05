package service

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
)

// WorkflowService validates status transitions against workflow definitions.
type WorkflowService struct {
	workflowRepo repository.WorkflowRepository
}

// NewWorkflowService creates a new WorkflowService.
func NewWorkflowService(wr repository.WorkflowRepository) *WorkflowService {
	return &WorkflowService{workflowRepo: wr}
}

// IsTransitionAllowed checks whether a status transition is permitted
// by the named workflow.
// Note: projectID is accepted for backward compatibility but ignored.
// Phase 4 removes it from the signature.
func (s *WorkflowService) IsTransitionAllowed(ctx context.Context, projectID string, workflowName string, from string, to string) (bool, error) {
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
// Note: projectID is accepted for backward compatibility but ignored.
func (s *WorkflowService) GetStatuses(ctx context.Context, projectID string, workflowName string) ([]string, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Statuses, nil
}

// GetTransitions returns all allowed transitions for the named workflow.
// Note: projectID is accepted for backward compatibility but ignored.
// Return type is []domain.WorkflowTransition (value slice, not pointer slice).
// Callers that iterate and access fields work identically with both types.
func (s *WorkflowService) GetTransitions(ctx context.Context, projectID string, workflowName string) ([]domain.WorkflowTransition, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Transitions, nil
}
