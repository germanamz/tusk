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

func newTestProjectRepo(test *testing.T) *sqlite.ProjectRepo {
	test.Helper()
	store, err := sqlite.New(test.TempDir()+"/test.db", migrations.FS)

	if err != nil {
		test.Fatalf("opening test db: %v", err)
	}

	test.Cleanup(func() { store.Close() })
	return sqlite.NewProjectRepo(store.DB())
}

func sampleProject(name string) *domain.Project {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return &domain.Project{
		ID:         uuid.New(),
		Name:       name,
		WorkflowID: defaultUUID,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestProjectRepo_CreateAndGetByID(test *testing.T) {
	repo := newTestProjectRepo(test)
	ctx := context.Background()

	project := sampleProject("backend")
	project.Description = "backend services and APIs"

	if err := repo.Create(ctx, project); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, project.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Name != "backend" {
		test.Errorf("got name %q, want %q", got.Name, "backend")
	}
	if got.WorkflowID != defaultUUID {
		test.Errorf("got workflow_id %v, want kanban UUID", got.WorkflowID)
	}
	if got.Description != "backend services and APIs" {
		test.Errorf("got description %q, want %q", got.Description, "backend services and APIs")
	}
}

func TestProjectRepo_Update_Description(test *testing.T) {
	repo := newTestProjectRepo(test)
	ctx := context.Background()

	project := sampleProject("backend")

	if err := repo.Create(ctx, project); err != nil {
		test.Fatalf("Create: %v", err)
	}

	if project.Description != "" {
		test.Fatalf("seeded description: got %q, want empty", project.Description)
	}

	project.Description = "vision text"

	if err := repo.Update(ctx, project); err != nil {
		test.Fatalf("Update: %v", err)
	}

	got, err := repo.GetByName(ctx, "backend")

	if err != nil {
		test.Fatalf("GetByName: %v", err)
	}

	if got.Description != "vision text" {
		test.Errorf("got description %q, want %q", got.Description, "vision text")
	}
}

func TestProjectRepo_Default_HasEmptyDescription(test *testing.T) {
	repo := newTestProjectRepo(test)
	got, err := repo.GetByID(context.Background(), defaultUUID)

	if err != nil {
		test.Fatalf("GetByID _default: %v", err)
	}

	if got.Description != "" {
		test.Errorf("_default description: got %q, want empty", got.Description)
	}
}

func TestProjectRepo_GetByName_Seed(test *testing.T) {
	repo := newTestProjectRepo(test)
	got, err := repo.GetByName(context.Background(), "default")

	if err != nil {
		test.Fatalf("GetByName _default: %v", err)
	}

	if got.ID != defaultUUID {
		test.Errorf("got ID %v, want defaultUUID", got.ID)
	}
}

func TestProjectRepo_GetByID_NotFound(test *testing.T) {
	repo := newTestProjectRepo(test)
	_, err := repo.GetByID(context.Background(), uuid.New())

	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("GetByID: got %v, want ErrNotFound", err)
	}
}

func TestProjectRepo_List_ContainsSeed(test *testing.T) {
	repo := newTestProjectRepo(test)
	projects, err := repo.List(context.Background())

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(projects) < 1 {
		test.Fatalf("want >= 1 project, got %d", len(projects))
	}
	found := false
	for _, project := range projects {
		if project.Name == "default" {
			found = true
		}
	}
	if !found {
		test.Errorf("_default seed not in list")
	}
}

func TestProjectRepo_Create_DuplicateName(test *testing.T) {
	repo := newTestProjectRepo(test)
	ctx := context.Background()
	project := sampleProject("backend")

	if err := repo.Create(ctx, project); err != nil {
		test.Fatalf("first Create: %v", err)
	}

	duplicate := sampleProject("backend")
	err := repo.Create(ctx, duplicate)

	if !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("dup Create: got %v, want ErrConflict", err)
	}
}

func TestProjectRepo_Create_UnknownWorkflow(test *testing.T) {
	repo := newTestProjectRepo(test)
	ctx := context.Background()
	project := sampleProject("backend")
	project.WorkflowID = uuid.New()
	err := repo.Create(ctx, project)

	if err == nil {
		test.Fatalf("expected FK violation, got nil")
	}
}

