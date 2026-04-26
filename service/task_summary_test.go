package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// statusOf transitions a task through the kanban workflow to the given
// terminal status by calling Start/Complete/Delete on the service. The
// resulting task pointer is returned (with refreshed version).
func statusOf(t *testing.T, svc *TaskService, task *domain.Task, target string) *domain.Task {
	t.Helper()
	ctx := context.Background()
	switch target {
	case "pending":
		// initial status; nothing to do.
		return task
	case "active":
		got, err := svc.Start(ctx, task.ShortID, task.Version, "")
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		return got
	case "completed":
		got, err := svc.Start(ctx, task.ShortID, task.Version, "")
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		got, err = svc.Complete(ctx, got.ShortID, got.Version)
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		return got
	case "deleted":
		got, err := svc.Delete(ctx, task.ShortID, task.Version)
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		return got
	}
	t.Fatalf("unknown target status %q", target)
	return nil
}

func makeChild(t *testing.T, svc *TaskService, title string, parentID uuid.UUID) *domain.Task {
	t.Helper()
	task := newMinimalTask(title)
	task.ParentID = &parentID
	mustCreateTask(t, svc, task)
	return task
}

func TestSummarizeSubtree_Leaf(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	leaf := newMinimalTask("Leaf")
	mustCreateTask(t, env.taskSvc, leaf)

	got, err := env.taskSvc.SummarizeSubtree(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("SummarizeSubtree: %v", err)
	}
	if got.Task.ID != leaf.ID {
		t.Fatalf("returned block must be the root, got %v", got.Task.ID)
	}
	if got.Rollup.Total != 0 || got.Rollup.Done != 0 {
		t.Fatalf("leaf rollup must be 0/0, got %d/%d", got.Rollup.Done, got.Rollup.Total)
	}
	if got.Rollup.Percent != 0.0 {
		t.Fatalf("leaf percent must be 0.0, got %v", got.Rollup.Percent)
	}
	if len(got.Rollup.StatusCounts) != 0 {
		t.Fatalf("leaf StatusCounts must be empty, got %v", got.Rollup.StatusCounts)
	}
}

func TestSummarizeSubtree_OneLevel(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	root := newMinimalTask("Root")
	mustCreateTask(t, env.taskSvc, root)

	c1 := makeChild(t, env.taskSvc, "C1", root.ID)
	c2 := makeChild(t, env.taskSvc, "C2", root.ID)
	c3 := makeChild(t, env.taskSvc, "C3", root.ID)

	statusOf(t, env.taskSvc, c1, "active")
	statusOf(t, env.taskSvc, c2, "completed")
	_ = c3 // remains pending

	got, err := env.taskSvc.SummarizeSubtree(ctx, root.ID)
	if err != nil {
		t.Fatalf("SummarizeSubtree: %v", err)
	}
	if got.Rollup.Total != 3 || got.Rollup.Done != 1 {
		t.Fatalf("want Done=1 Total=3, got Done=%d Total=%d", got.Rollup.Done, got.Rollup.Total)
	}
	if got.Rollup.Percent < 0.333 || got.Rollup.Percent > 0.334 {
		t.Fatalf("want Percent ≈ 0.333, got %v", got.Rollup.Percent)
	}
	// Kanban order should be pending, active, completed.
	if !hasOrder(got.Rollup.StatusCounts, []string{"pending", "active", "completed"}) {
		t.Fatalf("unexpected order: %v", got.Rollup.StatusCounts)
	}
}

