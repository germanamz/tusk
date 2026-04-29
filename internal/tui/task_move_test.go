package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestRunMove_MutualExclusion_BeforeAndAfter(test *testing.T) {
	app, _ := testApp(test)
	app.root.SetArgs([]string{"task", "move", "abcd1234", "--before", "efgh5678", "--after", "efgh5678"})
	if err := app.root.Execute(); err == nil {
		test.Fatal("expected error when --before and --after are both set")
	}
}

func TestRunMove_MutualExclusion_BeforeAndFirst(test *testing.T) {
	app, _ := testApp(test)
	app.root.SetArgs([]string{"task", "move", "abcd1234", "--before", "efgh5678", "--first"})
	if err := app.root.Execute(); err == nil {
		test.Fatal("expected error when --before and --first are both set")
	}
}

func TestRunMove_MutualExclusion_BeforeAndResequence(test *testing.T) {
	app, _ := testApp(test)
	app.root.SetArgs([]string{"task", "move", "--before", "efgh5678", "--resequence", "abcd1234"})
	if err := app.root.Execute(); err == nil {
		test.Fatal("expected error when --before and --resequence are both set")
	}
}

func TestRunMove_ParentRequiresFirstOrLast(test *testing.T) {
	app, _ := testApp(test)
	app.root.SetArgs([]string{"task", "move", "abcd1234", "--before", "efgh5678", "--parent", "root"})
	if err := app.root.Execute(); err == nil {
		test.Fatal("expected error when --parent is used without --first or --last")
	}
}

func TestRunMove_NoModeRejected(test *testing.T) {
	app, _ := testApp(test)
	app.root.SetArgs([]string{"task", "move", "abcd1234"})
	if err := app.root.Execute(); err == nil {
		test.Fatal("expected error when no positional mode flag is supplied")
	}
}

func TestRunMove_Before_SameParent(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	taskA := &domain.Task{Title: "A"}
	if err := taskSvc.Create(ctx, taskA); err != nil {
		test.Fatalf("create A: %v", err)
	}
	taskB := &domain.Task{Title: "B"}
	if err := taskSvc.Create(ctx, taskB); err != nil {
		test.Fatalf("create B: %v", err)
	}
	taskC := &domain.Task{Title: "C"}
	if err := taskSvc.Create(ctx, taskC); err != nil {
		test.Fatalf("create C: %v", err)
	}
	// Orders should be 1, 2, 3 respectively. Move C to before B → C lands
	// between A (1.0) and B (2.0).

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "move", taskC.ShortID, "--before", taskB.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("move: %v", err)
	}

	got, getErr := taskSvc.GetByShortID(ctx, taskC.ShortID)

	if getErr != nil {
		test.Fatalf("GetByShortID C: %v", getErr)
	}

	if got.Order == nil {
		test.Fatal("expected C to have a non-nil order after move")
	}
	if *got.Order <= 1.0 || *got.Order >= 2.0 {
		test.Fatalf("expected C.order in (1.0, 2.0), got %v", *got.Order)
	}
}

func TestRunMove_FirstRootReparent(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	root := &domain.Task{Title: "root-parent"}
	if err := taskSvc.Create(ctx, root); err != nil {
		test.Fatalf("create root: %v", err)
	}
	child := &domain.Task{Title: "child", ParentID: &root.ID}
	if err := taskSvc.Create(ctx, child); err != nil {
		test.Fatalf("create child: %v", err)
	}

	app.root.SetArgs([]string{"task", "move", child.ShortID, "--first", "--parent", "root"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("move: %v", err)
	}

	got, getErr := taskSvc.GetByShortID(ctx, child.ShortID)

	if getErr != nil {
		test.Fatalf("GetByShortID: %v", getErr)
	}

	if got.ParentID != nil {
		test.Fatalf("expected child re-parented to root, got %v", got.ParentID)
	}
}

func TestRunMove_Resequence(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	parent := &domain.Task{Title: "parent"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		test.Fatalf("create parent: %v", err)
	}
	for _, title := range []string{"A", "B", "C"} {
		child := &domain.Task{Title: title, ParentID: &parent.ID}
		if err := taskSvc.Create(ctx, child); err != nil {
			test.Fatalf("create %s: %v", title, err)
		}
	}

	// Sparse: perturb one child's order to force a rewrite.
	children, getErr := taskSvc.GetChildren(ctx, parent.ID)

	if getErr != nil {
		test.Fatalf("GetChildren: %v", getErr)
	}

	if len(children) != 3 {
		test.Fatalf("expected 3 children, got %d", len(children))
	}
	orderVal := 100.5
	upd := domain.TaskUpdate{
		ShortID: children[1].ShortID,
		Version: children[1].Version,
	}
	orderPtr := &orderVal
	upd.Order = &orderPtr
	if _, err := taskSvc.Update(ctx, upd); err != nil {
		test.Fatalf("Update order: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "move", "--resequence", parent.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("resequence: %v", err)
	}
	if !strings.Contains(buf.String(), "resequenced") {
		test.Fatalf("expected 'resequenced' in output, got %q", buf.String())
	}
}

func TestRunMove_Resequence_JSON(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	parent := &domain.Task{Title: "parent"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		test.Fatalf("create parent: %v", err)
	}
	child := &domain.Task{Title: "child", ParentID: &parent.ID}
	if err := taskSvc.Create(ctx, child); err != nil {
		test.Fatalf("create child: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"--format", "json", "task", "move", "--resequence", parent.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("resequence: %v", err)
	}
	var resp struct {
		Rewritten int     `json:"rewritten"`
		ParentID  *string `json:"parent_id"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		test.Fatalf("decode JSON: %v\nraw: %s", err, buf.String())
	}
	if resp.ParentID == nil || *resp.ParentID != parent.ID.String() {
		test.Fatalf("expected parent_id %s, got %v", parent.ID.String(), resp.ParentID)
	}
}

func TestRunMove_TaskNotFound(test *testing.T) {
	app, _ := testApp(test)
	app.root.SetArgs([]string{"task", "move", "deadbeef", "--first"})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error when subject task missing")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		test.Fatalf("expected 'not found' in error, got %q", err)
	}
}
