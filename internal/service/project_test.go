package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

func testProjectService(t *testing.T) *ProjectService {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	repo := sqlite.NewProjectRepo(store.DB())
	return NewProjectService(repo)
}

func TestProjectService_Create(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	p, err := svc.Create(ctx, "backend", "Backend services")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.Name != "backend" {
		t.Fatalf("expected name 'backend', got %q", p.Name)
	}
	if p.Description != "Backend services" {
		t.Fatalf("expected description 'Backend services', got %q", p.Description)
	}
	if p.DefaultWorkflow != "default" {
		t.Fatalf("expected workflow 'default', got %q", p.DefaultWorkflow)
	}
	if p.Version != 1 {
		t.Fatalf("expected version 1, got %d", p.Version)
	}
	if p.ID.String() == "" {
		t.Fatal("expected UUID to be set")
	}
}

func TestProjectService_CreateDuplicate(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "dup", "First"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, "dup", "Second")
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
}

func TestProjectService_CreateEmptyName(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, "", "No name")
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestProjectService_List(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	// _default is seeded by migration
	if _, err := svc.Create(ctx, "proj1", ""); err != nil {
		t.Fatal(err)
	}

	projects, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// _default + proj1 = 2
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

func TestProjectService_GetByName(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "myproj", "My project"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetByName(ctx, "myproj")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Name != "myproj" {
		t.Fatalf("expected 'myproj', got %q", got.Name)
	}
}

func TestProjectService_GetByNameNotFound(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	_, err := svc.GetByName(ctx, "nonexistent")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
