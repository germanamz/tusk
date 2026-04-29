package service

import (
	"context"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// effectiveWeightsTestEnv mirrors testEnv but installs a non-nil UrgencyEngine
// so buildEffectiveWeights and ResolveEffectiveWeights can run end-to-end.
type effectiveWeightsTestEnv struct {
	taskSvc *TaskService
	bundle  *RepoBundle
}

func newEffectiveWeightsEnv(test *testing.T) *effectiveWeightsTestEnv {
	test.Helper()
	bundle, projectRepo, _ := newSeededBundle(test)
	workflowRepo := sqlite.NewWorkflowRepo(bundle.Store.DB())
	resolver, projects := singleBundleResolver(bundle, domain.DefaultProjectUUID)
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	engine := NewUrgencyEngine(defaultWeights())
	taskSvc := NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, engine)
	return &effectiveWeightsTestEnv{taskSvc: taskSvc, bundle: bundle}
}

// setOverrides persists the given urgency overrides on an already-created task
// by re-loading and writing through bundle.Tasks. Phase 2 has no service-layer
// write surface for overrides yet — that ships in Phase 3 — so tests stitch the
// data directly.
func setOverrides(test *testing.T, env *effectiveWeightsTestEnv, taskID uuid.UUID, override *domain.UrgencyOverrides) {
	test.Helper()
	ctx := context.Background()

	task, loadErr := env.bundle.Tasks.GetByID(ctx, taskID)

	if loadErr != nil {
		test.Fatalf("loading task to set overrides: %v", loadErr)
	}

	task.UrgencyOverrides = override

	if updateErr := env.bundle.Tasks.Update(ctx, task); updateErr != nil {
		test.Fatalf("updating task overrides: %v", updateErr)
	}
}

func float64Ptr(v float64) *float64 { return &v }

func TestBuildEffectiveWeightsNoOverrides(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	tasks := make([]*domain.Task, 0, 3)
	for i := 0; i < 3; i++ {
		task := newMinimalTask("root task")
		mustCreateTask(test, env.taskSvc, task)
		tasks = append(tasks, task)
	}

	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)

	got, err := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)

	if err != nil {
		test.Fatalf("buildEffectiveWeights: %v", err)
	}

	if got != nil {
		test.Fatalf("expected nil (fast path) when no overrides + no parents, got %v", got)
	}
}

func TestBuildEffectiveWeightsSelfOnly(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("self override")
	mustCreateTask(test, env.taskSvc, task)
	setOverrides(test, env, task.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(20)})

	reloaded, reloadErr := env.bundle.Tasks.GetByID(ctx, task.ID)

	if reloadErr != nil {
		test.Fatalf("reloading task: %v", reloadErr)
	}

	tasks := []*domain.Task{reloaded}
	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)

	got, err := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)

	if err != nil {
		test.Fatalf("buildEffectiveWeights: %v", err)
	}

	weights, ok := got[task.ID]

	if !ok {
		test.Fatalf("expected entry for task %v, got %v", task.ID, got)
	}

	if weights.Blocking != 20 {
		test.Errorf("Blocking: got %v, want 20", weights.Blocking)
	}
}

func TestBuildEffectiveWeightsAncestorOnly(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	root := newMinimalTask("root")
	mustCreateTask(test, env.taskSvc, root)
	setOverrides(test, env, root.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(10)})

	child := &domain.Task{Title: "child", ParentID: &root.ID}
	mustCreateTask(test, env.taskSvc, child)

	rootReloaded, rootReloadErr := env.bundle.Tasks.GetByID(ctx, root.ID)

	if rootReloadErr != nil {
		test.Fatalf("reload root: %v", rootReloadErr)
	}

	childReloaded, childReloadErr := env.bundle.Tasks.GetByID(ctx, child.ID)

	if childReloadErr != nil {
		test.Fatalf("reload child: %v", childReloadErr)
	}

	tasks := []*domain.Task{rootReloaded, childReloaded}
	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)

	got, buildErr := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)

	if buildErr != nil {
		test.Fatalf("buildEffectiveWeights: %v", buildErr)
	}

	childWeights, ok := got[child.ID]

	if !ok {
		test.Fatalf("expected entry for child, got %v", got)
	}

	if childWeights.Blocking != 10 {
		test.Errorf("child Blocking: got %v, want 10 (inherited from root)", childWeights.Blocking)
	}

	rootWeights, ok := got[root.ID]

	if !ok {
		test.Fatalf("expected entry for root, got %v", got)
	}

	if rootWeights.Blocking != 10 {
		test.Errorf("root Blocking: got %v, want 10", rootWeights.Blocking)
	}
}

