package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
)

func testWorkflowEnv(test *testing.T) *WorkflowService {
	test.Helper()
	_, projRepo, workflowRepo := sqlitetest.NewStore(test)
	sqlitetest.SeedProject(test, projRepo, "backend")
	return NewWorkflowService(workflowRepo, projRepo)
}

func TestIsTransitionAllowed_Allowed(test *testing.T) {
	svc := testWorkflowEnv(test)
	allowed, err := svc.IsTransitionAllowed(context.Background(), "kanban", "pending", "active")

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if !allowed {
		test.Fatal("expected pending->active to be allowed")
	}
}

func TestIsTransitionAllowed_Disallowed(test *testing.T) {
	svc := testWorkflowEnv(test)
	allowed, err := svc.IsTransitionAllowed(context.Background(), "kanban", "pending", "completed")

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if allowed {
		test.Fatal("expected pending->completed to be disallowed")
	}
}

func TestIsTransitionAllowed_WorkflowNotFound(test *testing.T) {
	svc := testWorkflowEnv(test)
	_, err := svc.IsTransitionAllowed(context.Background(), "nonexistent", "pending", "active")
	if err == nil {
		test.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetStatuses(test *testing.T) {
	svc := testWorkflowEnv(test)
	statuses, err := svc.GetStatuses(context.Background(), "kanban")

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	// StatusNames() returns sorted names.
	expected := []string{"active", "completed", "deleted", "pending"}
	if len(statuses) != len(expected) {
		test.Fatalf("expected %d statuses, got %d", len(expected), len(statuses))
	}
	for index, status := range statuses {
		if status != expected[index] {
			test.Fatalf("status[%d]: expected %q, got %q", index, expected[index], status)
		}
	}
}

func TestGetStatuses_WorkflowNotFound(test *testing.T) {
	svc := testWorkflowEnv(test)
	_, err := svc.GetStatuses(context.Background(), "nonexistent")
	if err == nil {
		test.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetTransitions(test *testing.T) {
	svc := testWorkflowEnv(test)
	transitions, err := svc.GetTransitions(context.Background(), "kanban")

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if len(transitions) != 6 {
		test.Fatalf("expected 6 transitions, got %d", len(transitions))
	}
}

func TestWorkflowList(test *testing.T) {
	svc := testWorkflowEnv(test)
	workflows, err := svc.List(context.Background())

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if len(workflows) != 1 || workflows[0].Name != "kanban" {
		test.Fatalf("expected [kanban], got %v", workflows)
	}
}

func TestGetByName(test *testing.T) {
	svc := testWorkflowEnv(test)
	workflow, err := svc.GetByName(context.Background(), "kanban")

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if workflow.Name != "kanban" {
		test.Fatalf("expected 'kanban', got %q", workflow.Name)
	}
}

func TestGetByName_NotFound(test *testing.T) {
	svc := testWorkflowEnv(test)
	_, err := svc.GetByName(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetWorkflowWithProjects(test *testing.T) {
	svc := testWorkflowEnv(test)
	workflow, projectIDs, err := svc.GetWorkflowWithProjects(context.Background(), "kanban")

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if workflow.Name != "kanban" {
		test.Fatalf("expected 'kanban', got %q", workflow.Name)
	}
	if len(projectIDs) != 2 || projectIDs[0] != "backend" || projectIDs[1] != "default" {
		test.Fatalf("expected [backend, default], got %v", projectIDs)
	}
}

func TestGetWorkflowWithProjects_NotFound(test *testing.T) {
	svc := testWorkflowEnv(test)
	_, _, err := svc.GetWorkflowWithProjects(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func roleWorkflowEnv(test *testing.T) *WorkflowService {
	test.Helper()
	_, projRepo, workflowRepo := sqlitetest.NewStore(test)
	return NewWorkflowService(workflowRepo, projRepo)
}

func TestGetStatusByRole(test *testing.T) {
	svc := roleWorkflowEnv(test)
	ctx := context.Background()

	tests := []struct {
		role     domain.StatusRole
		expected string
	}{
		{domain.RoleInitial, "pending"},
		{domain.RoleStart, "active"},
		{domain.RoleDone, "completed"},
		{domain.RoleDelete, "deleted"},
	}

	for _, tt := range tests {
		test.Run(string(tt.role), func(test *testing.T) {
			name, err := svc.GetStatusByRole(ctx, "kanban", tt.role)

			if err != nil {
				test.Fatalf("unexpected error: %v", err)
			}

			if name != tt.expected {
				test.Fatalf("expected %q, got %q", tt.expected, name)
			}
		})
	}
}

func TestGetStatusByRole_UnknownWorkflow(test *testing.T) {
	svc := roleWorkflowEnv(test)
	_, err := svc.GetStatusByRole(context.Background(), "nonexistent", domain.RoleInitial)
	if err == nil {
		test.Fatal("expected error for unknown workflow")
	}
}

func TestGetStatusByRole_NoMatchingRole(test *testing.T) {
	svc := roleWorkflowEnv(test)
	_, err := svc.GetStatusByRole(context.Background(), "kanban", "nonexistent")
	if err == nil {
		test.Fatal("expected error for nonexistent role")
	}
}

func TestGetNonTerminalStatuses(test *testing.T) {
	svc := roleWorkflowEnv(test)
	statuses, err := svc.GetNonTerminalStatuses(context.Background(), "kanban")

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"active", "pending"}
	if len(statuses) != len(expected) {
		test.Fatalf("expected %d non-terminal statuses, got %d: %v", len(expected), len(statuses), statuses)
	}
	for index, status := range statuses {
		if status != expected[index] {
			test.Fatalf("index %d: expected %q, got %q", index, expected[index], status)
		}
	}
}

func TestGetDeleteStatus(test *testing.T) {
	svc := roleWorkflowEnv(test)
	name, err := svc.GetDeleteStatus(context.Background(), "kanban")

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if name != "deleted" {
		test.Fatalf("expected %q, got %q", "deleted", name)
	}
}

func TestWorkflowService_GetByID(test *testing.T) {
	svc := testWorkflowEnv(test)
	ctx := context.Background()

	byName, byNameErr := svc.GetByName(ctx, "kanban")

	if byNameErr != nil {
		test.Fatalf("GetByName: %v", byNameErr)
	}

	got, byIDErr := svc.GetByID(ctx, byName.ID)

	if byIDErr != nil {
		test.Fatalf("GetByID: %v", byIDErr)
	}

	if got.Name != "kanban" {
		test.Errorf("got name %q, want kanban", got.Name)
	}
}