func TestSummarizeSubtree_DeepTree(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	root := newMinimalTask("Root")
	mustCreateTask(t, env.taskSvc, root)

	a := makeChild(t, env.taskSvc, "a", root.ID)
	b := makeChild(t, env.taskSvc, "b", a.ID)
	c := makeChild(t, env.taskSvc, "c", b.ID)
	d := makeChild(t, env.taskSvc, "d", c.ID)
	deletedChild := makeChild(t, env.taskSvc, "deleted", root.ID)

	statusOf(t, env.taskSvc, b, "active")
	statusOf(t, env.taskSvc, d, "completed")
	statusOf(t, env.taskSvc, deletedChild, "deleted")

	got, err := env.taskSvc.SummarizeSubtree(ctx, root.ID)
	if err != nil {
		t.Fatalf("SummarizeSubtree: %v", err)
	}
	// Descendants: a (pending), b (active), c (pending), d (completed),
	// deletedChild (deleted — excluded). Total = 4, Done = 1.
	if got.Rollup.Total != 4 || got.Rollup.Done != 1 {
		t.Fatalf("want Done=1 Total=4, got Done=%d Total=%d (counts: %v)",
			got.Rollup.Done, got.Rollup.Total, got.Rollup.StatusCounts)
	}
	for _, sc := range got.Rollup.StatusCounts {
		if sc.Name == "deleted" {
			t.Fatalf("deleted bucket must be absent: %v", got.Rollup.StatusCounts)
		}
	}
}

