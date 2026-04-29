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
func statusOf(test *testing.T, svc *TaskService, task *domain.Task, target string) *domain.Task {
	test.Helper()
	ctx := context.Background()

	switch target {
	case "pending":
		// initial status; nothing to do.
		return task
	case "active":
		got, err := svc.Start(ctx, task.ShortID, task.Version, "")

		if err != nil {
			test.Fatalf("Start: %v", err)
		}

		return got
	case "completed":
		got, startErr := svc.Start(ctx, task.ShortID, task.Version, "")

		if startErr != nil {
			test.Fatalf("Start: %v", startErr)
		}

		got, completeErr := svc.Complete(ctx, got.ShortID, got.Version)

		if completeErr != nil {
			test.Fatalf("Complete: %v", completeErr)
		}

		return got
	case "deleted":
		got, err := svc.Delete(ctx, task.ShortID, task.Version)

		if err != nil {
			test.Fatalf("Delete: %v", err)
		}

		return got
	}

	test.Fatalf("unknown target status %q", target)
	return nil
}

func makeChild(test *testing.T, svc *TaskService, title string, parentID uuid.UUID) *domain.Task {
	test.Helper()
	task := newMinimalTask(title)
	task.ParentID = &parentID
	mustCreateTask(test, svc, task)
	return task
}

func TestSummarizeSubtree_Leaf(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	leaf := newMinimalTask("Leaf")
	mustCreateTask(test, env.taskSvc, leaf)

	got, err := env.taskSvc.SummarizeSubtree(ctx, leaf.ID)

	if err != nil {
		test.Fatalf("SummarizeSubtree: %v", err)
	}

	if got.Task.ID != leaf.ID {
		test.Fatalf("returned block must be the root, got %v", got.Task.ID)
	}

	if got.Rollup.Total != 0 || got.Rollup.Done != 0 {
		test.Fatalf("leaf rollup must be 0/0, got %d/%d", got.Rollup.Done, got.Rollup.Total)
	}

	if got.Rollup.Percent != 0.0 {
		test.Fatalf("leaf percent must be 0.0, got %v", got.Rollup.Percent)
	}

	if len(got.Rollup.StatusCounts) != 0 {
		test.Fatalf("leaf StatusCounts must be empty, got %v", got.Rollup.StatusCounts)
	}
}

func TestSummarizeSubtree_OneLevel(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	root := newMinimalTask("Root")
	mustCreateTask(test, env.taskSvc, root)

	child1 := makeChild(test, env.taskSvc, "C1", root.ID)
	child2 := makeChild(test, env.taskSvc, "C2", root.ID)
	child3 := makeChild(test, env.taskSvc, "C3", root.ID)

	statusOf(test, env.taskSvc, child1, "active")
	statusOf(test, env.taskSvc, child2, "completed")
	_ = child3 // remains pending

	got, err := env.taskSvc.SummarizeSubtree(ctx, root.ID)

	if err != nil {
		test.Fatalf("SummarizeSubtree: %v", err)
	}

	if got.Rollup.Total != 3 || got.Rollup.Done != 1 {
		test.Fatalf("want Done=1 Total=3, got Done=%d Total=%d", got.Rollup.Done, got.Rollup.Total)
	}

	if got.Rollup.Percent < 0.333 || got.Rollup.Percent > 0.334 {
		test.Fatalf("want Percent ≈ 0.333, got %v", got.Rollup.Percent)
	}

	// Kanban order should be pending, active, completed.
	if !hasOrder(got.Rollup.StatusCounts, []string{"pending", "active", "completed"}) {
		test.Fatalf("unexpected order: %v", got.Rollup.StatusCounts)
	}
}

