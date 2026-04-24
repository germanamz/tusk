package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestRunList_SortByPriority(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	low := &domain.Task{Title: "low-prio", Priority: 0}
	high := &domain.Task{Title: "high-prio", Priority: 4}
	if err := taskSvc.Create(ctx, low); err != nil {
		t.Fatalf("create low: %v", err)
	}
	if err := taskSvc.Create(ctx, high); err != nil {
		t.Fatalf("create high: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "list", "--sort", "priority"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := buf.String()
	idxHigh := strings.Index(out, high.ShortID)
	idxLow := strings.Index(out, low.ShortID)
	if idxHigh < 0 || idxLow < 0 {
		t.Fatalf("expected both tasks in output: %q", out)
	}
	if idxHigh > idxLow {
		t.Fatalf("expected high-priority task first, got:\n%s", out)
	}
}

func TestRunList_SortByOrder(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	first := &domain.Task{Title: "first"}
	second := &domain.Task{Title: "second"}
	if err := taskSvc.Create(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := taskSvc.Create(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	// Swap their orders so "second" comes first when sorted by order.
	swap := 0.5
	upd := domain.TaskUpdate{
		ShortID: second.ShortID,
		Version: second.Version,
	}
	sp := &swap
	upd.Order = &sp
	if _, err := taskSvc.Update(ctx, upd); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "list", "--sort", "order"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := buf.String()
	idxFirst := strings.Index(out, first.ShortID)
	idxSecond := strings.Index(out, second.ShortID)
	if idxFirst < 0 || idxSecond < 0 {
		t.Fatalf("expected both tasks in output: %q", out)
	}
	if idxSecond > idxFirst {
		t.Fatalf("expected 'second' (order=0.5) before 'first' (order=1.0), got:\n%s", out)
	}
}

func TestRunList_InvalidSortRejected(t *testing.T) {
	app, _ := testApp(t)
	app.root.SetArgs([]string{"task", "list", "--sort", "bogus"})
	if err := app.root.Execute(); err == nil {
		t.Fatal("expected error for invalid --sort value")
	}
}

func TestRunTree_DefaultOrderSort(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	parent := &domain.Task{Title: "parent"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	// Create in reverse so the default-created orders (1, 2) don't already
	// match the task titles alphabetically.
	childZ := &domain.Task{Title: "Z", ParentID: &parent.ID}
	if err := taskSvc.Create(ctx, childZ); err != nil {
		t.Fatalf("create Z: %v", err)
	}
	childA := &domain.Task{Title: "A", ParentID: &parent.ID}
	if err := taskSvc.Create(ctx, childA); err != nil {
		t.Fatalf("create A: %v", err)
	}

	// Force Z to order=0 so it sorts before A when --sort=order.
	zero := 0.0
	upd := domain.TaskUpdate{
		ShortID: childZ.ShortID,
		Version: childZ.Version,
	}
	op := &zero
	upd.Order = &op
	if _, err := taskSvc.Update(ctx, upd); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "tree", parent.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree: %v", err)
	}
	out := buf.String()
	idxZ := strings.Index(out, childZ.ShortID)
	idxA := strings.Index(out, childA.ShortID)
	if idxZ < 0 || idxA < 0 {
		t.Fatalf("expected both children in output: %q", out)
	}
	if idxZ > idxA {
		t.Fatalf("expected childZ (order=0) before childA (order=1), got:\n%s", out)
	}
}
