package inmem_test

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/inmem"
)

func TestWorkflowRepository_GetByName(t *testing.T) {
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
		wf, err := repo.GetByName(ctx, "kanban")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if wf.Name != "kanban" {
			t.Errorf("expected Name 'kanban', got %q", wf.Name)
		}
		if len(wf.Statuses) != 4 {
			t.Errorf("expected 4 statuses, got %d", len(wf.Statuses))
		}
		if len(wf.Transitions) != 2 {
			t.Errorf("expected 2 transitions, got %d", len(wf.Transitions))
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := repo.GetByName(ctx, "nonexistent")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("returns defensive copy", func(t *testing.T) {
		wf1, _ := repo.GetByName(ctx, "kanban")
		wf1.Name = "mutated"
		wf2, _ := repo.GetByName(ctx, "kanban")
		if wf2.Name != "kanban" {
			t.Errorf("defensive copy failed: got %q", wf2.Name)
		}
	})
}

func TestWorkflowRepository_List(t *testing.T) {
	workflows := map[string]config.WorkflowConfig{
		"kanban":       {Statuses: []string{"pending", "active"}},
		"bug-tracking": {Statuses: []string{"open", "closed"}},
	}

	repo := inmem.NewWorkflowRepository(workflows)
	list, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(list))
	}
	if list[0].Name != "bug-tracking" || list[1].Name != "kanban" {
		t.Errorf("expected alphabetical order, got [%s, %s]", list[0].Name, list[1].Name)
	}
}
