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

var defaultUUID = uuid.MustParse("00000000-0000-0000-0000-000000000000")

func newTestProjectRepo(t *testing.T) *sqlite.ProjectRepo {
	t.Helper()
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return sqlite.NewProjectRepo(store.DB())
}

func sampleProject(name string) *domain.Project {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return &domain.Project{
		ID:         uuid.New(),
		Name:       name,
		WorkflowID: defaultUUID,
		Workflow:   "kanban",
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestProjectRepo_CreateAndGetByID(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()

	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "backend" {
		t.Errorf("got name %q, want %q", got.Name, "backend")
	}
	if got.WorkflowID != defaultUUID {
		t.Errorf("got workflow_id %v, want kanban UUID", got.WorkflowID)
	}
}

func TestProjectRepo_GetByName_Seed(t *testing.T) {
	repo := newTestProjectRepo(t)
	got, err := repo.GetByName(context.Background(), "_default")
	if err != nil {
		t.Fatalf("GetByName _default: %v", err)
	}
	if got.ID != defaultUUID {
		t.Errorf("got ID %v, want defaultUUID", got.ID)
	}
}

func TestProjectRepo_GetByID_NotFound(t *testing.T) {
	repo := newTestProjectRepo(t)
	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByID: got %v, want ErrNotFound", err)
	}
}

func TestProjectRepo_List_ContainsSeed(t *testing.T) {
	repo := newTestProjectRepo(t)
	ps, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ps) < 1 {
		t.Fatalf("want >= 1 project, got %d", len(ps))
	}
	found := false
	for _, p := range ps {
		if p.Name == "_default" {
			found = true
		}
	}
	if !found {
		t.Errorf("_default seed not in list")
	}
}

func TestProjectRepo_Create_DuplicateName(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()
	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	p2 := sampleProject("backend")
	err := repo.Create(ctx, p2)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("dup Create: got %v, want ErrConflict", err)
	}
}

func TestProjectRepo_Create_UnknownWorkflow(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()
	p := sampleProject("backend")
	p.WorkflowID = uuid.New()
	err := repo.Create(ctx, p)
	if err == nil {
		t.Fatalf("expected FK violation, got nil")
	}
}
