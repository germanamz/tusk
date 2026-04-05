package inmem_test

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/inmem"
)

func TestWorkflowRepository_GetByProjectAndName(t *testing.T) {
	workflows := map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: []string{"pending", "active", "completed", "deleted"},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "active", To: "completed"},
			},
		},
	}

	repo := inmem.NewWorkflowRepository(workflows)
	ctx := context.Background()

	t.Run("existing workflow", func(t *testing.T) {
		wf, err := repo.GetByProjectAndName(ctx, "default", "kanban")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if wf.Name != "kanban" {
			t.Errorf("expected Name 'kanban', got %q", wf.Name)
		}
		if len(wf.Statuses) != 4 {
			t.Errorf("expected 4 statuses, got %d", len(wf.Statuses))
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := repo.GetByProjectAndName(ctx, "any", "nonexistent")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestWorkflowRepository_GetTransitions(t *testing.T) {
	workflows := map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: []string{"pending", "active"},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "active", To: "pending"},
			},
		},
	}

	repo := inmem.NewWorkflowRepository(workflows)
	ctx := context.Background()

	wf, _ := repo.GetByProjectAndName(ctx, "default", "kanban")
	transitions, err := repo.GetTransitions(ctx, wf.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(transitions))
	}
	if transitions[0].FromStatus != "pending" || transitions[0].ToStatus != "active" {
		t.Errorf("unexpected transition: %s -> %s", transitions[0].FromStatus, transitions[0].ToStatus)
	}
}

func TestWorkflowRepository_GetTransitions_NotFound(t *testing.T) {
	repo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{})
	ctx := context.Background()

	_, err := repo.GetTransitions(ctx, [16]byte{}) // zero UUID
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
