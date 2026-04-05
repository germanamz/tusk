package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/inmem"
)

// defaultProjectID matches the seeded default project in the migration.
const defaultProjectID = "default"

// testWorkflowService creates a WorkflowService backed by an in-memory
// workflow repository with the default kanban workflow.
func testWorkflowService(t *testing.T) *WorkflowService {
	t.Helper()
	workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: []string{"pending", "active", "completed", "deleted"},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "pending", To: "deleted"},
				{From: "active", To: "completed"},
				{From: "active", To: "pending"},
				{From: "active", To: "deleted"},
				{From: "completed", To: "pending"},
			},
		},
	})
	return NewWorkflowService(workflowRepo)
}

func TestIsTransitionAllowed_Allowed(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	allowed, err := svc.IsTransitionAllowed(ctx, defaultProjectID, "kanban", "pending", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected pending→active to be allowed")
	}
}

func TestIsTransitionAllowed_Disallowed(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	allowed, err := svc.IsTransitionAllowed(ctx, defaultProjectID, "kanban", "pending", "completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected pending→completed to be disallowed")
	}
}

func TestIsTransitionAllowed_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	_, err := svc.IsTransitionAllowed(ctx, defaultProjectID, "nonexistent", "pending", "active")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetStatuses(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	statuses, err := svc.GetStatuses(ctx, defaultProjectID, "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"pending", "active", "completed", "deleted"}
	if len(statuses) != len(expected) {
		t.Fatalf("expected %d statuses, got %d", len(expected), len(statuses))
	}
	for i, s := range statuses {
		if s != expected[i] {
			t.Fatalf("status[%d]: expected %q, got %q", i, expected[i], s)
		}
	}
}

func TestGetStatuses_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowService(t)
	ctx := context.Background()

	_, err := svc.GetStatuses(ctx, defaultProjectID, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}
