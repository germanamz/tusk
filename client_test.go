package tusk

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestNewClient_CreateAndGetTask(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		test.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	task := &domain.Task{Title: "Test task"}
	if err := client.Tasks.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	if task.ShortID == "" {
		test.Fatal("expected ShortID to be set after create")
	}

	got, err := client.Tasks.GetByShortID(ctx, task.ShortID)
	if err != nil {
		test.Fatalf("GetByShortID: %v", err)
	}

	if got.Title != "Test task" {
		test.Errorf("Title = %q, want %q", got.Title, "Test task")
	}
}

func TestNewClient_DefaultConfig(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		test.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	projects, err := client.Projects.List(ctx)
	if err != nil {
		test.Fatalf("Projects.List: %v", err)
	}
	found := false
	for _, project := range projects {
		if project.Name == "default" {
			found = true
			break
		}
	}
	if !found {
		test.Error("expected builtin 'default' project")
	}

	workflows, err := client.Workflows.List(ctx)
	if err != nil {
		test.Fatalf("Workflows.List: %v", err)
	}
	found = false
	for _, workflow := range workflows {
		if workflow.Name == "kanban" {
			found = true
			break
		}
	}
	if !found {
		test.Error("expected builtin 'kanban' workflow")
	}
}

func TestNewClient_EmptyDBPath(test *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		test.Fatal("expected error for empty DBPath")
	}
}

func TestNewClient_Notes(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		test.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if client.Notes == nil {
		test.Fatal("Notes service should not be nil")
	}
}

func TestNewClient_Portability(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		test.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if client.Portability == nil {
		test.Fatal("Portability service should not be nil")
	}
}

func TestClose(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		test.Fatalf("NewClient: %v", err)
	}

	if err := client.Close(); err != nil {
		test.Fatalf("Close: %v", err)
	}

	// Operations after close should fail.
	ctx := context.Background()
	task := &domain.Task{Title: "After close"}
	if err := client.Tasks.Create(ctx, task); err == nil {
		test.Fatal("expected error after Close")
	}
}