func TestProjectRepo_Update_IncrementsVersion(test *testing.T) {
	repo := newTestProjectRepo(test)
	ctx := context.Background()
	project := sampleProject("backend")

	if err := repo.Create(ctx, project); err != nil {
		test.Fatalf("Create: %v", err)
	}

	priority := 15.0
	project.Settings.Urgency = &domain.UrgencyOverrides{BlockingWeight: &priority}

	if err := repo.Update(ctx, project); err != nil {
		test.Fatalf("Update: %v", err)
	}

	if project.Version != 2 {
		test.Errorf("local version: got %d, want 2", project.Version)
	}

	got, err := repo.GetByID(ctx, project.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Version != 2 {
		test.Errorf("stored version: got %d, want 2", got.Version)
	}
	if got.Settings.Urgency == nil || got.Settings.Urgency.BlockingWeight == nil {
		test.Errorf("urgency override lost round-trip")
	}
}

func TestProjectRepo_Update_StaleVersion(test *testing.T) {
	repo := newTestProjectRepo(test)
	ctx := context.Background()
	project := sampleProject("backend")

	if err := repo.Create(ctx, project); err != nil {
		test.Fatalf("Create: %v", err)
	}

	stale := *project

	if err := repo.Update(ctx, project); err != nil {
		test.Fatalf("first Update: %v", err)
	}

	err := repo.Update(ctx, &stale)

	if !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("stale Update: got %v, want ErrConflict", err)
	}
}

func TestProjectRepo_CountProjectsByWorkflow(test *testing.T) {
	repo := newTestProjectRepo(test)
	ctx := context.Background()

	count, err := repo.CountProjectsByWorkflow(ctx, defaultUUID)

	if err != nil {
		test.Fatalf("CountProjectsByWorkflow seed: %v", err)
	}

	if count != 1 {
		test.Errorf("seed count: got %d, want 1 (the _default project)", count)
	}

	for _, name := range []string{"backend", "frontend"} {
		project := sampleProject(name)

		if err := repo.Create(ctx, project); err != nil {
			test.Fatalf("Create %s: %v", name, err)
		}
	}

	count, err = repo.CountProjectsByWorkflow(ctx, defaultUUID)

	if err != nil {
		test.Fatalf("CountProjectsByWorkflow after inserts: %v", err)
	}

	if count != 3 {
		test.Errorf("count after inserts: got %d, want 3", count)
	}

	count, err = repo.CountProjectsByWorkflow(ctx, uuid.New())

	if err != nil {
		test.Fatalf("CountProjectsByWorkflow unknown workflow: %v", err)
	}

	if count != 0 {
		test.Errorf("unknown workflow count: got %d, want 0", count)
	}
}

func TestProjectRepo_Delete(test *testing.T) {
	repo := newTestProjectRepo(test)
	ctx := context.Background()

	project := sampleProject("backend")

	if err := repo.Create(ctx, project); err != nil {
		test.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, project.ID, project.Version); err != nil {
		test.Fatalf("Delete: %v", err)
	}

	_, err := repo.GetByID(ctx, project.ID)

	if !errors.Is(err, domain.ErrNotFound) {
		test.Errorf("after Delete, GetByID: got %v, want ErrNotFound", err)
	}
}

func TestProjectRepo_Delete_StaleVersion(test *testing.T) {
	repo := newTestProjectRepo(test)
	ctx := context.Background()

	project := sampleProject("backend")

	if err := repo.Create(ctx, project); err != nil {
		test.Fatalf("Create: %v", err)
	}

	if err := repo.Update(ctx, project); err != nil {
		test.Fatalf("Update: %v", err)
	}

	err := repo.Delete(ctx, project.ID, 1)

	if !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("stale Delete: got %v, want ErrConflict", err)
	}
}

func TestProjectRepo_Delete_NotFound(test *testing.T) {
	repo := newTestProjectRepo(test)
	err := repo.Delete(context.Background(), uuid.New(), 1)

	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("Delete missing: got %v, want ErrNotFound", err)
	}
}
