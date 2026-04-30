package filter

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

func testSetup(test *testing.T) (*Resolver, *sqlite.TaskRepo) {
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
	return NewResolver(taskRepo, projects, []string{"pending", "active"}), taskRepo
}

func TestIntegration_DefaultFilter(test *testing.T) {
	resolver, _ := testSetup(test)
	fs, parseErrs := Parse("")
	if len(parseErrs) != 0 {
		test.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := resolver.Resolve(context.Background(), fs)
	if len(resolveErrs) != 0 {
		test.Fatalf("resolve errors: %v", resolveErrs)
	}
	if len(tf.Statuses) != 2 || tf.Statuses[0] != "pending" || tf.Statuses[1] != "active" {
		test.Fatalf("expected default statuses [pending active], got %v", tf.Statuses)
	}
}

func TestIntegration_ComplexFilter(test *testing.T) {
	resolver, _ := testSetup(test)
	fs, parseErrs := Parse("status=completed project=default priority=2..4 +api -docs")
	if len(parseErrs) != 0 {
		test.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := resolver.Resolve(context.Background(), fs)
	if len(resolveErrs) != 0 {
		test.Fatalf("resolve errors: %v", resolveErrs)
	}

	if len(tf.Statuses) != 1 || tf.Statuses[0] != "completed" {
		test.Fatalf("expected [completed], got %v", tf.Statuses)
	}
	if tf.ProjectID == nil {
		test.Fatal("expected ProjectID to be set")
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 2 {
		test.Fatalf("expected PriorityMin=2, got %v", tf.PriorityMin)
	}
	if tf.PriorityMax == nil || *tf.PriorityMax != 4 {
		test.Fatalf("expected PriorityMax=4, got %v", tf.PriorityMax)
	}
	if len(tf.Tags) != 1 || tf.Tags[0] != "api" {
		test.Fatalf("expected Tags=[api], got %v", tf.Tags)
	}
	if len(tf.ExcludeTags) != 1 || tf.ExcludeTags[0] != "docs" {
		test.Fatalf("expected ExcludeTags=[docs], got %v", tf.ExcludeTags)
	}
}

func TestIntegration_ParentFilter(test *testing.T) {
	resolver, taskRepo := testSetup(test)
	ctx := context.Background()

	parent := &domain.Task{
		ID:      uuid.New(),
		ShortID: "abcd1234",
		Title:   "Parent",
		Status:  "pending",
		Version: 1,
	}

	if createErr := taskRepo.Create(ctx, parent); createErr != nil {
		test.Fatalf("creating task: %v", createErr)
	}

	fs, parseErrs := Parse("parent=abcd1234 status=active")
	if len(parseErrs) != 0 {
		test.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := resolver.Resolve(ctx, fs)
	if len(resolveErrs) != 0 {
		test.Fatalf("resolve errors: %v", resolveErrs)
	}
	if tf.ParentID == nil || *tf.ParentID != parent.ID {
		test.Fatalf("expected ParentID=%v, got %v", parent.ID, tf.ParentID)
	}
	if len(tf.Statuses) != 1 || tf.Statuses[0] != "active" {
		test.Fatalf("expected [active], got %v", tf.Statuses)
	}
}

func TestIntegration_TreeFilter(test *testing.T) {
	resolver, taskRepo := testSetup(test)
	ctx := context.Background()

	root := &domain.Task{
		ID:      uuid.New(),
		ShortID: "deadbeef",
		Title:   "Root",
		Status:  "pending",
		Version: 1,
	}

	if createErr := taskRepo.Create(ctx, root); createErr != nil {
		test.Fatalf("creating task: %v", createErr)
	}

	fs, parseErrs := Parse("tree=deadbeef")
	if len(parseErrs) != 0 {
		test.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := resolver.Resolve(ctx, fs)
	if len(resolveErrs) != 0 {
		test.Fatalf("resolve errors: %v", resolveErrs)
	}
	if tf.RootID == nil || *tf.RootID != root.ID {
		test.Fatalf("expected RootID=%v, got %v", root.ID, tf.RootID)
	}
}

func TestIntegration_OrderFilterMatchesRows(test *testing.T) {
	resolver, taskRepo := testSetup(test)
	ctx := context.Background()

	orderOne := 1.0
	orderFive := 5.0
	lo := &domain.Task{
		ID: uuid.New(), ShortID: "11111111", Title: "lo", Status: "pending",
		Version: 1, Order: &orderOne,
	}
	hi := &domain.Task{
		ID: uuid.New(), ShortID: "22222222", Title: "hi", Status: "pending",
		Version: 1, Order: &orderFive,
	}
	nul := &domain.Task{
		ID: uuid.New(), ShortID: "33333333", Title: "null", Status: "pending",
		Version: 1,
	}
	for _, task := range []*domain.Task{lo, hi, nul} {
		if seedErr := taskRepo.Create(ctx, task); seedErr != nil {
			test.Fatalf("seed %s: %v", task.ShortID, seedErr)
		}
	}

	test.Run("exact match", func(test *testing.T) {
		fs, _ := Parse("order=1 status=pending")
		tf, errs := resolver.Resolve(ctx, fs)
		if len(errs) != 0 {
			test.Fatalf("resolve: %v", errs)
		}
		tasks, listErr := taskRepo.List(ctx, &domain.TermFilter{TaskFilter: *tf})

		if listErr != nil {
			test.Fatalf("list: %v", listErr)
		}
		if len(tasks) != 1 || tasks[0].ShortID != lo.ShortID {
			test.Fatalf("expected 1 task matching lo, got %d", len(tasks))
		}
	})

	test.Run("range match", func(test *testing.T) {
		fs, _ := Parse("order=0..3 status=pending")
		tf, errs := resolver.Resolve(ctx, fs)
		if len(errs) != 0 {
			test.Fatalf("resolve: %v", errs)
		}
		tasks, listErr := taskRepo.List(ctx, &domain.TermFilter{TaskFilter: *tf})

		if listErr != nil {
			test.Fatalf("list: %v", listErr)
		}
		if len(tasks) != 1 || tasks[0].ShortID != lo.ShortID {
			test.Fatalf("expected 1 task in (0..3), got %d", len(tasks))
		}
	})

	test.Run("empty matches null", func(test *testing.T) {
		fs, _ := Parse("order= status=pending")
		tf, errs := resolver.Resolve(ctx, fs)
		if len(errs) != 0 {
			test.Fatalf("resolve: %v", errs)
		}
		tasks, listErr := taskRepo.List(ctx, &domain.TermFilter{TaskFilter: *tf})

		if listErr != nil {
			test.Fatalf("list: %v", listErr)
		}
		if len(tasks) != 1 || tasks[0].ShortID != nul.ShortID {
			test.Fatalf("expected 1 task with null order, got %d", len(tasks))
		}
	})
}

func TestIntegration_ParseAndResolveErrors(test *testing.T) {
	resolver, _ := testSetup(test)
	// "foo=bar" triggers a parse error; "parent=ffffffff" triggers a resolve error
	fs, parseErrs := Parse("foo=bar parent=ffffffff status=active")
	if len(parseErrs) != 1 {
		test.Fatalf("expected 1 parse error, got %d: %v", len(parseErrs), parseErrs)
	}
	_, resolveErrs := resolver.Resolve(context.Background(), fs)
	if len(resolveErrs) != 1 {
		test.Fatalf("expected 1 resolve error, got %d: %v", len(resolveErrs), resolveErrs)
	}
}

func TestIntegration_TitleExtraction(test *testing.T) {
	fs, parseErrs := Parse("Implement auth middleware project=backend +api priority=3")
	if len(parseErrs) != 0 {
		test.Fatalf("parse errors: %v", parseErrs)
	}
	if fs.Title() != "Implement auth middleware" {
		test.Fatalf("expected title %q, got %q", "Implement auth middleware", fs.Title())
	}
}
