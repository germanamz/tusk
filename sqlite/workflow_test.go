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

func newTestWorkflowRepo(test *testing.T) *sqlite.WorkflowRepo {
	test.Helper()
	store, err := sqlite.New(test.TempDir()+"/test.db", migrations.FS)

	if err != nil {
		test.Fatalf("opening test db: %v", err)
	}

	test.Cleanup(func() { store.Close() })

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

func TestWorkflowRepo_CreateAndGetByID(test *testing.T) {
	repo := newTestWorkflowRepo(test)
	ctx := context.Background()

	workflow := sampleWorkflow("sprint")

	if err := repo.Create(ctx, workflow); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, workflow.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Name != "sprint" {
		test.Errorf("got name %q, want %q", got.Name, "sprint")
	}

	if len(got.Statuses) != 3 {
		test.Errorf("got %d statuses, want 3", len(got.Statuses))
	}

	if len(got.Transitions) != 2 {
		test.Errorf("got %d transitions, want 2", len(got.Transitions))
	}

	if got.Version != 1 {
		test.Errorf("got version %d, want 1", got.Version)
	}
}

func TestWorkflowRepo_GetByID_NotFound(test *testing.T) {
	repo := newTestWorkflowRepo(test)
	_, err := repo.GetByID(context.Background(), uuid.New())

	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("GetByID: got %v, want ErrNotFound", err)
	}
}

func TestWorkflowRepo_GetByName_Seed(test *testing.T) {
	repo := newTestWorkflowRepo(test)
	got, err := repo.GetByName(context.Background(), "kanban")

	if err != nil {
		test.Fatalf("GetByName: %v", err)
	}

	if got.ID != uuid.Nil {
		test.Errorf("got ID %v, want uuid.Nil", got.ID)
	}

	if _, ok := got.Statuses["pending"]; !ok {
		test.Errorf("expected pending status in seed workflow")
	}
}

func TestWorkflowRepo_List_ContainsSeed(test *testing.T) {
	repo := newTestWorkflowRepo(test)
	workflows, err := repo.List(context.Background())

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(workflows) < 1 {
		test.Fatalf("List: want >=1 workflow, got %d", len(workflows))
	}

	found := false

	for _, workflow := range workflows {
		if workflow.Name == "kanban" {
			found = true
		}
	}

	if !found {
		test.Errorf("kanban seed not in list")
	}
}

func TestWorkflowRepo_CreateDuplicate(test *testing.T) {
	repo := newTestWorkflowRepo(test)
	ctx := context.Background()

	workflow := sampleWorkflow("sprint")

	if err := repo.Create(ctx, workflow); err != nil {
		test.Fatalf("first Create: %v", err)
	}

	workflow2 := sampleWorkflow("sprint")
	err := repo.Create(ctx, workflow2)

	if !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("second Create: got %v, want ErrConflict", err)
	}
}

func TestWorkflowRepo_Update_IncrementsVersion(test *testing.T) {
	repo := newTestWorkflowRepo(test)
	ctx := context.Background()

	workflow := sampleWorkflow("sprint")

	if err := repo.Create(ctx, workflow); err != nil {
		test.Fatalf("Create: %v", err)
	}

	workflow.Statuses["review"] = domain.StatusConfig{Roles: []domain.StatusRole{domain.RoleHighlight}}

	if err := repo.Update(ctx, workflow); err != nil {
		test.Fatalf("Update: %v", err)
	}

	if workflow.Version != 2 {
		test.Errorf("local version after Update: got %d, want 2", workflow.Version)
	}

	got, err := repo.GetByID(ctx, workflow.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Version != 2 {
		test.Errorf("stored version: got %d, want 2", got.Version)
	}

	if _, ok := got.Statuses["review"]; !ok {
		test.Errorf("expected review status after update")
	}
}

func TestWorkflowRepo_Update_StaleVersion(test *testing.T) {
	repo := newTestWorkflowRepo(test)
	ctx := context.Background()

	workflow := sampleWorkflow("sprint")

	if err := repo.Create(ctx, workflow); err != nil {
		test.Fatalf("Create: %v", err)
	}

	stale := *workflow

	if err := repo.Update(ctx, workflow); err != nil {
		test.Fatalf("first Update: %v", err)
	}

	err := repo.Update(ctx, &stale)

	if !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("stale Update: got %v, want ErrConflict", err)
	}
}

func TestWorkflowRepo_Update_NotFound(test *testing.T) {
	repo := newTestWorkflowRepo(test)
	ghost := sampleWorkflow("ghost")
	err := repo.Update(context.Background(), ghost)

	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("Update missing row: got %v, want ErrNotFound", err)
	}
}

func TestWorkflowRepo_Delete(test *testing.T) {
	repo := newTestWorkflowRepo(test)
	ctx := context.Background()

	workflow := sampleWorkflow("sprint")

	if err := repo.Create(ctx, workflow); err != nil {
		test.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, workflow.ID, workflow.Version); err != nil {
		test.Fatalf("Delete: %v", err)
	}

	_, err := repo.GetByID(ctx, workflow.ID)

	if !errors.Is(err, domain.ErrNotFound) {
		test.Errorf("after Delete, GetByID: got %v, want ErrNotFound", err)
	}
}

func TestWorkflowRepo_Delete_StaleVersion(test *testing.T) {
	repo := newTestWorkflowRepo(test)
	ctx := context.Background()

	workflow := sampleWorkflow("sprint")

	if err := repo.Create(ctx, workflow); err != nil {
		test.Fatalf("Create: %v", err)
	}

	if err := repo.Update(ctx, workflow); err != nil {
		test.Fatalf("Update: %v", err)
	}

	err := repo.Delete(ctx, workflow.ID, 1)

	if !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("stale Delete: got %v, want ErrConflict", err)
	}
}

func TestWorkflowRepo_Delete_NotFound(test *testing.T) {
	repo := newTestWorkflowRepo(test)
	err := repo.Delete(context.Background(), uuid.New(), 1)

	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("Delete missing: got %v, want ErrNotFound", err)
	}
}