func TestBuildEffectiveWeightsCloserAncestorWins(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	root := newMinimalTask("root")
	mustCreateTask(test, env.taskSvc, root)
	setOverrides(test, env, root.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(10)})

	mid := &domain.Task{Title: "mid", ParentID: &root.ID}
	mustCreateTask(test, env.taskSvc, mid)
	setOverrides(test, env, mid.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(20)})

	leaf := &domain.Task{Title: "leaf", ParentID: &mid.ID}
	mustCreateTask(test, env.taskSvc, leaf)

	leafReloaded, leafReloadErr := env.bundle.Tasks.GetByID(ctx, leaf.ID)

	if leafReloadErr != nil {
		test.Fatalf("reload leaf: %v", leafReloadErr)
	}

	tasks := []*domain.Task{leafReloaded}
	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)

	got, buildErr := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)

	if buildErr != nil {
		test.Fatalf("buildEffectiveWeights: %v", buildErr)
	}

	weights, ok := got[leaf.ID]

	if !ok {
		test.Fatalf("expected entry for leaf, got %v", got)
	}

	if weights.Blocking != 20 {
		test.Errorf("Blocking: got %v, want 20 (mid should win over root)", weights.Blocking)
	}
}

func TestBuildEffectiveWeightsPerKeyMerge(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	root := newMinimalTask("root")
	mustCreateTask(test, env.taskSvc, root)
	setOverrides(test, env, root.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(10)})

	mid := &domain.Task{Title: "mid", ParentID: &root.ID}
	mustCreateTask(test, env.taskSvc, mid)
	setOverrides(test, env, mid.ID, &domain.UrgencyOverrides{DueWeight: float64Ptr(5)})

	leaf := &domain.Task{Title: "leaf", ParentID: &mid.ID}
	mustCreateTask(test, env.taskSvc, leaf)

	leafReloaded, leafReloadErr := env.bundle.Tasks.GetByID(ctx, leaf.ID)

	if leafReloadErr != nil {
		test.Fatalf("reload leaf: %v", leafReloadErr)
	}

	tasks := []*domain.Task{leafReloaded}
	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)

	got, buildErr := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)

	if buildErr != nil {
		test.Fatalf("buildEffectiveWeights: %v", buildErr)
	}

	weights, ok := got[leaf.ID]

	if !ok {
		test.Fatalf("expected entry for leaf, got %v", got)
	}

	if weights.Blocking != 10 {
		test.Errorf("Blocking: got %v, want 10 (from root)", weights.Blocking)
	}
	if weights.Due != 5 {
		test.Errorf("Due: got %v, want 5 (from mid)", weights.Due)
	}
}

func TestBuildEffectiveWeightsProjectPlusSelf(test *testing.T) {
	bundle, projectRepo, _ := newSeededBundle(test)
	workflowRepo := sqlite.NewWorkflowRepo(bundle.Store.DB())
	resolver, projects := singleBundleResolver(bundle, domain.DefaultProjectUUID)
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	engine := NewUrgencyEngine(defaultWeights())
	taskSvc := NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, engine)

	ctx := context.Background()

	defaultProj, projLoadErr := projectRepo.GetByID(ctx, domain.DefaultProjectUUID)

	if projLoadErr != nil {
		test.Fatalf("loading default project: %v", projLoadErr)
	}

	defaultProj.Settings = domain.ProjectSettings{
		Urgency: &domain.UrgencyOverrides{BlockingWeight: float64Ptr(10)},
	}

	if projUpdateErr := projectRepo.Update(ctx, defaultProj); projUpdateErr != nil {
		test.Fatalf("updating project settings: %v", projUpdateErr)
	}

	env := &effectiveWeightsTestEnv{taskSvc: taskSvc, bundle: bundle}
	task := newMinimalTask("self+project")
	mustCreateTask(test, taskSvc, task)
	setOverrides(test, env, task.ID, &domain.UrgencyOverrides{DueWeight: float64Ptr(5)})

	reloaded, reloadErr := bundle.Tasks.GetByID(ctx, task.ID)

	if reloadErr != nil {
		test.Fatalf("reload task: %v", reloadErr)
	}

	tasks := []*domain.Task{reloaded}
	projectWeights := taskSvc.buildProjectWeights(ctx, tasks)

	got, buildErr := taskSvc.buildEffectiveWeights(ctx, bundle, tasks, projectWeights)

	if buildErr != nil {
		test.Fatalf("buildEffectiveWeights: %v", buildErr)
	}

	weights, ok := got[task.ID]

	if !ok {
		test.Fatalf("expected entry for task, got %v", got)
	}

	if weights.Blocking != 10 {
		test.Errorf("Blocking: got %v, want 10 (from project)", weights.Blocking)
	}
	if weights.Due != 5 {
		test.Errorf("Due: got %v, want 5 (from self)", weights.Due)
	}
}