func TestSummarizeSubtree_DeepTree(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	root := newMinimalTask("Root")
	mustCreateTask(test, env.taskSvc, root)

	nodeA := makeChild(test, env.taskSvc, "a", root.ID)
	nodeB := makeChild(test, env.taskSvc, "b", nodeA.ID)
	nodeC := makeChild(test, env.taskSvc, "c", nodeB.ID)
	nodeD := makeChild(test, env.taskSvc, "d", nodeC.ID)
	deletedChild := makeChild(test, env.taskSvc, "deleted", root.ID)

	statusOf(test, env.taskSvc, nodeB, "active")
	statusOf(test, env.taskSvc, nodeD, "completed")
	statusOf(test, env.taskSvc, deletedChild, "deleted")

	got, err := env.taskSvc.SummarizeSubtree(ctx, root.ID)

	if err != nil {
		test.Fatalf("SummarizeSubtree: %v", err)
	}

	// Descendants: a (pending), b (active), c (pending), d (completed),
	// deletedChild (deleted — excluded). Total = 4, Done = 1.
	if got.Rollup.Total != 4 || got.Rollup.Done != 1 {
		test.Fatalf("want Done=1 Total=4, got Done=%d Total=%d (counts: %v)",
			got.Rollup.Done, got.Rollup.Total, got.Rollup.StatusCounts)
	}

	for _, statusCount := range got.Rollup.StatusCounts {
		if statusCount.Name == "deleted" {
			test.Fatalf("deleted bucket must be absent: %v", got.Rollup.StatusCounts)
		}
	}
}

