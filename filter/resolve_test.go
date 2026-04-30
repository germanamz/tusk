package filter

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// defaultProjectUUID is the UUID seeded for the "default" project in tests.
var defaultProjectUUID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// testResolver creates an in-memory SQLite store and returns a Resolver wired
// to its TaskRepo and a fake ProjectLookup containing one "default" project.
func testResolver(test *testing.T) (*Resolver, *sqlite.Store) {
	test.Helper()

	store, storeErr := sqlite.New(":memory:", migrations.FS)

	if storeErr != nil {
		test.Fatalf("opening test store: %v", storeErr)
	}

	test.Cleanup(func() { store.Close() })

	taskRepo := sqlite.NewTaskRepo(store.DB())
	projects := &fakeProjectLookup{
		byName: map[string]*domain.Project{
			"default": {ID: defaultProjectUUID, Name: "default"},
		},
	}
	return NewResolver(taskRepo, projects, []string{"pending", "active"}), store
}

func TestResolve_DefaultStatuses(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Statuses) != 2 || tf.Statuses[0] != "pending" || tf.Statuses[1] != "active" {
		test.Fatalf("expected default statuses [pending active], got %v", tf.Statuses)
	}
}

func TestResolve_ExplicitStatuses(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "status", Value: "completed"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Statuses) != 1 || tf.Statuses[0] != "completed" {
		test.Fatalf("expected [completed], got %v", tf.Statuses)
	}
}

func TestResolve_MultipleStatuses(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "status", Value: "pending,active,completed"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Statuses) != 3 {
		test.Fatalf("expected 3 statuses, got %d", len(tf.Statuses))
	}
}

func TestResolve_ProjectByID(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "project", Value: "default"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if tf.ProjectID == nil {
		test.Fatal("expected ProjectID to be set")
	}
	if *tf.ProjectID != defaultProjectUUID {
		test.Fatalf("expected ProjectID=%v, got %v", defaultProjectUUID, *tf.ProjectID)
	}
}

func TestResolve_ProjectNotFound(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "project", Value: "nonexistent"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 1 {
		test.Fatalf("expected 1 error for unknown project, got %d: %v", len(errs), errs)
	}
	if tf.ProjectID != nil {
		test.Fatalf("expected ProjectID nil after error, got %v", tf.ProjectID)
	}
}

func TestResolve_PrioritySingle(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "priority", Value: "3"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 3 {
		test.Fatalf("expected PriorityMin=3, got %v", tf.PriorityMin)
	}
	if tf.PriorityMax == nil || *tf.PriorityMax != 3 {
		test.Fatalf("expected PriorityMax=3, got %v", tf.PriorityMax)
	}
}

func TestResolve_PriorityRange(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "priority", Value: "2..4"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 2 {
		test.Fatalf("expected PriorityMin=2, got %v", tf.PriorityMin)
	}
	if tf.PriorityMax == nil || *tf.PriorityMax != 4 {
		test.Fatalf("expected PriorityMax=4, got %v", tf.PriorityMax)
	}
}

func TestResolve_OrderSingle(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "order", Value: "2.5"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if tf.OrderMin == nil || *tf.OrderMin != 2.5 {
		test.Fatalf("expected OrderMin=2.5, got %v", tf.OrderMin)
	}
	if tf.OrderMax == nil || *tf.OrderMax != 2.5 {
		test.Fatalf("expected OrderMax=2.5, got %v", tf.OrderMax)
	}
}

func TestResolve_OrderRange(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "order", Value: "1..5"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if tf.OrderMin == nil || *tf.OrderMin != 1 {
		test.Fatalf("expected OrderMin=1, got %v", tf.OrderMin)
	}
	if tf.OrderMax == nil || *tf.OrderMax != 5 {
		test.Fatalf("expected OrderMax=5, got %v", tf.OrderMax)
	}
}

func TestResolve_OrderEmptyIsNull(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "order", Value: ""}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if tf.OrderIsNull == nil || !*tf.OrderIsNull {
		test.Fatalf("expected OrderIsNull=true, got %v", tf.OrderIsNull)
	}
	if tf.OrderMin != nil || tf.OrderMax != nil {
		test.Fatalf("expected Min/Max to be nil when IS NULL, got min=%v max=%v", tf.OrderMin, tf.OrderMax)
	}
}

func TestResolve_PriorityNamed(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "priority", Value: "high"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 3 {
		test.Fatalf("expected PriorityMin=3 (high), got %v", tf.PriorityMin)
	}
}

func TestResolve_DueSingle(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "due", Value: "2026-04-10"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	// Single due date sets DueBefore to end of that day
	wantDate := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if tf.DueAfter == nil || !tf.DueAfter.Equal(wantDate) {
		test.Fatalf("expected DueAfter=%v, got %v", wantDate, tf.DueAfter)
	}
	wantEnd := wantDate.AddDate(0, 0, 1)
	if tf.DueBefore == nil || !tf.DueBefore.Equal(wantEnd) {
		test.Fatalf("expected DueBefore=%v, got %v", wantEnd, tf.DueBefore)
	}
}

