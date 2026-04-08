package filter

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

// testResolver creates an in-memory SQLite store and returns a Resolver wired
// to its ProjectRepo and TaskRepo.
func testResolver(t *testing.T) (*Resolver, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	taskRepo := sqlite.NewTaskRepo(store.DB())
	return NewResolver(taskRepo), store
}

func TestResolve_DefaultStatuses(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Statuses) != 2 || tf.Statuses[0] != "pending" || tf.Statuses[1] != "active" {
		t.Fatalf("expected default statuses [pending active], got %v", tf.Statuses)
	}
}

func TestResolve_ExplicitStatuses(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "status", Value: "completed"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Statuses) != 1 || tf.Statuses[0] != "completed" {
		t.Fatalf("expected [completed], got %v", tf.Statuses)
	}
}

func TestResolve_MultipleStatuses(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "status", Value: "pending,active,completed"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(tf.Statuses))
	}
}

func TestResolve_ProjectByID(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "project", Value: "default"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.ProjectID == nil {
		t.Fatal("expected ProjectID to be set")
	}
	if *tf.ProjectID != "default" {
		t.Fatalf("expected ProjectID=%q, got %q", "default", *tf.ProjectID)
	}
}

func TestResolve_ProjectStringValue(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "project", Value: "nonexistent"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.ProjectID == nil || *tf.ProjectID != "nonexistent" {
		t.Fatalf("expected ProjectID=%q, got %v", "nonexistent", tf.ProjectID)
	}
}

func TestResolve_PrioritySingle(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "priority", Value: "3"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 3 {
		t.Fatalf("expected PriorityMin=3, got %v", tf.PriorityMin)
	}
	if tf.PriorityMax == nil || *tf.PriorityMax != 3 {
		t.Fatalf("expected PriorityMax=3, got %v", tf.PriorityMax)
	}
}

func TestResolve_PriorityRange(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "priority", Value: "2..4"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 2 {
		t.Fatalf("expected PriorityMin=2, got %v", tf.PriorityMin)
	}
	if tf.PriorityMax == nil || *tf.PriorityMax != 4 {
		t.Fatalf("expected PriorityMax=4, got %v", tf.PriorityMax)
	}
}

func TestResolve_PriorityNamed(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "priority", Value: "high"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 3 {
		t.Fatalf("expected PriorityMin=3 (high), got %v", tf.PriorityMin)
	}
}

func TestResolve_DueSingle(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "due", Value: "2026-04-10"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// Single due date sets DueBefore to end of that day
	wantDate := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if tf.DueAfter == nil || !tf.DueAfter.Equal(wantDate) {
		t.Fatalf("expected DueAfter=%v, got %v", wantDate, tf.DueAfter)
	}
	wantEnd := wantDate.AddDate(0, 0, 1)
	if tf.DueBefore == nil || !tf.DueBefore.Equal(wantEnd) {
		t.Fatalf("expected DueBefore=%v, got %v", wantEnd, tf.DueBefore)
	}
}

func TestResolve_DueRange(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "due", Value: "2026-04-01..2026-04-10"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	wantAfter := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if tf.DueAfter == nil || !tf.DueAfter.Equal(wantAfter) {
		t.Fatalf("expected DueAfter=%v, got %v", wantAfter, tf.DueAfter)
	}
	if tf.DueBefore == nil || !tf.DueBefore.Equal(wantBefore) {
		t.Fatalf("expected DueBefore=%v, got %v", wantBefore, tf.DueBefore)
	}
}

func TestResolve_Tags(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Tags: []TagFilter{
			{Name: "api", Exclude: false},
			{Name: "docs", Exclude: true},
			{Name: "frontend", Exclude: false},
		},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Tags) != 2 || tf.Tags[0] != "api" || tf.Tags[1] != "frontend" {
		t.Fatalf("expected Tags=[api frontend], got %v", tf.Tags)
	}
	if len(tf.ExcludeTags) != 1 || tf.ExcludeTags[0] != "docs" {
		t.Fatalf("expected ExcludeTags=[docs], got %v", tf.ExcludeTags)
	}
}

func TestResolve_WaitingTrue(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "waiting", Value: "true"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.WaitingOnly == nil || !*tf.WaitingOnly {
		t.Fatal("expected WaitingOnly=true")
	}
}

func TestResolve_WaitingFalse(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "waiting", Value: "false"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.WaitingOnly == nil || *tf.WaitingOnly {
		t.Fatal("expected WaitingOnly=false")
	}
}

func TestResolve_ParentShortID(t *testing.T) {
	r, store := testResolver(t)
	ctx := context.Background()

	// Create a task to use as parent
	taskRepo := sqlite.NewTaskRepo(store.DB())
	parent := &domain.Task{
		ID:      uuid.New(),
		ShortID: "a3f8b2c1",
		Title:   "Parent task",
		Status:  "pending",
		Version: 1,
	}
	if err := taskRepo.Create(ctx, parent); err != nil {
		t.Fatalf("creating parent task: %v", err)
	}

	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "parent", Value: "a3f8b2c1"}},
	}

	tf, errs := r.Resolve(ctx, fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.ParentID == nil || *tf.ParentID != parent.ID {
		t.Fatalf("expected ParentID=%v, got %v", parent.ID, tf.ParentID)
	}
}

func TestResolve_TreeShortID(t *testing.T) {
	r, store := testResolver(t)
	ctx := context.Background()

	taskRepo := sqlite.NewTaskRepo(store.DB())
	root := &domain.Task{
		ID:      uuid.New(),
		ShortID: "deadbeef",
		Title:   "Root task",
		Status:  "pending",
		Version: 1,
	}
	if err := taskRepo.Create(ctx, root); err != nil {
		t.Fatalf("creating root task: %v", err)
	}

	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "tree", Value: "deadbeef"}},
	}

	tf, errs := r.Resolve(ctx, fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.RootID == nil || *tf.RootID != root.ID {
		t.Fatalf("expected RootID=%v, got %v", root.ID, tf.RootID)
	}
}

func TestResolve_ParentNotFound(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "parent", Value: "ffffffff"}},
	}

	_, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestResolve_MultipleErrors(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "parent", Value: "ffffffff"},
			{Key: "tree", Value: "eeeeeeee"},
		},
	}

	_, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
}
