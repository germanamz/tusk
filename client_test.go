package tusk

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestNewClient_CreateAndGetTask(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	task := &domain.Task{Title: "Test task"}
	if err := client.Tasks.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if task.ShortID == "" {
		t.Fatal("expected ShortID to be set after create")
	}

	got, err := client.Tasks.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}

	if got.Title != "Test task" {
		t.Errorf("Title = %q, want %q", got.Title, "Test task")
	}
}

func TestNewClient_DefaultConfig(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	projects, err := client.Projects.List(ctx)
	if err != nil {
		t.Fatalf("Projects.List: %v", err)
	}
	found := false
	for _, p := range projects {
		if p.Name == "default" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected builtin 'default' project")
	}

	workflows, err := client.Workflows.List(ctx)
	if err != nil {
		t.Fatalf("Workflows.List: %v", err)
	}
	found = false
	for _, w := range workflows {
		if w.Name == "kanban" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected builtin 'kanban' workflow")
	}
}

func TestNewClient_EmptyDBPath(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("expected error for empty DBPath")
	}
}

func TestNewClient_Notes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if client.Notes == nil {
		t.Fatal("Notes service should not be nil")
	}
}

func TestNewClient_Portability(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if client.Portability == nil {
		t.Fatal("Portability service should not be nil")
	}
}

func TestClose(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Operations after close should fail.
	ctx := context.Background()
	task := &domain.Task{Title: "After close"}
	if err := client.Tasks.Create(ctx, task); err == nil {
		t.Fatal("expected error after Close")
	}
}