func TestSummarizeSubtree_NotFound(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	_, err := env.taskSvc.SummarizeSubtree(ctx, uuid.New())

	if err == nil {
		test.Fatal("expected error for unknown rootID")
	}

	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSummarizeBlocks_NilFilterReturnsRoots(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	root1 := newMinimalTask("Root 1")
	mustCreateTask(test, env.taskSvc, root1)
	makeChild(test, env.taskSvc, "r1c1", root1.ID)
	r1c2 := makeChild(test, env.taskSvc, "r1c2", root1.ID)
	statusOf(test, env.taskSvc, r1c2, "completed")

	root2 := newMinimalTask("Root 2")
	mustCreateTask(test, env.taskSvc, root2)
	r2c1 := makeChild(test, env.taskSvc, "r2c1", root2.ID)
	statusOf(test, env.taskSvc, r2c1, "completed")

	blocks, err := env.taskSvc.SummarizeBlocks(ctx, nil, false)

	if err != nil {
		test.Fatalf("SummarizeBlocks: %v", err)
	}

	if len(blocks) != 2 {
		test.Fatalf("want 2 root blocks, got %d", len(blocks))
	}

	rollupByID := make(map[uuid.UUID]domain.Rollup)

	for _, block := range blocks {
		rollupByID[block.Task.ID] = block.Rollup
	}

	r1roll, ok := rollupByID[root1.ID]

	if !ok {
		test.Fatalf("expected r1 in blocks")
	}

	if r1roll.Total != 2 || r1roll.Done != 1 {
		test.Fatalf("r1 want 1/2, got %d/%d", r1roll.Done, r1roll.Total)
	}

	r2roll := rollupByID[root2.ID]

	if r2roll.Total != 1 || r2roll.Done != 1 {
		test.Fatalf("r2 want 1/1, got %d/%d", r2roll.Done, r2roll.Total)
	}
}

func TestSummarizeBlocks_FilterScopesBoth(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	storyLevel := "story"
	taskLevel := "task"

	root := newMinimalTask("Root milestone")
	mustCreateTask(test, env.taskSvc, root)

	storyA := newMinimalTask("Story A")
	storyA.ParentID = &root.ID
	storyA.Level = &storyLevel
	mustCreateTask(test, env.taskSvc, storyA)

	storyB := newMinimalTask("Story B")
	storyB.ParentID = &root.ID
	storyB.Level = &storyLevel
	mustCreateTask(test, env.taskSvc, storyB)

	// Sub-stories under storyA.
	subStory := newMinimalTask("Sub-story under A")
	subStory.ParentID = &storyA.ID
	subStory.Level = &storyLevel
	mustCreateTask(test, env.taskSvc, subStory)

	// Non-story descendants under storyA — must be excluded when filter
	// scopes descendants too.
	taskUnderA := newMinimalTask("Task under A")
	taskUnderA.ParentID = &storyA.ID
	taskUnderA.Level = &taskLevel
	mustCreateTask(test, env.taskSvc, taskUnderA)

	statusOf(test, env.taskSvc, subStory, "completed")
	// taskUnderA stays pending.

	expr := &domain.TermFilter{TaskFilter: domain.TaskFilter{Levels: []string{"story"}}}
	blocks, err := env.taskSvc.SummarizeBlocks(ctx, expr, false)

	if err != nil {
		test.Fatalf("SummarizeBlocks: %v", err)
	}

	// Three story-level tasks total. Each becomes a block, with descendant
	// counts restricted to story-level descendants.
	if len(blocks) != 3 {
		test.Fatalf("want 3 story blocks, got %d (%+v)", len(blocks), blockShortIDs(blocks))
	}

	rollupByID := make(map[uuid.UUID]domain.Rollup)

	for _, block := range blocks {
		rollupByID[block.Task.ID] = block.Rollup
	}

	// storyA's descendants: subStory (story, completed), taskUnderA (task, excluded by filter).
	rollA := rollupByID[storyA.ID]

	if rollA.Total != 1 || rollA.Done != 1 {
		test.Fatalf("storyA scoped descendants want 1/1, got %d/%d (%v)", rollA.Done, rollA.Total, rollA.StatusCounts)
	}

	rollB := rollupByID[storyB.ID]

	if rollB.Total != 0 {
		test.Fatalf("storyB has no descendants, got Total=%d", rollB.Total)
	}

	rollSub := rollupByID[subStory.ID]

	if rollSub.Total != 0 {
		test.Fatalf("subStory leaf rollup want 0, got %d", rollSub.Total)
	}
}

func TestSummarizeBlocks_FilterFull(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	storyLevel := "story"
	taskLevel := "task"

	root := newMinimalTask("Root")
	mustCreateTask(test, env.taskSvc, root)

	storyA := newMinimalTask("Story A")
	storyA.ParentID = &root.ID
	storyA.Level = &storyLevel
	mustCreateTask(test, env.taskSvc, storyA)

	subStory := newMinimalTask("Sub-story")
	subStory.ParentID = &storyA.ID
	subStory.Level = &storyLevel
	mustCreateTask(test, env.taskSvc, subStory)
	statusOf(test, env.taskSvc, subStory, "completed")

	taskUnderA := newMinimalTask("Task under A")
	taskUnderA.ParentID = &storyA.ID
	taskUnderA.Level = &taskLevel
	mustCreateTask(test, env.taskSvc, taskUnderA)

	expr := &domain.TermFilter{TaskFilter: domain.TaskFilter{Levels: []string{"story"}}}
	blocks, err := env.taskSvc.SummarizeBlocks(ctx, expr, true)

	if err != nil {
		test.Fatalf("SummarizeBlocks: %v", err)
	}

	rollupByID := make(map[uuid.UUID]domain.Rollup)

	for _, block := range blocks {
		rollupByID[block.Task.ID] = block.Rollup
	}

	// With full=true, storyA must include taskUnderA in Total.
	rollA := rollupByID[storyA.ID]

	if rollA.Total != 2 || rollA.Done != 1 {
		test.Fatalf("storyA full descendants want 1/2, got %d/%d (%v)", rollA.Done, rollA.Total, rollA.StatusCounts)
	}
}

func TestSummarizeBlocks_FilterScopesByTag(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	tagRepo := sqlite.NewTagRepo(env.store.DB())
	urgent := &domain.Tag{ID: uuid.New(), Name: "urgent"}

	if err := tagRepo.Create(ctx, urgent); err != nil {
		test.Fatalf("create tag: %v", err)
	}

	// Two roots, each with two children. Only one child per root carries
	// the urgent tag; SummarizeBlocks with +urgent and full=false must
	// scope both block selection and descendant counting to urgent tasks.
	rootA := newMinimalTask("Root A")
	mustCreateTask(test, env.taskSvc, rootA)

	if tagErr := tagRepo.AssignToTask(ctx, rootA.ID, urgent.ID); tagErr != nil {
		test.Fatalf("tag rootA: %v", tagErr)
	}

	urgentChild := makeChild(test, env.taskSvc, "urgent under A", rootA.ID)

	if tagErr := tagRepo.AssignToTask(ctx, urgentChild.ID, urgent.ID); tagErr != nil {
		test.Fatalf("tag urgent child: %v", tagErr)
	}

	calmChild := makeChild(test, env.taskSvc, "calm under A", rootA.ID)
	_ = calmChild

	rootB := newMinimalTask("Root B (untagged)")
	mustCreateTask(test, env.taskSvc, rootB)
	makeChild(test, env.taskSvc, "calm under B", rootB.ID)

	expr := &domain.TermFilter{TaskFilter: domain.TaskFilter{Tags: []string{"urgent"}}}

	scoped, scopedErr := env.taskSvc.SummarizeBlocks(ctx, expr, false)

	if scopedErr != nil {
		test.Fatalf("SummarizeBlocks scoped: %v", scopedErr)
	}

	rollupByID := make(map[uuid.UUID]domain.Rollup, len(scoped))

	for _, block := range scoped {
		rollupByID[block.Task.ID] = block.Rollup
	}

	rollA, ok := rollupByID[rootA.ID]

	if !ok {
		test.Fatalf("rootA missing from scoped blocks: %v", blockShortIDs(scoped))
	}

	if rollA.Total != 1 {
		test.Fatalf("descendant tag filter must drop calmChild: want Total=1, got %d (%+v)",
			rollA.Total, rollA.StatusCounts)
	}

	if _, untaggedShown := rollupByID[rootB.ID]; untaggedShown {
		test.Fatalf("rootB must not appear: blocks=%v", blockShortIDs(scoped))
	}

	full, fullErr := env.taskSvc.SummarizeBlocks(ctx, expr, true)

	if fullErr != nil {
		test.Fatalf("SummarizeBlocks full: %v", fullErr)
	}

	rollupByID = make(map[uuid.UUID]domain.Rollup, len(full))

	for _, block := range full {
		rollupByID[block.Task.ID] = block.Rollup
	}

	rollA = rollupByID[rootA.ID]

	if rollA.Total != 2 {
		test.Fatalf("full=true must keep calmChild: want Total=2, got %d (%+v)",
			rollA.Total, rollA.StatusCounts)
	}
}

func TestSummarizeBlocks_FilterTreePredicateNoOpsForDescendants(test *testing.T) {
	// tree=<X> selects blocks under X via the SQL evaluator. Each block's
	// descendants are by definition also under X (transitivity), so the
	// in-memory descendant pass treats RootID as match-all. Verifies that
	// the spec'd behavior — "filter scopes both block selection AND
	// descendant counting" — degenerates correctly when the predicate
	// is structurally satisfied.
	env := testTaskEnv(test)
	ctx := context.Background()

	root := newMinimalTask("Root")
	mustCreateTask(test, env.taskSvc, root)

	storyA := makeChild(test, env.taskSvc, "Story A", root.ID)
	leaf := makeChild(test, env.taskSvc, "Leaf under A", storyA.ID)
	_ = leaf

	expr := &domain.TermFilter{TaskFilter: domain.TaskFilter{RootID: &root.ID}}
	blocks, err := env.taskSvc.SummarizeBlocks(ctx, expr, false)

	if err != nil {
		test.Fatalf("SummarizeBlocks: %v", err)
	}

	for _, block := range blocks {
		if block.Task.ID == storyA.ID {
			if block.Rollup.Total != 1 {
				test.Fatalf("storyA descendants under root must count leaf: want Total=1, got %d", block.Rollup.Total)
			}

			return
		}
	}

	test.Fatalf("storyA missing from blocks: %v", blockShortIDs(blocks))
}

func TestSummarizeBlocks_EmptyResult(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	root := newMinimalTask("Root")
	mustCreateTask(test, env.taskSvc, root)

	expr := &domain.TermFilter{TaskFilter: domain.TaskFilter{Levels: []string{"nonexistent_level"}}}
	blocks, err := env.taskSvc.SummarizeBlocks(ctx, expr, false)

	if err != nil {
		test.Fatalf("SummarizeBlocks: %v", err)
	}

	if len(blocks) != 0 {
		test.Fatalf("want empty result, got %d", len(blocks))
	}
}

func hasOrder(got []domain.StatusCount, want []string) bool {
	if len(got) < len(want) {
		return false
	}

	idx := make(map[string]int, len(got))

	for index, statusCount := range got {
		idx[statusCount.Name] = index
	}

	last := -1

	for _, name := range want {
		pos, ok := idx[name]

		if !ok || pos <= last {
			return false
		}

		last = pos
	}

	return true
}

func blockShortIDs(blocks []*domain.SummaryBlock) []string {
	ids := make([]string, 0, len(blocks))

	for _, block := range blocks {
		ids = append(ids, block.Task.ShortID)
	}

	return ids
}
