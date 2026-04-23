package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
)

// TestRunCreate_Level covers the CLI level= parse on `tusk task create`.
// No taxonomy is configured here, so the service accepts any level verbatim.
func TestRunCreate_Level(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	app.root.SetArgs([]string{"task", "create", "Top-level goal", "level=story"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("create: %v", err)
	}

	tasks, err := taskSvc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Level == nil || *tasks[0].Level != "story" {
		t.Fatalf("expected level 'story', got %v", tasks[0].Level)
	}
}

func TestRunCreate_LevelEmptyRejected(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"task", "create", "No-op", "level="})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for empty level= on create")
	}
	if !strings.Contains(err.Error(), "level") {
		t.Fatalf("error %q should reference 'level'", err)
	}
}

func TestRunCreate_LevelModifierRejected(t *testing.T) {
	app, _ := testApp(t)

	// `+level=story` and `-level=story` must be rejected on create.
	app.root.SetArgs([]string{"task", "create", "bad", "+level=story"})
	if err := app.root.Execute(); err == nil {
		t.Fatal("expected error for +level= on create")
	}

	app.root.SetArgs([]string{"task", "create", "bad", "--", "-level=story"})
	if err := app.root.Execute(); err == nil {
		t.Fatal("expected error for -level= on create")
	}
}

func TestRunModify_Level(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "modify level"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "modify", task.ShortID, "level=task"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify: %v", err)
	}

	got, err := taskSvc.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.Level == nil || *got.Level != "task" {
		t.Fatalf("expected level 'task', got %v", got.Level)
	}
}

func TestRunModify_ClearLevel(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	lvl := "legacy"
	task := &domain.Task{Title: "clear level", Level: &lvl}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	app.root.SetArgs([]string{"task", "modify", task.ShortID, "level="})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify: %v", err)
	}

	got, err := taskSvc.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.Level != nil {
		t.Fatalf("expected cleared level, got %v", got.Level)
	}
}

func TestRunModify_LevelModifierRejected(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "mod-reject"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	app.root.SetArgs([]string{"task", "modify", task.ShortID, "+level=story"})
	if err := app.root.Execute(); err == nil {
		t.Fatal("expected error for +level= on modify")
	}

	app.root.SetArgs([]string{"task", "modify", task.ShortID, "--", "-level=story"})
	if err := app.root.Execute(); err == nil {
		t.Fatal("expected error for -level= on modify")
	}
}
