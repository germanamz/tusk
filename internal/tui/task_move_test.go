package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestRunMove_MutualExclusion_BeforeAndAfter(t *testing.T) {
	app, _ := testApp(t)
	app.root.SetArgs([]string{"task", "move", "abcd1234", "--before", "efgh5678", "--after", "efgh5678"})
	if err := app.root.Execute(); err == nil {
		t.Fatal("expected error when --before and --after are both set")
	}
}

func TestRunMove_MutualExclusion_BeforeAndFirst(t *testing.T) {
	app, _ := testApp(t)
	app.root.SetArgs([]string{"task", "move", "abcd1234", "--before", "efgh5678", "--first"})
	if err := app.root.Execute(); err == nil {
		t.Fatal("expected error when --before and --first are both set")
	}
}

func TestRunMove_MutualExclusion_BeforeAndResequence(t *testing.T) {
	app, _ := testApp(t)
	app.root.SetArgs([]string{"task", "move", "--before", "efgh5678", "--resequence", "abcd1234"})
	if err := app.root.Execute(); err == nil {
		t.Fatal("expected error when --before and --resequence are both set")
	}
}

func TestRunMove_ParentRequiresFirstOrLast(t *testing.T) {
	app, _ := testApp(t)
	app.root.SetArgs([]string{"task", "move", "abcd1234", "--before", "efgh5678", "--parent", "root"})
	if err := app.root.Execute(); err == nil {
		t.Fatal("expected error when --parent is used without --first or --last")
	}
}

func TestRunMove_NoModeRejected(t *testing.T) {
	app, _ := testApp(t)
	app.root.SetArgs([]string{"task", "move", "abcd1234"})
	if err := app.root.Execute(); err == nil {
		t.Fatal("expected error when no positional mode flag is supplied")
	}
}

func TestRunMove_Before_SameParent(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	a := &domain.Task{Title: "A"}
	if err := taskSvc.Create(ctx, a); err != nil {
		t.Fatalf("create A: %v", err)
	}
	b := &domain.Task{Title: "B"}
	if err := taskSvc.Create(ctx, b); err != nil {
		t.Fatalf("create B: %v", err)
	}
	c := &domain.Task{Title: "C"}
	if err := taskSvc.Create(ctx, c); err != nil {
		t.Fatalf("create C: %v", err)
	}
	// Orders should be 1, 2, 3 respectively. Move C to before B → C lands
	// between A (1.0) and B (2.0).

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "move", c.ShortID, "--before", b.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("move: %v", err)
	}

	got, err := taskSvc.GetByShortID(ctx, c.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID C: %v", err)
	}
	if got.Order == nil {
		t.Fatal("expected C to have a non-nil order after move")
	}
	if *got.Order <= 1.0 || *got.Order >= 2.0 {
		t.Fatalf("expected C.order in (1.0, 2.0), got %v", *got.Order)
	}
}

func TestRunMove_FirstRootReparent(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	root := &domain.Task{Title: "root-parent"}
	if err := taskSvc.Create(ctx, root); err != nil {
		t.Fatalf("create root: %v", err)
	}
	child := &domain.Task{Title: "child", ParentID: &root.ID}
	if err := taskSvc.Create(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}

	app.root.SetArgs([]string{"task", "move", child.ShortID, "--first", "--parent", "root"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("move: %v", err)
	}

	got, err := taskSvc.GetByShortID(ctx, child.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.ParentID != nil {
		t.Fatalf("expected child re-parented to root, got %v", got.ParentID)
	}
}

func TestRunMove_Resequence(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	parent := &domain.Task{Title: "parent"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	for _, title := range []string{"A", "B", "C"} {
		c := &domain.Task{Title: title, ParentID: &parent.ID}
		if err := taskSvc.Create(ctx, c); err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
	}

	// Sparse: perturb one child's order to force a rewrite.
	children, err := taskSvc.GetChildren(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetChildren: %v", err)
	}
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(children))
	}
	o := 100.5
	upd := domain.TaskUpdate{
		ShortID: children[1].ShortID,
		Version: children[1].Version,
	}
	op := &o
	upd.Order = &op
	if _, err := taskSvc.Update(ctx, upd); err != nil {
		t.Fatalf("Update order: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "move", "--resequence", parent.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("resequence: %v", err)
	}
	if !strings.Contains(buf.String(), "resequenced") {
		t.Fatalf("expected 'resequenced' in output, got %q", buf.String())
	}
}

func TestRunMove_Resequence_JSON(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	parent := &domain.Task{Title: "parent"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child := &domain.Task{Title: "child", ParentID: &parent.ID}
	if err := taskSvc.Create(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"--format", "json", "task", "move", "--resequence", parent.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("resequence: %v", err)
	}
	var resp struct {
		Rewritten int     `json:"rewritten"`
		ParentID  *string `json:"parent_id"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %s", err, buf.String())
	}
	if resp.ParentID == nil || *resp.ParentID != parent.ID.String() {
		t.Fatalf("expected parent_id %s, got %v", parent.ID.String(), resp.ParentID)
	}
}

func TestRunMove_TaskNotFound(t *testing.T) {
	app, _ := testApp(t)
	app.root.SetArgs([]string{"task", "move", "deadbeef", "--first"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error when subject task missing")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Fatalf("expected 'not found' in error, got %q", err)
	}
}
