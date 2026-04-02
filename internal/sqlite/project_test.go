package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// Compile-time check: *ProjectRepo must implement repository.ProjectRepository.
// If ProjectRepo is missing any method, this line produces a compile error.
// The nil pointer is never dereferenced — it costs nothing at runtime.
var _ repository.ProjectRepository = (*ProjectRepo)(nil)

// TestProjectCreate verifies that we can insert a new project and read it back.
// It exercises Create and GetByID together because you need GetByID to verify
// that Create actually persisted the data.
func TestProjectCreate(t *testing.T) {
	// testStore creates an in-memory SQLite database with all migrations applied.
	// It registers t.Cleanup to close the DB when this test finishes.
	s := testStore(t)

	// NewProjectRepo takes a *sql.DB (not a *Store). We get the *sql.DB via s.DB().
	// This keeps ProjectRepo decoupled from the Store type — it only needs
	// the standard library's database interface.
	repo := NewProjectRepo(s.DB())

	// context.Background() returns a non-nil, empty context. It is never cancelled.
	// In tests, this is fine. In production, you would use a context with a timeout.
	ctx := context.Background()

	// Build a Project value. We set all fields explicitly so there are no surprises.
	// time.Now().UTC().Truncate(time.Millisecond) matches SQLite's millisecond
	// precision — without Truncate, the round-trip would lose sub-millisecond data
	// and the comparison would fail.
	p := &domain.Project{
		ID:              uuid.New(),
		Name:            "backend",
		Description:     "Backend services",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}

	// Create should succeed with no error.
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read it back by ID and verify the name survived the round-trip.
	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "backend" {
		t.Fatalf("expected name backend, got %s", got.Name)
	}
}

// TestProjectGetByName verifies lookup by name works.
// The migration seeds a "_default" project, so we do not need to create one.
func TestProjectGetByName(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	// "_default" was inserted by the migration SQL. It should always exist.
	got, err := repo.GetByName(ctx, "_default")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Name != "_default" {
		t.Fatalf("expected _default, got %s", got.Name)
	}
}

// TestProjectGetByIDNotFound verifies that looking up a non-existent ID
// returns domain.ErrNotFound (not sql.ErrNoRows or nil).
func TestProjectGetByIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	// uuid.New() generates a random UUID that definitely does not exist in the DB.
	_, err := repo.GetByID(ctx, uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestProjectGetByNameNotFound verifies the same ErrNotFound behavior for GetByName.
func TestProjectGetByNameNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	_, err := repo.GetByName(ctx, "nonexistent")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestProjectList verifies that List returns all projects.
// The migration seeds 1 project ("_default"). We add 1 more, so we expect 2.
func TestProjectList(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID: uuid.New(), Name: "frontend", Description: "Frontend app",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}

	projects, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 1 seeded ("_default") + 1 we just created = 2
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

// TestProjectUpdate verifies that Update changes a field and GetByID sees the change.
func TestProjectUpdate(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID: uuid.New(), Name: "mobile", Description: "Mobile app",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}

	// Mutate the in-memory struct and call Update.
	p.Description = "Mobile applications"
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Read it back and verify the change persisted.
	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Mobile applications" {
		t.Fatalf("expected updated description, got %s", got.Description)
	}
}

// TestProjectDeleteNotFound verifies that deleting a non-existent project
// returns domain.ErrNotFound.
func TestProjectDeleteNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	err := repo.Delete(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestProjectCreateDuplicate verifies that creating a project with a duplicate
// name returns an error (UNIQUE constraint violation).
func TestProjectCreateDuplicate(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()
	p1 := &domain.Project{
		ID: uuid.New(), Name: "dupname", Description: "First",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p1); err != nil {
		t.Fatal(err)
	}
	p2 := &domain.Project{
		ID: uuid.New(), Name: "dupname", Description: "Second",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p2); err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
}

// TestProjectDelete verifies that Delete removes a project, and that
// GetByID returns ErrNotFound afterward.
func TestProjectDelete(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID: uuid.New(), Name: "temp", Description: "Temporary",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}

	// Delete should succeed.
	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// After deletion, GetByID should return ErrNotFound.
	_, err := repo.GetByID(ctx, p.ID)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}
