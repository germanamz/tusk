package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

func newTestWorkflowRepo(t *testing.T) *sqlite.WorkflowRepo {
	t.Helper()
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return sqlite.NewWorkflowRepo(store.DB())
}

func sampleWorkflow(name string) *domain.Workflow {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return &domain.Workflow{
		ID:   uuid.New(),
		Name: name,
		Statuses: map[string]domain.StatusConfig{
			"pending": {Roles: []domain.StatusRole{domain.RoleInitial}},
			"active":  {Roles: []domain.StatusRole{domain.RoleStart}},
			"done":    {Roles: []domain.StatusRole{domain.RoleTerminal, domain.RoleDone}},
		},
		Transitions: []domain.WorkflowTransition{
			{FromStatus: "pending", ToStatus: "active"},
			{FromStatus: "active", ToStatus: "done"},
		},
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestWorkflowRepo_CreateAndGetByID(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, wf.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "sprint" {
		t.Errorf("got name %q, want %q", got.Name, "sprint")
	}
	if len(got.Statuses) != 3 {
		t.Errorf("got %d statuses, want 3", len(got.Statuses))
	}
	if len(got.Transitions) != 2 {
		t.Errorf("got %d transitions, want 2", len(got.Transitions))
	}
	if got.Version != 1 {
		t.Errorf("got version %d, want 1", got.Version)
	}
}

func TestWorkflowRepo_GetByID_NotFound(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByID: got %v, want ErrNotFound", err)
	}
}

func TestWorkflowRepo_GetByName_Seed(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	got, err := repo.GetByName(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.ID != uuid.Nil {
		t.Errorf("got ID %v, want uuid.Nil", got.ID)
	}
	if _, ok := got.Statuses["pending"]; !ok {
		t.Errorf("expected pending status in seed workflow")
	}
}

func TestWorkflowRepo_List_ContainsSeed(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	wfs, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(wfs) < 1 {
		t.Fatalf("List: want >=1 workflow, got %d", len(wfs))
	}
	found := false
	for _, w := range wfs {
		if w.Name == "kanban" {
			found = true
		}
	}
	if !found {
		t.Errorf("kanban seed not in list")
	}
}

func TestWorkflowRepo_CreateDuplicate(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	wf2 := sampleWorkflow("sprint")
	err := repo.Create(ctx, wf2)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second Create: got %v, want ErrConflict", err)
	}
}

func TestWorkflowRepo_Update_IncrementsVersion(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}

	wf.Statuses["review"] = domain.StatusConfig{Roles: []domain.StatusRole{domain.RoleHighlight}}
	if err := repo.Update(ctx, wf); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if wf.Version != 2 {
		t.Errorf("local version after Update: got %d, want 2", wf.Version)
	}

	got, err := repo.GetByID(ctx, wf.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("stored version: got %d, want 2", got.Version)
	}
	if _, ok := got.Statuses["review"]; !ok {
		t.Errorf("expected review status after update")
	}
}

func TestWorkflowRepo_Update_StaleVersion(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}
	stale := *wf
	if err := repo.Update(ctx, wf); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	err := repo.Update(ctx, &stale)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale Update: got %v, want ErrConflict", err)
	}
}

func TestWorkflowRepo_Update_NotFound(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ghost := sampleWorkflow("ghost")
	err := repo.Update(context.Background(), ghost)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update missing row: got %v, want ErrNotFound", err)
	}
}

func TestWorkflowRepo_Delete(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, wf.ID, wf.Version); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := repo.GetByID(ctx, wf.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("after Delete, GetByID: got %v, want ErrNotFound", err)
	}
}

func TestWorkflowRepo_Delete_StaleVersion(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Update(ctx, wf); err != nil {
		t.Fatalf("Update: %v", err)
	}
	err := repo.Delete(ctx, wf.ID, 1)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale Delete: got %v, want ErrConflict", err)
	}
}

func TestWorkflowRepo_Delete_NotFound(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	err := repo.Delete(context.Background(), uuid.New(), 1)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete missing: got %v, want ErrNotFound", err)
	}
}
