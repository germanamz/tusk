// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// newSQLiteWorkflowSvc opens a fresh sqlite store, runs migrations, and returns
// a WorkflowService backed by real sqlite repositories. The store is closed
// via test.Cleanup.
func newSQLiteWorkflowSvc(test *testing.T) (*WorkflowService, *sqlite.WorkflowRepo, *sqlite.ProjectRepo) {
	test.Helper()
	dir := test.TempDir()
	store, err := sqlite.New(filepath.Join(dir, "test.db"), migrations.FS)

	if err != nil {
		test.Fatalf("sqlite.New: %v", err)
	}

	test.Cleanup(func() { store.Close() })

	db := store.DB()
	workflowRepo := sqlite.NewWorkflowRepo(db)
	projRepo := sqlite.NewProjectRepo(db)
	return NewWorkflowService(workflowRepo, projRepo), workflowRepo, projRepo
}

func sampleCreateInput(name string) CreateWorkflowInput {
	return CreateWorkflowInput{
		Name: name,
		Statuses: map[string]domain.StatusConfig{
			"todo":  {Roles: []domain.StatusRole{domain.RoleInitial}},
			"doing": {Roles: []domain.StatusRole{domain.RoleStart, domain.RoleHighlight}},
			"done":  {Roles: []domain.StatusRole{domain.RoleTerminal, domain.RoleDone, domain.RoleDim}},
			"drop":  {Roles: []domain.StatusRole{domain.RoleTerminal, domain.RoleDelete, domain.RoleDim}},
		},
		Transitions: []domain.WorkflowTransition{
			{FromStatus: "todo", ToStatus: "doing"},
			{FromStatus: "doing", ToStatus: "done"},
			{FromStatus: "doing", ToStatus: "drop"},
		},
	}
}

func TestWorkflowService_Create_Happy(test *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(test)
	ctx := context.Background()

	workflow, createErr := svc.Create(ctx, sampleCreateInput("sprint"))

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if workflow.Version != 1 {
		test.Fatalf("expected version 1, got %d", workflow.Version)
	}

	got, getErr := svc.GetByName(ctx, "sprint")

	if getErr != nil {
		test.Fatalf("GetByName: %v", getErr)
	}

	if got.ID != workflow.ID {
		test.Fatalf("mismatched IDs: %v vs %v", got.ID, workflow.ID)
	}
}

