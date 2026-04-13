package inmem_test

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/inmem"
)

func TestProjectRepository_GetByID(t *testing.T) {
	projects := map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {
			Workflow: "kanban",
			Settings: config.ProjectSettingsConfig{
				AutoCompleteParent: &config.AutoCompleteParentConfig{
					TriggerStatus: "completed",
					TargetStatus:  "completed",
				},
			},
		},
	}

	repo := inmem.NewProjectRepository(projects)
	ctx := context.Background()

	t.Run("existing project", func(t *testing.T) {
		p, err := repo.GetByID(ctx, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.ID != "default" {
			t.Errorf("expected ID 'default', got %q", p.ID)
		}
		if p.Workflow != "kanban" {
			t.Errorf("expected Workflow 'kanban', got %q", p.Workflow)
		}
	})

	t.Run("project with settings", func(t *testing.T) {
		p, err := repo.GetByID(ctx, "backend")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Settings.AutoCompleteParent == nil {
			t.Fatal("expected AutoCompleteParent settings")
		}
		if p.Settings.AutoCompleteParent.TriggerStatus != "completed" {
			t.Errorf("expected trigger_status 'completed', got %q", p.Settings.AutoCompleteParent.TriggerStatus)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := repo.GetByID(ctx, "nonexistent")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestProjectRepository_List(t *testing.T) {
	projects := map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {Workflow: "kanban"},
		"mobile":  {Workflow: "kanban"},
	}

	repo := inmem.NewProjectRepository(projects)
	ctx := context.Background()

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(list))
	}

	// Verify sorted by ID
	if list[0].ID != "backend" {
		t.Errorf("expected first project 'backend', got %q", list[0].ID)
	}
	if list[1].ID != "default" {
		t.Errorf("expected second project 'default', got %q", list[1].ID)
	}
	if list[2].ID != "mobile" {
		t.Errorf("expected third project 'mobile', got %q", list[2].ID)
	}
}

func TestProjectRepository_Reload(t *testing.T) {
	repo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"alpha": {Workflow: "kanban"},
	})

	got, err := repo.List(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != "alpha" {
		t.Fatalf("pre-reload List: got %+v err=%v", got, err)
	}

	repo.Reload(map[string]config.ProjectConfig{
		"beta":  {Workflow: "kanban"},
		"gamma": {Workflow: "kanban"},
	})

	got, err = repo.List(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("post-reload List: got %+v err=%v", got, err)
	}
	if got[0].ID != "beta" || got[1].ID != "gamma" {
		t.Fatalf("post-reload IDs: got [%s %s], want [beta gamma]", got[0].ID, got[1].ID)
	}
	if _, err := repo.GetByID(context.Background(), "alpha"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("alpha should be gone after Reload, got err=%v", err)
	}
}
