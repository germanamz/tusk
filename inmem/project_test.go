package inmem_test

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/inmem"
	"github.com/google/uuid"
)

func TestProjectRepository_GetByName(t *testing.T) {
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
		p, err := repo.GetByName(ctx, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name != "default" {
			t.Errorf("expected ID 'default', got %q", p.Name)
		}
		expectedWorkflowID := uuid.Nil
		if p.WorkflowID != expectedWorkflowID {
			t.Errorf("expected WorkflowID for kanban, got %v", p.WorkflowID)
		}
	})

	t.Run("project with settings", func(t *testing.T) {
		p, err := repo.GetByName(ctx, "backend")
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
		_, err := repo.GetByName(ctx, "nonexistent")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestProjectRepository_GetByID(t *testing.T) {
	projects := map[string]config.ProjectConfig{
		"backend": {Workflow: "kanban"},
	}
	repo := inmem.NewProjectRepository(projects)
	ctx := context.Background()

	p, err := repo.GetByName(ctx, "backend")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}

	byID, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if byID.Name != "backend" {
		t.Errorf("got name %q, want backend", byID.Name)
	}
}

func TestProjectRepository_GetByID_NotFound(t *testing.T) {
	repo := inmem.NewProjectRepository(map[string]config.ProjectConfig{})
	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
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

	// Verify sorted by name
	if list[0].Name != "backend" {
		t.Errorf("expected first project 'backend', got %q", list[0].Name)
	}
	if list[1].Name != "default" {
		t.Errorf("expected second project 'default', got %q", list[1].Name)
	}
	if list[2].Name != "mobile" {
		t.Errorf("expected third project 'mobile', got %q", list[2].Name)
	}
}

func TestProjectRepository_Reload(t *testing.T) {
	repo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"alpha": {Workflow: "kanban"},
	})

	got, err := repo.List(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "alpha" {
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
	if got[0].Name != "beta" || got[1].Name != "gamma" {
		t.Fatalf("post-reload names: got [%s %s], want [beta gamma]", got[0].Name, got[1].Name)
	}
	if _, err := repo.GetByName(context.Background(), "alpha"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("alpha should be gone after Reload, got err=%v", err)
	}
}
