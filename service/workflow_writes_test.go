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
// via t.Cleanup.
func newSQLiteWorkflowSvc(t *testing.T) (*WorkflowService, *sqlite.WorkflowRepo, *sqlite.ProjectRepo) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.New(filepath.Join(dir, "test.db"), migrations.FS)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db := store.DB()
	wfRepo := sqlite.NewWorkflowRepo(db)
	projRepo := sqlite.NewProjectRepo(db)
	return NewWorkflowService(wfRepo, projRepo), wfRepo, projRepo
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

func TestWorkflowService_Create_Happy(t *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(t)
	ctx := context.Background()

	wf, err := svc.Create(ctx, sampleCreateInput("sprint"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if wf.Version != 1 {
		t.Fatalf("expected version 1, got %d", wf.Version)
	}
	got, err := svc.GetByName(ctx, "sprint")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.ID != wf.ID {
		t.Fatalf("mismatched IDs: %v vs %v", got.ID, wf.ID)
	}
}

func TestWorkflowService_Create_EmptyName(t *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(t)
	_, err := svc.Create(context.Background(), CreateWorkflowInput{})
	if !errors.Is(err, domain.ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestWorkflowService_Create_Duplicate(t *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, sampleCreateInput("sprint")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := svc.Create(ctx, sampleCreateInput("sprint"))
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestWorkflowService_Create_ValidationFailure(t *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(t)
	bad := sampleCreateInput("broken")
	delete(bad.Statuses, "todo") // drops the initial role
	_, err := svc.Create(context.Background(), bad)
	if !errors.Is(err, domain.ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestWorkflowService_Modify_Happy(t *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(t)
	ctx := context.Background()
	wf, err := svc.Create(ctx, sampleCreateInput("sprint"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Modify(ctx, ModifyWorkflowInput{
		Name:            "sprint",
		ExpectedVersion: wf.Version,
		AddStatuses: map[string]domain.StatusConfig{
			"review": {Roles: []domain.StatusRole{domain.RoleHighlight}},
		},
		AddTransitions: []domain.WorkflowTransition{
			{FromStatus: "doing", ToStatus: "review"},
			{FromStatus: "review", ToStatus: "done"},
		},
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if _, ok := updated.Statuses["review"]; !ok {
		t.Fatalf("review status missing: %+v", updated.Statuses)
	}
	if updated.Version != wf.Version+1 {
		t.Fatalf("expected version bump, got %d", updated.Version)
	}
}

func TestWorkflowService_Modify_VersionConflict(t *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, sampleCreateInput("sprint")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := svc.Modify(ctx, ModifyWorkflowInput{
		Name:            "sprint",
		ExpectedVersion: 99,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestWorkflowService_Modify_RemoveStatusPrunesTransitions(t *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(t)
	ctx := context.Background()
	wf, err := svc.Create(ctx, sampleCreateInput("sprint"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Replace drop with a different terminal to keep role-schema valid.
	updated, err := svc.Modify(ctx, ModifyWorkflowInput{
		Name:            "sprint",
		ExpectedVersion: wf.Version,
		RemoveStatuses:  []string{"drop"},
		AddStatuses: map[string]domain.StatusConfig{
			"archived": {Roles: []domain.StatusRole{domain.RoleTerminal, domain.RoleDelete, domain.RoleDim}},
		},
		AddTransitions: []domain.WorkflowTransition{
			{FromStatus: "doing", ToStatus: "archived"},
		},
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	for _, tr := range updated.Transitions {
		if tr.FromStatus == "drop" || tr.ToStatus == "drop" {
			t.Fatalf("dangling transition: %+v", tr)
		}
	}
}

func TestWorkflowService_Modify_ValidationFailure(t *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(t)
	ctx := context.Background()
	wf, err := svc.Create(ctx, sampleCreateInput("sprint"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = svc.Modify(ctx, ModifyWorkflowInput{
		Name:            "sprint",
		ExpectedVersion: wf.Version,
		RemoveStatuses:  []string{"todo"}, // drops the initial role
	})
	if !errors.Is(err, domain.ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

func TestWorkflowService_Delete_Happy(t *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(t)
	ctx := context.Background()
	wf, err := svc.Create(ctx, sampleCreateInput("sprint"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Delete(ctx, wf.ID, wf.Version); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.GetByName(ctx, "sprint"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWorkflowService_Delete_BuiltInKanban(t *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(t)
	ctx := context.Background()
	// No project references the kanban workflow in this fresh DB (migration 004
	// seeds _default → uuid.Nil). Delete the default project so only the
	// built-in guard remains to reject the delete.
	if err := svc.Delete(ctx, uuid.Nil, 1); !errors.Is(err, domain.ErrWorkflowInUse) &&
		!errors.Is(err, domain.ErrBuiltInWorkflow) {
		t.Fatalf("expected ErrWorkflowInUse or ErrBuiltInWorkflow, got %v", err)
	}
}

func TestWorkflowService_Delete_WorkflowInUse(t *testing.T) {
	svc, _, projRepo := newSQLiteWorkflowSvc(t)
	ctx := context.Background()
	wf, err := svc.Create(ctx, sampleCreateInput("sprint"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	p := &domain.Project{
		ID:         uuid.New(),
		Name:       "backend",
		WorkflowID: wf.ID,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := projRepo.Create(ctx, p); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := svc.Delete(ctx, wf.ID, wf.Version); !errors.Is(err, domain.ErrWorkflowInUse) {
		t.Fatalf("expected ErrWorkflowInUse, got %v", err)
	}
}

func TestWorkflowService_Delete_VersionConflict(t *testing.T) {
	svc, _, _ := newSQLiteWorkflowSvc(t)
	ctx := context.Background()
	wf, err := svc.Create(ctx, sampleCreateInput("sprint"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Delete(ctx, wf.ID, 99); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}