func TestResolveEffectiveWeightsSingleTask_HasOverrides(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	root := newMinimalTask("root")
	mustCreateTask(test, env.taskSvc, root)
	setOverrides(test, env, root.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(33)})

	child := &domain.Task{Title: "child", ParentID: &root.ID}
	mustCreateTask(test, env.taskSvc, child)

	resolvedWeights, contributed, resolveErr := env.taskSvc.ResolveEffectiveWeights(ctx, child.ID)

	if resolveErr != nil {
		test.Fatalf("ResolveEffectiveWeights: %v", resolveErr)
	}

	if !contributed {
		test.Fatal("expected contributed=true when ancestor has overrides")
	}
	if resolvedWeights.Blocking != 33 {
		test.Errorf("Blocking: got %v, want 33", resolvedWeights.Blocking)
	}

	// Numeric result must match buildEffectiveWeights for the same task.
	childReloaded, childReloadErr := env.bundle.Tasks.GetByID(ctx, child.ID)

	if childReloadErr != nil {
		test.Fatalf("reload child: %v", childReloadErr)
	}

	tasks := []*domain.Task{childReloaded}
	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)

	got, buildErr := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)

	if buildErr != nil {
		test.Fatalf("buildEffectiveWeights: %v", buildErr)
	}

	builtWeights, ok := got[child.ID]

	if !ok {
		test.Fatalf("buildEffectiveWeights should have an entry for child")
	}

	if *builtWeights != resolvedWeights {
		test.Errorf("ResolveEffectiveWeights diverged from buildEffectiveWeights: got %+v vs %+v", resolvedWeights, *builtWeights)
	}
}

func TestResolveEffectiveWeightsSingleTask_NoOverrides(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("plain")
	mustCreateTask(test, env.taskSvc, task)

	weights, contributed, err := env.taskSvc.ResolveEffectiveWeights(ctx, task.ID)

	if err != nil {
		test.Fatalf("ResolveEffectiveWeights: %v", err)
	}

	if contributed {
		test.Fatal("expected contributed=false when no chain overrides")
	}
	defaults := env.taskSvc.engine.Defaults()

	if weights != defaults {
		test.Errorf("expected engine defaults, got %+v vs %+v", weights, defaults)
	}
}

// ptrToOverrides wraps a *UrgencyOverrides in another pointer so callers can
// build the **UrgencyOverrides field on TaskUpdate. Passing nil yields the
// "clear" form (outer non-nil, inner nil).
func ptrToOverrides(override *domain.UrgencyOverrides) **domain.UrgencyOverrides {
	return &override
}

// newEffectiveWeightsEnvWithProjectOverride mirrors newEffectiveWeightsEnv but
// also installs the given UrgencyOverrides on the default project so tests
// can verify inherited-baseline behavior in delta application.
func newEffectiveWeightsEnvWithProjectOverride(test *testing.T, projectOverride *domain.UrgencyOverrides) *effectiveWeightsTestEnv {
	test.Helper()
	bundle, projectRepo, _ := newSeededBundle(test)
	workflowRepo := sqlite.NewWorkflowRepo(bundle.Store.DB())

	ctx := context.Background()

	defaultProj, projLoadErr := projectRepo.GetByID(ctx, domain.DefaultProjectUUID)

	if projLoadErr != nil {
		test.Fatalf("loading default project: %v", projLoadErr)
	}

	defaultProj.Settings = domain.ProjectSettings{Urgency: projectOverride}

	if projUpdateErr := projectRepo.Update(ctx, defaultProj); projUpdateErr != nil {
		test.Fatalf("updating default project settings: %v", projUpdateErr)
	}

	resolver, projects := singleBundleResolver(bundle, domain.DefaultProjectUUID)
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	engine := NewUrgencyEngine(defaultWeights())
	taskSvc := NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, engine)
	return &effectiveWeightsTestEnv{taskSvc: taskSvc, bundle: bundle}
}

