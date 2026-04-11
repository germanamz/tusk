package filter

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

func testSetup(t *testing.T) (*Resolver, *sqlite.TaskRepo) {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	taskRepo := sqlite.NewTaskRepo(store.DB())
	return NewResolver(taskRepo), taskRepo
}

func TestIntegration_DefaultFilter(t *testing.T) {
	r, _ := testSetup(t)
	fs, parseErrs := Parse("")
	if len(parseErrs) != 0 {
		t.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := r.Resolve(context.Background(), fs)
	if len(resolveErrs) != 0 {
		t.Fatalf("resolve errors: %v", resolveErrs)
	}
	if len(tf.Statuses) != 2 || tf.Statuses[0] != "pending" || tf.Statuses[1] != "active" {
		t.Fatalf("expected default statuses [pending active], got %v", tf.Statuses)
	}
}

func TestIntegration_ComplexFilter(t *testing.T) {
	r, _ := testSetup(t)
	fs, parseErrs := Parse("status=completed project=default priority=2..4 +api -docs")
	if len(parseErrs) != 0 {
		t.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := r.Resolve(context.Background(), fs)
	if len(resolveErrs) != 0 {
		t.Fatalf("resolve errors: %v", resolveErrs)
	}

	if len(tf.Statuses) != 1 || tf.Statuses[0] != "completed" {
		t.Fatalf("expected [completed], got %v", tf.Statuses)
	}
	if tf.ProjectID == nil {
		t.Fatal("expected ProjectID to be set")
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 2 {
		t.Fatalf("expected PriorityMin=2, got %v", tf.PriorityMin)
	}
	if tf.PriorityMax == nil || *tf.PriorityMax != 4 {
		t.Fatalf("expected PriorityMax=4, got %v", tf.PriorityMax)
	}
	if len(tf.Tags) != 1 || tf.Tags[0] != "api" {
		t.Fatalf("expected Tags=[api], got %v", tf.Tags)
	}
	if len(tf.ExcludeTags) != 1 || tf.ExcludeTags[0] != "docs" {
		t.Fatalf("expected ExcludeTags=[docs], got %v", tf.ExcludeTags)
	}
}

func TestIntegration_ParentFilter(t *testing.T) {
	r, taskRepo := testSetup(t)
	ctx := context.Background()

	parent := &domain.Task{
		ID:      uuid.New(),
		ShortID: "abcd1234",
		Title:   "Parent",
		Status:  "pending",
		Version: 1,
	}
	if err := taskRepo.Create(ctx, parent); err != nil {
		t.Fatalf("creating task: %v", err)
	}

	fs, parseErrs := Parse("parent=abcd1234 status=active")
	if len(parseErrs) != 0 {
		t.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := r.Resolve(ctx, fs)
	if len(resolveErrs) != 0 {
		t.Fatalf("resolve errors: %v", resolveErrs)
	}
	if tf.ParentID == nil || *tf.ParentID != parent.ID {
		t.Fatalf("expected ParentID=%v, got %v", parent.ID, tf.ParentID)
	}
	if len(tf.Statuses) != 1 || tf.Statuses[0] != "active" {
		t.Fatalf("expected [active], got %v", tf.Statuses)
	}
}

func TestIntegration_TreeFilter(t *testing.T) {
	r, taskRepo := testSetup(t)
	ctx := context.Background()

	root := &domain.Task{
		ID:      uuid.New(),
		ShortID: "deadbeef",
		Title:   "Root",
		Status:  "pending",
		Version: 1,
	}
	if err := taskRepo.Create(ctx, root); err != nil {
		t.Fatalf("creating task: %v", err)
	}

	fs, parseErrs := Parse("tree=deadbeef")
	if len(parseErrs) != 0 {
		t.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := r.Resolve(ctx, fs)
	if len(resolveErrs) != 0 {
		t.Fatalf("resolve errors: %v", resolveErrs)
	}
	if tf.RootID == nil || *tf.RootID != root.ID {
		t.Fatalf("expected RootID=%v, got %v", root.ID, tf.RootID)
	}
}

func TestIntegration_ParseAndResolveErrors(t *testing.T) {
	r, _ := testSetup(t)
	// "foo=bar" triggers a parse error; "parent=ffffffff" triggers a resolve error
	fs, parseErrs := Parse("foo=bar parent=ffffffff status=active")
	if len(parseErrs) != 1 {
		t.Fatalf("expected 1 parse error, got %d: %v", len(parseErrs), parseErrs)
	}
	_, resolveErrs := r.Resolve(context.Background(), fs)
	if len(resolveErrs) != 1 {
		t.Fatalf("expected 1 resolve error, got %d: %v", len(resolveErrs), resolveErrs)
	}
}

func TestIntegration_TitleExtraction(t *testing.T) {
	fs, parseErrs := Parse("Implement auth middleware project=backend +api priority=3")
	if len(parseErrs) != 0 {
		t.Fatalf("parse errors: %v", parseErrs)
	}
	if fs.Title() != "Implement auth middleware" {
		t.Fatalf("expected title %q, got %q", "Implement auth middleware", fs.Title())
	}
}