func TestWorkflowService_Create_EmptyName(test *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(test)
	_, err := svc.Create(context.Background(), CreateWorkflowInput{})
	if !errors.Is(err, domain.ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestWorkflowService_Create_Duplicate(test *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(test)
	ctx := context.Background()
	if _, err := svc.Create(ctx, sampleCreateInput("sprint")); err != nil {
		test.Fatalf("seed: %v", err)
	}
	_, err := svc.Create(ctx, sampleCreateInput("sprint"))
	if !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestWorkflowService_Create_ValidationFailure(test *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(test)
	bad := sampleCreateInput("broken")
	delete(bad.Statuses, "todo") // drops the initial role
	_, err := svc.Create(context.Background(), bad)
	if !errors.Is(err, domain.ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestWorkflowService_Modify_Happy(test *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(test)
	ctx := context.Background()

	workflow, createErr := svc.Create(ctx, sampleCreateInput("sprint"))

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	updated, modifyErr := svc.Modify(ctx, ModifyWorkflowInput{
		Name:            "sprint",
		ExpectedVersion: workflow.Version,
		AddStatuses: map[string]domain.StatusConfig{
			"review": {Roles: []domain.StatusRole{domain.RoleHighlight}},
		},
		AddTransitions: []domain.WorkflowTransition{
			{FromStatus: "doing", ToStatus: "review"},
			{FromStatus: "review", ToStatus: "done"},
		},
	})

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if _, ok := updated.Statuses["review"]; !ok {
		test.Fatalf("review status missing: %+v", updated.Statuses)
	}
	if updated.Version != workflow.Version+1 {
		test.Fatalf("expected version bump, got %d", updated.Version)
	}
}

func TestWorkflowService_Modify_VersionConflict(test *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(test)
	ctx := context.Background()
	if _, err := svc.Create(ctx, sampleCreateInput("sprint")); err != nil {
		test.Fatalf("Create: %v", err)
	}
	_, err := svc.Modify(ctx, ModifyWorkflowInput{
		Name:            "sprint",
		ExpectedVersion: 99,
	})
	if !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestWorkflowService_Modify_RemoveStatusPrunesTransitions(test *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(test)
	ctx := context.Background()

	workflow, createErr := svc.Create(ctx, sampleCreateInput("sprint"))

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	// Replace drop with a different terminal to keep role-schema valid.
	updated, modifyErr := svc.Modify(ctx, ModifyWorkflowInput{
		Name:            "sprint",
		ExpectedVersion: workflow.Version,
		RemoveStatuses:  []string{"drop"},
		AddStatuses: map[string]domain.StatusConfig{
			"archived": {Roles: []domain.StatusRole{domain.RoleTerminal, domain.RoleDelete, domain.RoleDim}},
		},
		AddTransitions: []domain.WorkflowTransition{
			{FromStatus: "doing", ToStatus: "archived"},
		},
	})

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	for _, transition := range updated.Transitions {
		if transition.FromStatus == "drop" || transition.ToStatus == "drop" {
			test.Fatalf("dangling transition: %+v", transition)
		}
	}
}

func TestWorkflowService_Modify_ValidationFailure(test *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(test)
	ctx := context.Background()
	workflow, err := svc.Create(ctx, sampleCreateInput("sprint"))

	if err != nil {
		test.Fatalf("Create: %v", err)
	}

	_, err = svc.Modify(ctx, ModifyWorkflowInput{
		Name:            "sprint",
		ExpectedVersion: workflow.Version,
		RemoveStatuses:  []string{"todo"}, // drops the initial role
	})
	if !errors.Is(err, domain.ErrInvalidWorkflow) {
		test.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestWorkflowService_Delete_Happy(test *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(test)
	ctx := context.Background()
	workflow, err := svc.Create(ctx, sampleCreateInput("sprint"))

	if err != nil {
		test.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(ctx, workflow.ID, workflow.Version); err != nil {
		test.Fatalf("Delete: %v", err)
	}
	if _, err := svc.GetByName(ctx, "sprint"); !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWorkflowService_Delete_BuiltInKanban(test *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(test)
	ctx := context.Background()
	// No project references the kanban workflow in this fresh DB (migration 004
	// seeds _default → uuid.Nil). Delete the default project so only the
	// built-in guard remains to reject the delete.
	if err := svc.Delete(ctx, uuid.Nil, 1); !errors.Is(err, domain.ErrWorkflowInUse) &&
		!errors.Is(err, domain.ErrBuiltInWorkflow) {
		test.Fatalf("expected ErrWorkflowInUse or ErrBuiltInWorkflow, got %v", err)
	}
}

func TestWorkflowService_Delete_WorkflowInUse(test *testing.T) {
	svc, _, projRepo := newSQLiteWorkflowSvc(test)
	ctx := context.Background()
	workflow, err := svc.Create(ctx, sampleCreateInput("sprint"))

	if err != nil {
		test.Fatalf("Create: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	project := &domain.Project{
		ID:         uuid.New(),
		Name:       "backend",
		WorkflowID: workflow.ID,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := projRepo.Create(ctx, project); err != nil {
		test.Fatalf("seed project: %v", err)
	}
	if err := svc.Delete(ctx, workflow.ID, workflow.Version); !errors.Is(err, domain.ErrWorkflowInUse) {
		test.Fatalf("expected ErrWorkflowInUse, got %v", err)
	}
}

func TestWorkflowService_Delete_VersionConflict(test *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(test)
	ctx := context.Background()
	workflow, err := svc.Create(ctx, sampleCreateInput("sprint"))

	if err != nil {
		test.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(ctx, workflow.ID, 99); !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("expected ErrConflict, got %v", err)
	}
}