func TestUpdateUrgencyOverridesFullReplace(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("full replace")
	mustCreateTask(test, env.taskSvc, task)

	updated, updateErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:          task.ShortID,
		Version:          task.Version,
		UrgencyOverrides: ptrToOverrides(&domain.UrgencyOverrides{BlockingWeight: float64Ptr(20)}),
	})

	if updateErr != nil {
		test.Fatalf("Update: %v", updateErr)
	}

	if updated.UrgencyOverrides == nil {
		test.Fatal("expected non-nil UrgencyOverrides after full replace")
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 20 {
		test.Errorf("BlockingWeight: got %v, want 20", updated.UrgencyOverrides.BlockingWeight)
	}

	// Now clear via outer-non-nil + inner-nil.
	cleared, clearErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:          updated.ShortID,
		Version:          updated.Version,
		UrgencyOverrides: ptrToOverrides(nil),
	})

	if clearErr != nil {
		test.Fatalf("Update (clear): %v", clearErr)
	}

	if cleared.UrgencyOverrides != nil {
		test.Errorf("expected nil UrgencyOverrides after clear, got %+v", cleared.UrgencyOverrides)
	}

	reloaded, reloadErr := env.bundle.Tasks.GetByID(ctx, task.ID)

	if reloadErr != nil {
		test.Fatalf("reload: %v", reloadErr)
	}

	if reloaded.UrgencyOverrides != nil {
		test.Errorf("expected NULL column after clear, got %+v", reloaded.UrgencyOverrides)
	}
}

func TestUpdateUrgencyOverridesMergePatchSet(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("merge set")
	mustCreateTask(test, env.taskSvc, task)

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			Set: map[string]float64{"blocking_weight": 20, "due_weight": 5},
		},
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.UrgencyOverrides == nil {
		test.Fatal("expected non-nil UrgencyOverrides")
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 20 {
		test.Errorf("BlockingWeight: got %v, want 20", updated.UrgencyOverrides.BlockingWeight)
	}
	if updated.UrgencyOverrides.DueWeight == nil || *updated.UrgencyOverrides.DueWeight != 5 {
		test.Errorf("DueWeight: got %v, want 5", updated.UrgencyOverrides.DueWeight)
	}
	if updated.UrgencyOverrides.PriorityWeight != nil {
		test.Errorf("PriorityWeight: got %v, want nil (untouched)", updated.UrgencyOverrides.PriorityWeight)
	}
}

func TestUpdateUrgencyOverridesMergePatchClear(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("merge clear")
	mustCreateTask(test, env.taskSvc, task)
	setOverrides(test, env, task.ID, &domain.UrgencyOverrides{
		PriorityWeight: float64Ptr(3),
		DueWeight:      float64Ptr(5),
		BlockingWeight: float64Ptr(20),
	})

	reloaded, reloadErr := env.bundle.Tasks.GetByID(ctx, task.ID)

	if reloadErr != nil {
		test.Fatalf("reload: %v", reloadErr)
	}

	updated, updateErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: reloaded.ShortID,
		Version: reloaded.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			Clear: map[string]bool{"due_weight": true},
		},
	})

	if updateErr != nil {
		test.Fatalf("Update: %v", updateErr)
	}

	if updated.UrgencyOverrides == nil {
		test.Fatal("expected non-nil UrgencyOverrides")
	}
	if updated.UrgencyOverrides.DueWeight != nil {
		test.Errorf("DueWeight: got %v, want nil after clear", updated.UrgencyOverrides.DueWeight)
	}
	if updated.UrgencyOverrides.PriorityWeight == nil || *updated.UrgencyOverrides.PriorityWeight != 3 {
		test.Errorf("PriorityWeight: got %v, want 3 (untouched)", updated.UrgencyOverrides.PriorityWeight)
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 20 {
		test.Errorf("BlockingWeight: got %v, want 20 (untouched)", updated.UrgencyOverrides.BlockingWeight)
	}
}

