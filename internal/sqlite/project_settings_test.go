package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

func TestProjectRepo_SettingsRoundTrip(t *testing.T) {
	store, err := New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := NewProjectRepo(store.DB())

	// The seeded _default project should have empty settings
	defaultID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	proj, err := repo.GetByID(ctx, defaultID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if proj.Settings.AutoCompleteParent != nil {
		t.Fatal("expected default project AutoCompleteParent to be nil")
	}
	if proj.Settings.AutoRevertParent != nil {
		t.Fatal("expected default project AutoRevertParent to be nil")
	}

	// Update with auto-complete settings
	proj.Settings = domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	}
	if err := repo.Update(ctx, proj); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Re-read and verify
	proj2, err := repo.GetByID(ctx, defaultID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if proj2.Settings.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to be non-nil")
	}
	if proj2.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", proj2.Settings.AutoCompleteParent.TriggerStatus)
	}
	if proj2.Settings.AutoCompleteParent.TargetStatus != "completed" {
		t.Fatalf("expected target_status 'completed', got %q", proj2.Settings.AutoCompleteParent.TargetStatus)
	}
	if proj2.Settings.AutoRevertParent != nil {
		t.Fatal("expected AutoRevertParent to still be nil")
	}

	// Update with both settings
	proj2.Settings.AutoRevertParent = &domain.AutoRevertConfig{
		TriggerStatus: "completed",
		TargetStatus:  "active",
	}
	if err := repo.Update(ctx, proj2); err != nil {
		t.Fatalf("Update with both: %v", err)
	}

	proj3, err := repo.GetByID(ctx, defaultID)
	if err != nil {
		t.Fatalf("GetByID after second update: %v", err)
	}
	if proj3.Settings.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to persist")
	}
	if proj3.Settings.AutoRevertParent == nil {
		t.Fatal("expected AutoRevertParent to be non-nil")
	}
	if proj3.Settings.AutoRevertParent.TargetStatus != "active" {
		t.Fatalf("expected revert target_status 'active', got %q", proj3.Settings.AutoRevertParent.TargetStatus)
	}
}

func TestProjectRepo_SettingsList(t *testing.T) {
	store, err := New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := NewProjectRepo(store.DB())

	// List should include settings
	projects, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("expected at least one project")
	}
	// Default project should have empty settings
	for _, p := range projects {
		if p.Settings.AutoCompleteParent != nil {
			t.Fatalf("project %q: expected nil AutoCompleteParent", p.Name)
		}
	}
}

func TestProjectRepo_SettingsCreate(t *testing.T) {
	store, err := New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := NewProjectRepo(store.DB())

	// Create a project with settings
	proj := &domain.Project{
		ID:              uuid.New(),
		Name:            "test-project",
		DefaultWorkflow: "default",
		Version:         1,
		Settings: domain.ProjectSettings{
			AutoCompleteParent: &domain.AutoCompleteConfig{
				TriggerStatus: "done",
				TargetStatus:  "done",
			},
		},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, proj); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, proj.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Settings.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent on created project")
	}
	if got.Settings.AutoCompleteParent.TriggerStatus != "done" {
		t.Fatalf("expected trigger_status 'done', got %q", got.Settings.AutoCompleteParent.TriggerStatus)
	}
}
