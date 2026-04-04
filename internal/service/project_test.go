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

func TestProjectService_ModifyDescription(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", "Old description"); err != nil {
		t.Fatal(err)
	}

	desc := "New description"
	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Description: &desc,
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if updated.Description != "New description" {
		t.Fatalf("expected 'New description', got %q", updated.Description)
	}
	// Version should have incremented
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
}

func TestProjectService_ModifyNotFound(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	desc := "whatever"
	_, err := svc.Modify(ctx, "nonexistent", ModifyOptions{
		Description: &desc,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestProjectService_ModifyNoOptions(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", "Desc"); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Modify(ctx, "proj", ModifyOptions{})
	if err == nil {
		t.Fatal("expected error when no modifications provided")
	}
}

func TestProjectService_ModifySetAutoComplete(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"auto_complete_parent.trigger_status": "completed",
			"auto_complete_parent.target_status":  "completed",
		},
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if updated.Settings.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to be set")
	}
	if updated.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", updated.Settings.AutoCompleteParent.TriggerStatus)
	}
	if updated.Settings.AutoCompleteParent.TargetStatus != "completed" {
		t.Fatalf("expected target_status 'completed', got %q", updated.Settings.AutoCompleteParent.TargetStatus)
	}
}

func TestProjectService_ModifySetAutoRevert(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"auto_revert_parent.trigger_status": "completed",
			"auto_revert_parent.target_status":  "pending",
		},
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if updated.Settings.AutoRevertParent == nil {
		t.Fatal("expected AutoRevertParent to be set")
	}
	if updated.Settings.AutoRevertParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", updated.Settings.AutoRevertParent.TriggerStatus)
	}
	if updated.Settings.AutoRevertParent.TargetStatus != "pending" {
		t.Fatalf("expected target_status 'pending', got %q", updated.Settings.AutoRevertParent.TargetStatus)
	}
}

func TestProjectService_ModifySetAutoInitNilConfig(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	// Setting just one field should auto-initialize the parent struct
	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"auto_complete_parent.trigger_status": "completed",
		},
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if updated.Settings.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to be auto-initialized")
	}
	if updated.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", updated.Settings.AutoCompleteParent.TriggerStatus)
	}
	// target_status should be zero value (empty string) since we didn't set it
	if updated.Settings.AutoCompleteParent.TargetStatus != "" {
		t.Fatalf("expected empty target_status, got %q", updated.Settings.AutoCompleteParent.TargetStatus)
	}
}

func TestProjectService_ModifyUnsetAutoComplete(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	// First, set auto-complete
	if _, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"auto_complete_parent.trigger_status": "completed",
			"auto_complete_parent.target_status":  "completed",
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Then unset it
	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Unsets: []string{"auto_complete_parent"},
	})
	if err != nil {
		t.Fatalf("Modify unset: %v", err)
	}
	if updated.Settings.AutoCompleteParent != nil {
		t.Fatal("expected AutoCompleteParent to be nil after unset")
	}
}

func TestProjectService_ModifyUnsetAutoRevert(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	// Set auto-revert
	if _, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"auto_revert_parent.trigger_status": "completed",
			"auto_revert_parent.target_status":  "pending",
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Unset it
	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Unsets: []string{"auto_revert_parent"},
	})
	if err != nil {
		t.Fatalf("Modify unset: %v", err)
	}
	if updated.Settings.AutoRevertParent != nil {
		t.Fatal("expected AutoRevertParent to be nil after unset")
	}
}

func TestProjectService_ModifyUnknownDotPath(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"unknown.path": "value",
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown dot-path")
	}
}

func TestProjectService_ModifyUnknownUnsetPath(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Modify(ctx, "proj", ModifyOptions{
		Unsets: []string{"unknown_key"},
	})
	if err == nil {
		t.Fatal("expected error for unknown unset path")
	}
}