func TestUpdateUrgencyOverridesMergePatchClearAll(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("clear all")
	mustCreateTask(test, env.taskSvc, task)
	setOverrides(test, env, task.ID, &domain.UrgencyOverrides{
		PriorityWeight: float64Ptr(3),
		DueWeight:      float64Ptr(5),
		BlockingWeight: float64Ptr(20),
	})

	reloaded, reloadErr := env.bundle.Tasks.GetByID(ctx, task.ID)

	if reloadErr != nil {
		test.Fatalf("reload: %v", reloadErr)
	}

	updated, updateErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: reloaded.ShortID,
		Version: reloaded.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			ClearAll: true,
		},
	})

	if updateErr != nil {
		test.Fatalf("Update: %v", updateErr)
	}

	if updated.UrgencyOverrides != nil {
		test.Errorf("expected nil UrgencyOverrides after ClearAll, got %+v", updated.UrgencyOverrides)
	}

	persisted, persistedErr := env.bundle.Tasks.GetByID(ctx, task.ID)

	if persistedErr != nil {
		test.Fatalf("reload after update: %v", persistedErr)
	}

	if persisted.UrgencyOverrides != nil {
		test.Errorf("expected persisted NULL column after ClearAll, got %+v", persisted.UrgencyOverrides)
	}
}

func TestUpdateUrgencyOverridesPatchCombined(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("combined patch")
	mustCreateTask(test, env.taskSvc, task)
	setOverrides(test, env, task.ID, &domain.UrgencyOverrides{
		PriorityWeight: float64Ptr(99),
		DueWeight:      float64Ptr(99),
		BlockingWeight: float64Ptr(99),
	})

	reloaded, reloadErr := env.bundle.Tasks.GetByID(ctx, task.ID)

	if reloadErr != nil {
		test.Fatalf("reload: %v", reloadErr)
	}

	updated, updateErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: reloaded.ShortID,
		Version: reloaded.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			ClearAll: true,
			Clear:    map[string]bool{"priority_weight": true},
			Set:      map[string]float64{"priority_weight": 5, "due_weight": 3},
		},
	})

	if updateErr != nil {
		test.Fatalf("Update: %v", updateErr)
	}

	if updated.UrgencyOverrides == nil {
		test.Fatal("expected non-nil UrgencyOverrides")
	}
	if updated.UrgencyOverrides.PriorityWeight == nil || *updated.UrgencyOverrides.PriorityWeight != 5 {
		test.Errorf("PriorityWeight: got %v, want 5 (Set runs after Clear)", updated.UrgencyOverrides.PriorityWeight)
	}
	if updated.UrgencyOverrides.DueWeight == nil || *updated.UrgencyOverrides.DueWeight != 3 {
		test.Errorf("DueWeight: got %v, want 3", updated.UrgencyOverrides.DueWeight)
	}
	if updated.UrgencyOverrides.BlockingWeight != nil {
		test.Errorf("BlockingWeight: got %v, want nil (ClearAll wiped, not re-set)", updated.UrgencyOverrides.BlockingWeight)
	}
}

func TestUpdateUrgencyDeltaInheritsResolvedValue(test *testing.T) {
	env := newEffectiveWeightsEnvWithProjectOverride(test, &domain.UrgencyOverrides{
		BlockingWeight: float64Ptr(10),
	})
	ctx := context.Background()

	task := newMinimalTask("inherit baseline")
	mustCreateTask(test, env.taskSvc, task)

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:      task.ShortID,
		Version:      task.Version,
		UrgencyDelta: map[string]float64{"blocking_weight": 5},
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.UrgencyOverrides == nil {
		test.Fatal("expected non-nil UrgencyOverrides after delta")
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 15 {
		test.Errorf("BlockingWeight: got %v, want 15 (project 10 + delta 5)", updated.UrgencyOverrides.BlockingWeight)
	}
}

func TestUpdateUrgencyDeltaAdditiveOnExistingValue(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("additive delta")
	mustCreateTask(test, env.taskSvc, task)
	setOverrides(test, env, task.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(20)})

	reloaded, reloadErr := env.bundle.Tasks.GetByID(ctx, task.ID)

	if reloadErr != nil {
		test.Fatalf("reload: %v", reloadErr)
	}

	updated, updateErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:      reloaded.ShortID,
		Version:      reloaded.Version,
		UrgencyDelta: map[string]float64{"blocking_weight": 5},
	})

	if updateErr != nil {
		test.Fatalf("Update: %v", updateErr)
	}

	if updated.UrgencyOverrides == nil {
		test.Fatal("expected non-nil UrgencyOverrides")
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 25 {
		test.Errorf("BlockingWeight: got %v, want 25 (self 20 + delta 5)", updated.UrgencyOverrides.BlockingWeight)
	}
}