func TestResolve_DueRange(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "due", Value: "2026-04-01..2026-04-10"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	wantAfter := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if tf.DueAfter == nil || !tf.DueAfter.Equal(wantAfter) {
		test.Fatalf("expected DueAfter=%v, got %v", wantAfter, tf.DueAfter)
	}
	if tf.DueBefore == nil || !tf.DueBefore.Equal(wantBefore) {
		test.Fatalf("expected DueBefore=%v, got %v", wantBefore, tf.DueBefore)
	}
}

func TestResolve_Tags(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Tags: []TagFilter{
			{Name: "api", Exclude: false},
			{Name: "docs", Exclude: true},
			{Name: "frontend", Exclude: false},
		},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Tags) != 2 || tf.Tags[0] != "api" || tf.Tags[1] != "frontend" {
		test.Fatalf("expected Tags=[api frontend], got %v", tf.Tags)
	}
	if len(tf.ExcludeTags) != 1 || tf.ExcludeTags[0] != "docs" {
		test.Fatalf("expected ExcludeTags=[docs], got %v", tf.ExcludeTags)
	}
}

func TestResolve_WaitingTrue(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "waiting", Value: "true"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if tf.WaitingOnly == nil || !*tf.WaitingOnly {
		test.Fatal("expected WaitingOnly=true")
	}
}

func TestResolve_WaitingFalse(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "waiting", Value: "false"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if tf.WaitingOnly == nil || *tf.WaitingOnly {
		test.Fatal("expected WaitingOnly=false")
	}
}

func TestResolve_ParentShortID(test *testing.T) {
	resolver, store := testResolver(test)
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

	if createErr := taskRepo.Create(ctx, parent); createErr != nil {
		test.Fatalf("creating parent task: %v", createErr)
	}

	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "parent", Value: "a3f8b2c1"}},
	}

	tf, errs := resolver.Resolve(ctx, fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if tf.ParentID == nil || *tf.ParentID != parent.ID {
		test.Fatalf("expected ParentID=%v, got %v", parent.ID, tf.ParentID)
	}
}

func TestResolve_TreeShortID(test *testing.T) {
	resolver, store := testResolver(test)
	ctx := context.Background()

	taskRepo := sqlite.NewTaskRepo(store.DB())
	root := &domain.Task{
		ID:      uuid.New(),
		ShortID: "deadbeef",
		Title:   "Root task",
		Status:  "pending",
		Version: 1,
	}

	if createErr := taskRepo.Create(ctx, root); createErr != nil {
		test.Fatalf("creating root task: %v", createErr)
	}

	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "tree", Value: "deadbeef"}},
	}

	tf, errs := resolver.Resolve(ctx, fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if tf.RootID == nil || *tf.RootID != root.ID {
		test.Fatalf("expected RootID=%v, got %v", root.ID, tf.RootID)
	}
}

func TestResolve_ParentNotFound(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "parent", Value: "ffffffff"}},
	}

	_, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 1 {
		test.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestResolve_MultipleErrors(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "parent", Value: "ffffffff"},
			{Key: "tree", Value: "eeeeeeee"},
		},
	}

	_, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 2 {
		test.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
}

func TestResolve_LevelSingle(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "level", Value: "story"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Levels) != 1 || tf.Levels[0] != "story" {
		test.Fatalf("expected levels [story], got %v", tf.Levels)
	}
}

func TestResolve_LevelMultiple(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "level", Value: "story,task"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Levels) != 2 || tf.Levels[0] != "story" || tf.Levels[1] != "task" {
		test.Fatalf("expected levels [story task], got %v", tf.Levels)
	}
}

func TestResolveExpr_NotLevel(test *testing.T) {
	resolver, _ := testResolver(test)
	expr := AndExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
		NotExpr{Child: TermExpr{Field: &FieldFilter{Key: "level", Value: "spike"}}},
	}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}

	andFilter, ok := result.(*domain.AndFilter)
	if !ok {
		test.Fatalf("expected *domain.AndFilter, got %T", result)
	}
	var notFilter *domain.NotFilter
	for _, child := range andFilter.Children {
		if nf, ok := child.(*domain.NotFilter); ok {
			notFilter = nf
			break
		}
	}
	if notFilter == nil {
		test.Fatalf("expected NotFilter child, got %#v", andFilter.Children)
	}
	termFilter, ok := notFilter.Child.(*domain.TermFilter)
	if !ok {
		test.Fatalf("expected NotFilter.Child == *domain.TermFilter, got %T", notFilter.Child)
	}
	if len(termFilter.Levels) != 1 || termFilter.Levels[0] != "spike" {
		test.Fatalf("expected levels [spike], got %v", termFilter.Levels)
	}
}

func TestResolve_UdaLevelStillRoutesToUDA(test *testing.T) {
	resolver, _ := testResolver(test)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "uda.level", Value: "whatever"}},
	}

	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if got := tf.UDA["level"]; got != "whatever" {
		test.Fatalf("expected UDA[level]=whatever, got %q", got)
	}
	if len(tf.Levels) != 0 {
		test.Fatalf("expected no Levels routing for uda.level, got %v", tf.Levels)
	}
}