func TestSummarizeSubtree_NotFound(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	_, err := env.taskSvc.SummarizeSubtree(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected error for unknown rootID")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSummarizeBlocks_NilFilterReturnsRoots(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	r1 := newMinimalTask("Root 1")
	mustCreateTask(t, env.taskSvc, r1)
	makeChild(t, env.taskSvc, "r1c1", r1.ID)
	r1c2 := makeChild(t, env.taskSvc, "r1c2", r1.ID)
	statusOf(t, env.taskSvc, r1c2, "completed")

	r2 := newMinimalTask("Root 2")
	mustCreateTask(t, env.taskSvc, r2)
	r2c1 := makeChild(t, env.taskSvc, "r2c1", r2.ID)
	statusOf(t, env.taskSvc, r2c1, "completed")

	blocks, err := env.taskSvc.SummarizeBlocks(ctx, nil, false)
	if err != nil {
		t.Fatalf("SummarizeBlocks: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("want 2 root blocks, got %d", len(blocks))
	}
	rollupByID := make(map[uuid.UUID]domain.Rollup)
	for _, b := range blocks {
		rollupByID[b.Task.ID] = b.Rollup
	}
	r1roll, ok := rollupByID[r1.ID]
	if !ok {
		t.Fatalf("expected r1 in blocks")
	}
	if r1roll.Total != 2 || r1roll.Done != 1 {
		t.Fatalf("r1 want 1/2, got %d/%d", r1roll.Done, r1roll.Total)
	}
	r2roll := rollupByID[r2.ID]
	if r2roll.Total != 1 || r2roll.Done != 1 {
		t.Fatalf("r2 want 1/1, got %d/%d", r2roll.Done, r2roll.Total)
	}
}

func TestSummarizeBlocks_FilterScopesBoth(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	storyLevel := "story"
	taskLevel := "task"

	root := newMinimalTask("Root milestone")
	mustCreateTask(t, env.taskSvc, root)

	storyA := newMinimalTask("Story A")
	storyA.ParentID = &root.ID
	storyA.Level = &storyLevel
	mustCreateTask(t, env.taskSvc, storyA)

	storyB := newMinimalTask("Story B")
	storyB.ParentID = &root.ID
	storyB.Level = &storyLevel
	mustCreateTask(t, env.taskSvc, storyB)

	// Sub-stories under storyA.
	subStory := newMinimalTask("Sub-story under A")
	subStory.ParentID = &storyA.ID
	subStory.Level = &storyLevel
	mustCreateTask(t, env.taskSvc, subStory)

	// Non-story descendants under storyA — must be excluded when filter
	// scopes descendants too.
	taskUnderA := newMinimalTask("Task under A")
	taskUnderA.ParentID = &storyA.ID
	taskUnderA.Level = &taskLevel
	mustCreateTask(t, env.taskSvc, taskUnderA)

	statusOf(t, env.taskSvc, subStory, "completed")
	// taskUnderA stays pending.

	expr := &domain.TermFilter{TaskFilter: domain.TaskFilter{Levels: []string{"story"}}}
	blocks, err := env.taskSvc.SummarizeBlocks(ctx, expr, false)
	if err != nil {
		t.Fatalf("SummarizeBlocks: %v", err)
	}
	// Three story-level tasks total. Each becomes a block, with descendant
	// counts restricted to story-level descendants.
	if len(blocks) != 3 {
		t.Fatalf("want 3 story blocks, got %d (%+v)", len(blocks), blockShortIDs(blocks))
	}
	rollupByID := make(map[uuid.UUID]domain.Rollup)
	for _, b := range blocks {
		rollupByID[b.Task.ID] = b.Rollup
	}
	// storyA's descendants: subStory (story, completed), taskUnderA (task, excluded by filter).
	rollA := rollupByID[storyA.ID]
	if rollA.Total != 1 || rollA.Done != 1 {
		t.Fatalf("storyA scoped descendants want 1/1, got %d/%d (%v)", rollA.Done, rollA.Total, rollA.StatusCounts)
	}
	rollB := rollupByID[storyB.ID]
	if rollB.Total != 0 {
		t.Fatalf("storyB has no descendants, got Total=%d", rollB.Total)
	}
	rollSub := rollupByID[subStory.ID]
	if rollSub.Total != 0 {
		t.Fatalf("subStory leaf rollup want 0, got %d", rollSub.Total)
	}
}

func TestSummarizeBlocks_FilterFull(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	storyLevel := "story"
	taskLevel := "task"

	root := newMinimalTask("Root")
	mustCreateTask(t, env.taskSvc, root)

	storyA := newMinimalTask("Story A")
	storyA.ParentID = &root.ID
	storyA.Level = &storyLevel
	mustCreateTask(t, env.taskSvc, storyA)

	subStory := newMinimalTask("Sub-story")
	subStory.ParentID = &storyA.ID
	subStory.Level = &storyLevel
	mustCreateTask(t, env.taskSvc, subStory)
	statusOf(t, env.taskSvc, subStory, "completed")

	taskUnderA := newMinimalTask("Task under A")
	taskUnderA.ParentID = &storyA.ID
	taskUnderA.Level = &taskLevel
	mustCreateTask(t, env.taskSvc, taskUnderA)

	expr := &domain.TermFilter{TaskFilter: domain.TaskFilter{Levels: []string{"story"}}}
	blocks, err := env.taskSvc.SummarizeBlocks(ctx, expr, true)
	if err != nil {
		t.Fatalf("SummarizeBlocks: %v", err)
	}
	rollupByID := make(map[uuid.UUID]domain.Rollup)
	for _, b := range blocks {
		rollupByID[b.Task.ID] = b.Rollup
	}
	// With full=true, storyA must include taskUnderA in Total.
	rollA := rollupByID[storyA.ID]
	if rollA.Total != 2 || rollA.Done != 1 {
		t.Fatalf("storyA full descendants want 1/2, got %d/%d (%v)", rollA.Done, rollA.Total, rollA.StatusCounts)
	}
}

func TestSummarizeBlocks_FilterScopesByTag(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	tagRepo := sqlite.NewTagRepo(env.store.DB())
	urgent := &domain.Tag{ID: uuid.New(), Name: "urgent"}
	if err := tagRepo.Create(ctx, urgent); err != nil {
		t.Fatalf("create tag: %v", err)
	}

	// Two roots, each with two children. Only one child per root carries
	// the urgent tag; SummarizeBlocks with +urgent and full=false must
	// scope both block selection and descendant counting to urgent tasks.
	rootA := newMinimalTask("Root A")
	mustCreateTask(t, env.taskSvc, rootA)
	if err := tagRepo.AssignToTask(ctx, rootA.ID, urgent.ID); err != nil {
		t.Fatalf("tag rootA: %v", err)
	}

	urgentChild := makeChild(t, env.taskSvc, "urgent under A", rootA.ID)
	if err := tagRepo.AssignToTask(ctx, urgentChild.ID, urgent.ID); err != nil {
		t.Fatalf("tag urgent child: %v", err)
	}
	calmChild := makeChild(t, env.taskSvc, "calm under A", rootA.ID)
	_ = calmChild

	rootB := newMinimalTask("Root B (untagged)")
	mustCreateTask(t, env.taskSvc, rootB)
	makeChild(t, env.taskSvc, "calm under B", rootB.ID)

	expr := &domain.TermFilter{TaskFilter: domain.TaskFilter{Tags: []string{"urgent"}}}

	scoped, err := env.taskSvc.SummarizeBlocks(ctx, expr, false)
	if err != nil {
		t.Fatalf("SummarizeBlocks scoped: %v", err)
	}
	rollupByID := make(map[uuid.UUID]domain.Rollup, len(scoped))
	for _, b := range scoped {
		rollupByID[b.Task.ID] = b.Rollup
	}
	rollA, ok := rollupByID[rootA.ID]
	if !ok {
		t.Fatalf("rootA missing from scoped blocks: %v", blockShortIDs(scoped))
	}
	if rollA.Total != 1 {
		t.Fatalf("descendant tag filter must drop calmChild: want Total=1, got %d (%+v)",
			rollA.Total, rollA.StatusCounts)
	}
	if _, untaggedShown := rollupByID[rootB.ID]; untaggedShown {
		t.Fatalf("rootB must not appear: blocks=%v", blockShortIDs(scoped))
	}

	full, err := env.taskSvc.SummarizeBlocks(ctx, expr, true)
	if err != nil {
		t.Fatalf("SummarizeBlocks full: %v", err)
	}
	rollupByID = make(map[uuid.UUID]domain.Rollup, len(full))
	for _, b := range full {
		rollupByID[b.Task.ID] = b.Rollup
	}
	rollA = rollupByID[rootA.ID]
	if rollA.Total != 2 {
		t.Fatalf("full=true must keep calmChild: want Total=2, got %d (%+v)",
			rollA.Total, rollA.StatusCounts)
	}
}

func TestSummarizeBlocks_FilterTreePredicateNoOpsForDescendants(t *testing.T) {
	// tree=<X> selects blocks under X via the SQL evaluator. Each block's
	// descendants are by definition also under X (transitivity), so the
	// in-memory descendant pass treats RootID as match-all. Verifies that
	// the spec'd behavior — "filter scopes both block selection AND
	// descendant counting" — degenerates correctly when the predicate
	// is structurally satisfied.
	env := testTaskEnv(t)
	ctx := context.Background()

	root := newMinimalTask("Root")
	mustCreateTask(t, env.taskSvc, root)

	storyA := makeChild(t, env.taskSvc, "Story A", root.ID)
	leaf := makeChild(t, env.taskSvc, "Leaf under A", storyA.ID)
	_ = leaf

	expr := &domain.TermFilter{TaskFilter: domain.TaskFilter{RootID: &root.ID}}
	blocks, err := env.taskSvc.SummarizeBlocks(ctx, expr, false)
	if err != nil {
		t.Fatalf("SummarizeBlocks: %v", err)
	}
	for _, b := range blocks {
		if b.Task.ID == storyA.ID {
			if b.Rollup.Total != 1 {
				t.Fatalf("storyA descendants under root must count leaf: want Total=1, got %d", b.Rollup.Total)
			}
			return
		}
	}
	t.Fatalf("storyA missing from blocks: %v", blockShortIDs(blocks))
}

func TestSummarizeBlocks_EmptyResult(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	root := newMinimalTask("Root")
	mustCreateTask(t, env.taskSvc, root)

	expr := &domain.TermFilter{TaskFilter: domain.TaskFilter{Levels: []string{"nonexistent_level"}}}
	blocks, err := env.taskSvc.SummarizeBlocks(ctx, expr, false)
	if err != nil {
		t.Fatalf("SummarizeBlocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("want empty result, got %d", len(blocks))
	}
}

func hasOrder(got []domain.StatusCount, want []string) bool {
	if len(got) < len(want) {
		return false
	}
	idx := make(map[string]int, len(got))
	for i, sc := range got {
		idx[sc.Name] = i
	}
	last := -1
	for _, name := range want {
		i, ok := idx[name]
		if !ok || i <= last {
			return false
		}
		last = i
	}
	return true
}

func blockShortIDs(blocks []*domain.SummaryBlock) []string {
	ids := make([]string, 0, len(blocks))
	for _, b := range blocks {
		ids = append(ids, b.Task.ShortID)
	}
	return ids
}