func TestUpdateUrgencyDeltaAfterPatch(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("patch then delta")
	mustCreateTask(test, env.taskSvc, task)

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			Set: map[string]float64{"blocking_weight": 10},
		},
		UrgencyDelta: map[string]float64{"blocking_weight": 3},
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.UrgencyOverrides == nil {
		test.Fatal("expected non-nil UrgencyOverrides")
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 13 {
		test.Errorf("BlockingWeight: got %v, want 13 (patch 10 + delta 3)", updated.UrgencyOverrides.BlockingWeight)
	}
}

func TestUpdateUrgencyConflictingFields(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("conflict")
	mustCreateTask(test, env.taskSvc, task)
	originalVersion := task.Version

	_, conflictErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:           task.ShortID,
		Version:           task.Version,
		UrgencyOverrides:  ptrToOverrides(&domain.UrgencyOverrides{BlockingWeight: float64Ptr(20)}),
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{Set: map[string]float64{"due_weight": 5}},
	})

	if conflictErr == nil {
		test.Fatal("expected error for conflicting fields, got nil")
	}

	if !strings.Contains(conflictErr.Error(), "mutually exclusive") {
		test.Errorf("error %q should mention 'mutually exclusive'", conflictErr)
	}

	persisted, persistedErr := env.bundle.Tasks.GetByID(ctx, task.ID)

	if persistedErr != nil {
		test.Fatalf("reload: %v", persistedErr)
	}

	if persisted.Version != originalVersion {
		test.Errorf("Version: got %d, want %d (no bump on rejected update)", persisted.Version, originalVersion)
	}
	if persisted.UrgencyOverrides != nil {
		test.Errorf("UrgencyOverrides: got %+v, want nil (no mutation on rejected update)", persisted.UrgencyOverrides)
	}
}

func TestUpdateUrgencyUnknownKey(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("unknown key")
	mustCreateTask(test, env.taskSvc, task)
	originalVersion := task.Version

	_, unknownErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			Set: map[string]float64{"bogus": 1},
		},
	})

	if unknownErr == nil {
		test.Fatal("expected error for unknown key, got nil")
	}

	if !strings.Contains(unknownErr.Error(), "bogus") {
		test.Errorf("error %q should mention key 'bogus'", unknownErr)
	}

	persisted, persistedErr := env.bundle.Tasks.GetByID(ctx, task.ID)

	if persistedErr != nil {
		test.Fatalf("reload: %v", persistedErr)
	}

	if persisted.Version != originalVersion {
		test.Errorf("Version: got %d, want %d (no bump on rejected update)", persisted.Version, originalVersion)
	}
	if persisted.UrgencyOverrides != nil {
		test.Errorf("UrgencyOverrides: got %+v, want nil (no mutation on rejected update)", persisted.UrgencyOverrides)
	}
}

func TestUpdateUrgencyNormalizesEmpty(test *testing.T) {
	env := newEffectiveWeightsEnv(test)
	ctx := context.Background()

	task := newMinimalTask("normalize")
	mustCreateTask(test, env.taskSvc, task)
	setOverrides(test, env, task.ID, &domain.UrgencyOverrides{PriorityWeight: float64Ptr(5)})

	reloaded, reloadErr := env.bundle.Tasks.GetByID(ctx, task.ID)

	if reloadErr != nil {
		test.Fatalf("reload: %v", reloadErr)
	}

	updated, updateErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: reloaded.ShortID,
		Version: reloaded.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			Clear: map[string]bool{"priority_weight": true},
		},
	})

	if updateErr != nil {
		test.Fatalf("Update: %v", updateErr)
	}

	if updated.UrgencyOverrides != nil {
		test.Errorf("expected nil UrgencyOverrides after clearing only key, got %+v", updated.UrgencyOverrides)
	}

	persisted, persistedErr := env.bundle.Tasks.GetByID(ctx, task.ID)

	if persistedErr != nil {
		test.Fatalf("reload after update: %v", persistedErr)
	}

	if persisted.UrgencyOverrides != nil {
		test.Errorf("expected NULL column (not empty {}), got %+v", persisted.UrgencyOverrides)
	}
}
