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

func TestProjectRepo_Update_IncrementsVersion(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()
	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	priority := 15.0
	p.Settings.Urgency = &domain.UrgencyOverrides{BlockingWeight: &priority}
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p.Version != 2 {
		t.Errorf("local version: got %d, want 2", p.Version)
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("stored version: got %d, want 2", got.Version)
	}
	if got.Settings.Urgency == nil || got.Settings.Urgency.BlockingWeight == nil {
		t.Errorf("urgency override lost round-trip")
	}
}

func TestProjectRepo_Update_StaleVersion(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()
	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	stale := *p
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	err := repo.Update(ctx, &stale)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale Update: got %v, want ErrConflict", err)
	}
}

func TestProjectRepo_CountByWorkflow(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()

	n, err := repo.CountByWorkflow(ctx, defaultUUID)
	if err != nil {
		t.Fatalf("CountByWorkflow seed: %v", err)
	}
	if n != 1 {
		t.Errorf("seed count: got %d, want 1 (the _default project)", n)
	}

	for _, name := range []string{"backend", "frontend"} {
		p := sampleProject(name)
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}

	n, err = repo.CountByWorkflow(ctx, defaultUUID)
	if err != nil {
		t.Fatalf("CountByWorkflow after inserts: %v", err)
	}
	if n != 3 {
		t.Errorf("count after inserts: got %d, want 3", n)
	}

	n, err = repo.CountByWorkflow(ctx, uuid.New())
	if err != nil {
		t.Fatalf("CountByWorkflow unknown workflow: %v", err)
	}
	if n != 0 {
		t.Errorf("unknown workflow count: got %d, want 0", n)
	}
}

func TestProjectRepo_Delete(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()

	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, p.ID, p.Version); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := repo.GetByID(ctx, p.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("after Delete, GetByID: got %v, want ErrNotFound", err)
	}
}

func TestProjectRepo_Delete_StaleVersion(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()

	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	err := repo.Delete(ctx, p.ID, 1)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale Delete: got %v, want ErrConflict", err)
	}
}

func TestProjectRepo_Delete_NotFound(t *testing.T) {
	repo := newTestProjectRepo(t)
	err := repo.Delete(context.Background(), uuid.New(), 1)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete missing: got %v, want ErrNotFound", err)
	}
}
