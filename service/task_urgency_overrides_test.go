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

func newEffectiveWeightsEnv(t *testing.T) *effectiveWeightsTestEnv {
	t.Helper()
	bundle, projectRepo, _ := newSeededBundle(t)
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
func setOverrides(t *testing.T, env *effectiveWeightsTestEnv, taskID uuid.UUID, ov *domain.UrgencyOverrides) {
	t.Helper()
	ctx := context.Background()
	task, err := env.bundle.Tasks.GetByID(ctx, taskID)
	if err != nil {
		t.Fatalf("loading task to set overrides: %v", err)
	}
	task.UrgencyOverrides = ov
	if err := env.bundle.Tasks.Update(ctx, task); err != nil {
		t.Fatalf("updating task overrides: %v", err)
	}
}

func float64Ptr(v float64) *float64 { return &v }

func TestBuildEffectiveWeightsNoOverrides(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	tasks := make([]*domain.Task, 0, 3)
	for i := 0; i < 3; i++ {
		task := newMinimalTask("root task")
		mustCreateTask(t, env.taskSvc, task)
		tasks = append(tasks, task)
	}

	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)
	got, err := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)
	if err != nil {
		t.Fatalf("buildEffectiveWeights: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil (fast path) when no overrides + no parents, got %v", got)
	}
}

func TestBuildEffectiveWeightsSelfOnly(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("self override")
	mustCreateTask(t, env.taskSvc, task)
	setOverrides(t, env, task.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(20)})

	reloaded, err := env.bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reloading task: %v", err)
	}
	tasks := []*domain.Task{reloaded}
	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)
	got, err := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)
	if err != nil {
		t.Fatalf("buildEffectiveWeights: %v", err)
	}
	w, ok := got[task.ID]
	if !ok {
		t.Fatalf("expected entry for task %v, got %v", task.ID, got)
	}
	if w.Blocking != 20 {
		t.Errorf("Blocking: got %v, want 20", w.Blocking)
	}
}

func TestBuildEffectiveWeightsAncestorOnly(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	root := newMinimalTask("root")
	mustCreateTask(t, env.taskSvc, root)
	setOverrides(t, env, root.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(10)})

	child := &domain.Task{Title: "child", ParentID: &root.ID}
	mustCreateTask(t, env.taskSvc, child)

	rootReloaded, err := env.bundle.Tasks.GetByID(ctx, root.ID)
	if err != nil {
		t.Fatalf("reload root: %v", err)
	}
	childReloaded, err := env.bundle.Tasks.GetByID(ctx, child.ID)
	if err != nil {
		t.Fatalf("reload child: %v", err)
	}
	tasks := []*domain.Task{rootReloaded, childReloaded}
	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)
	got, err := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)
	if err != nil {
		t.Fatalf("buildEffectiveWeights: %v", err)
	}

	cw, ok := got[child.ID]
	if !ok {
		t.Fatalf("expected entry for child, got %v", got)
	}
	if cw.Blocking != 10 {
		t.Errorf("child Blocking: got %v, want 10 (inherited from root)", cw.Blocking)
	}

	rw, ok := got[root.ID]
	if !ok {
		t.Fatalf("expected entry for root, got %v", got)
	}
	if rw.Blocking != 10 {
		t.Errorf("root Blocking: got %v, want 10", rw.Blocking)
	}
}

func TestBuildEffectiveWeightsCloserAncestorWins(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	root := newMinimalTask("root")
	mustCreateTask(t, env.taskSvc, root)
	setOverrides(t, env, root.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(10)})

	mid := &domain.Task{Title: "mid", ParentID: &root.ID}
	mustCreateTask(t, env.taskSvc, mid)
	setOverrides(t, env, mid.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(20)})

	leaf := &domain.Task{Title: "leaf", ParentID: &mid.ID}
	mustCreateTask(t, env.taskSvc, leaf)

	leafReloaded, err := env.bundle.Tasks.GetByID(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("reload leaf: %v", err)
	}
	tasks := []*domain.Task{leafReloaded}
	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)
	got, err := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)
	if err != nil {
		t.Fatalf("buildEffectiveWeights: %v", err)
	}

	w, ok := got[leaf.ID]
	if !ok {
		t.Fatalf("expected entry for leaf, got %v", got)
	}
	if w.Blocking != 20 {
		t.Errorf("Blocking: got %v, want 20 (mid should win over root)", w.Blocking)
	}
}

func TestBuildEffectiveWeightsPerKeyMerge(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	root := newMinimalTask("root")
	mustCreateTask(t, env.taskSvc, root)
	setOverrides(t, env, root.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(10)})

	mid := &domain.Task{Title: "mid", ParentID: &root.ID}
	mustCreateTask(t, env.taskSvc, mid)
	setOverrides(t, env, mid.ID, &domain.UrgencyOverrides{DueWeight: float64Ptr(5)})

	leaf := &domain.Task{Title: "leaf", ParentID: &mid.ID}
	mustCreateTask(t, env.taskSvc, leaf)

	leafReloaded, err := env.bundle.Tasks.GetByID(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("reload leaf: %v", err)
	}
	tasks := []*domain.Task{leafReloaded}
	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)
	got, err := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)
	if err != nil {
		t.Fatalf("buildEffectiveWeights: %v", err)
	}

	w, ok := got[leaf.ID]
	if !ok {
		t.Fatalf("expected entry for leaf, got %v", got)
	}
	if w.Blocking != 10 {
		t.Errorf("Blocking: got %v, want 10 (from root)", w.Blocking)
	}
	if w.Due != 5 {
		t.Errorf("Due: got %v, want 5 (from mid)", w.Due)
	}
}

func TestBuildEffectiveWeightsProjectPlusSelf(t *testing.T) {
	bundle, projectRepo, _ := newSeededBundle(t)
	workflowRepo := sqlite.NewWorkflowRepo(bundle.Store.DB())
	resolver, projects := singleBundleResolver(bundle, domain.DefaultProjectUUID)
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	engine := NewUrgencyEngine(defaultWeights())
	taskSvc := NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, engine)

	ctx := context.Background()
	defaultProj, err := projectRepo.GetByID(ctx, domain.DefaultProjectUUID)
	if err != nil {
		t.Fatalf("loading default project: %v", err)
	}
	defaultProj.Settings = domain.ProjectSettings{
		Urgency: &domain.UrgencyOverrides{BlockingWeight: float64Ptr(10)},
	}
	if err := projectRepo.Update(ctx, defaultProj); err != nil {
		t.Fatalf("updating project settings: %v", err)
	}

	env := &effectiveWeightsTestEnv{taskSvc: taskSvc, bundle: bundle}
	task := newMinimalTask("self+project")
	mustCreateTask(t, taskSvc, task)
	setOverrides(t, env, task.ID, &domain.UrgencyOverrides{DueWeight: float64Ptr(5)})

	reloaded, err := bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	tasks := []*domain.Task{reloaded}
	projectWeights := taskSvc.buildProjectWeights(ctx, tasks)
	got, err := taskSvc.buildEffectiveWeights(ctx, bundle, tasks, projectWeights)
	if err != nil {
		t.Fatalf("buildEffectiveWeights: %v", err)
	}

	w, ok := got[task.ID]
	if !ok {
		t.Fatalf("expected entry for task, got %v", got)
	}
	if w.Blocking != 10 {
		t.Errorf("Blocking: got %v, want 10 (from project)", w.Blocking)
	}
	if w.Due != 5 {
		t.Errorf("Due: got %v, want 5 (from self)", w.Due)
	}
}

func TestResolveEffectiveWeightsSingleTask_HasOverrides(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	root := newMinimalTask("root")
	mustCreateTask(t, env.taskSvc, root)
	setOverrides(t, env, root.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(33)})

	child := &domain.Task{Title: "child", ParentID: &root.ID}
	mustCreateTask(t, env.taskSvc, child)

	w, contributed, err := env.taskSvc.ResolveEffectiveWeights(ctx, child.ID)
	if err != nil {
		t.Fatalf("ResolveEffectiveWeights: %v", err)
	}
	if !contributed {
		t.Fatal("expected contributed=true when ancestor has overrides")
	}
	if w.Blocking != 33 {
		t.Errorf("Blocking: got %v, want 33", w.Blocking)
	}

	// Numeric result must match buildEffectiveWeights for the same task.
	childReloaded, err := env.bundle.Tasks.GetByID(ctx, child.ID)
	if err != nil {
		t.Fatalf("reload child: %v", err)
	}
	tasks := []*domain.Task{childReloaded}
	projectWeights := env.taskSvc.buildProjectWeights(ctx, tasks)
	got, err := env.taskSvc.buildEffectiveWeights(ctx, env.bundle, tasks, projectWeights)
	if err != nil {
		t.Fatalf("buildEffectiveWeights: %v", err)
	}
	bw, ok := got[child.ID]
	if !ok {
		t.Fatalf("buildEffectiveWeights should have an entry for child")
	}
	if *bw != w {
		t.Errorf("ResolveEffectiveWeights diverged from buildEffectiveWeights: got %+v vs %+v", w, *bw)
	}
}

func TestResolveEffectiveWeightsSingleTask_NoOverrides(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("plain")
	mustCreateTask(t, env.taskSvc, task)

	w, contributed, err := env.taskSvc.ResolveEffectiveWeights(ctx, task.ID)
	if err != nil {
		t.Fatalf("ResolveEffectiveWeights: %v", err)
	}
	if contributed {
		t.Fatal("expected contributed=false when no chain overrides")
	}
	defaults := env.taskSvc.engine.Defaults()
	if w != defaults {
		t.Errorf("expected engine defaults, got %+v vs %+v", w, defaults)
	}
}

// ptrToOverrides wraps a *UrgencyOverrides in another pointer so callers can
// build the **UrgencyOverrides field on TaskUpdate. Passing nil yields the
// "clear" form (outer non-nil, inner nil).
func ptrToOverrides(o *domain.UrgencyOverrides) **domain.UrgencyOverrides {
	return &o
}

// newEffectiveWeightsEnvWithProjectOverride mirrors newEffectiveWeightsEnv but
// also installs the given UrgencyOverrides on the default project so tests
// can verify inherited-baseline behavior in delta application.
func newEffectiveWeightsEnvWithProjectOverride(t *testing.T, ov *domain.UrgencyOverrides) *effectiveWeightsTestEnv {
	t.Helper()
	bundle, projectRepo, _ := newSeededBundle(t)
	workflowRepo := sqlite.NewWorkflowRepo(bundle.Store.DB())

	ctx := context.Background()
	defaultProj, err := projectRepo.GetByID(ctx, domain.DefaultProjectUUID)
	if err != nil {
		t.Fatalf("loading default project: %v", err)
	}
	defaultProj.Settings = domain.ProjectSettings{Urgency: ov}
	if err := projectRepo.Update(ctx, defaultProj); err != nil {
		t.Fatalf("updating default project settings: %v", err)
	}

	resolver, projects := singleBundleResolver(bundle, domain.DefaultProjectUUID)
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	engine := NewUrgencyEngine(defaultWeights())
	taskSvc := NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, engine)
	return &effectiveWeightsTestEnv{taskSvc: taskSvc, bundle: bundle}
}

func TestUpdateUrgencyOverridesFullReplace(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("full replace")
	mustCreateTask(t, env.taskSvc, task)

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:          task.ShortID,
		Version:          task.Version,
		UrgencyOverrides: ptrToOverrides(&domain.UrgencyOverrides{BlockingWeight: float64Ptr(20)}),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UrgencyOverrides == nil {
		t.Fatal("expected non-nil UrgencyOverrides after full replace")
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 20 {
		t.Errorf("BlockingWeight: got %v, want 20", updated.UrgencyOverrides.BlockingWeight)
	}

	// Now clear via outer-non-nil + inner-nil.
	cleared, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:          updated.ShortID,
		Version:          updated.Version,
		UrgencyOverrides: ptrToOverrides(nil),
	})
	if err != nil {
		t.Fatalf("Update (clear): %v", err)
	}
	if cleared.UrgencyOverrides != nil {
		t.Errorf("expected nil UrgencyOverrides after clear, got %+v", cleared.UrgencyOverrides)
	}

	reloaded, err := env.bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.UrgencyOverrides != nil {
		t.Errorf("expected NULL column after clear, got %+v", reloaded.UrgencyOverrides)
	}
}

func TestUpdateUrgencyOverridesMergePatchSet(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("merge set")
	mustCreateTask(t, env.taskSvc, task)

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			Set: map[string]float64{"blocking_weight": 20, "due_weight": 5},
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UrgencyOverrides == nil {
		t.Fatal("expected non-nil UrgencyOverrides")
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 20 {
		t.Errorf("BlockingWeight: got %v, want 20", updated.UrgencyOverrides.BlockingWeight)
	}
	if updated.UrgencyOverrides.DueWeight == nil || *updated.UrgencyOverrides.DueWeight != 5 {
		t.Errorf("DueWeight: got %v, want 5", updated.UrgencyOverrides.DueWeight)
	}
	if updated.UrgencyOverrides.PriorityWeight != nil {
		t.Errorf("PriorityWeight: got %v, want nil (untouched)", updated.UrgencyOverrides.PriorityWeight)
	}
}

func TestUpdateUrgencyOverridesMergePatchClear(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("merge clear")
	mustCreateTask(t, env.taskSvc, task)
	setOverrides(t, env, task.ID, &domain.UrgencyOverrides{
		PriorityWeight: float64Ptr(3),
		DueWeight:      float64Ptr(5),
		BlockingWeight: float64Ptr(20),
	})
	reloaded, err := env.bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: reloaded.ShortID,
		Version: reloaded.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			Clear: map[string]bool{"due_weight": true},
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UrgencyOverrides == nil {
		t.Fatal("expected non-nil UrgencyOverrides")
	}
	if updated.UrgencyOverrides.DueWeight != nil {
		t.Errorf("DueWeight: got %v, want nil after clear", updated.UrgencyOverrides.DueWeight)
	}
	if updated.UrgencyOverrides.PriorityWeight == nil || *updated.UrgencyOverrides.PriorityWeight != 3 {
		t.Errorf("PriorityWeight: got %v, want 3 (untouched)", updated.UrgencyOverrides.PriorityWeight)
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 20 {
		t.Errorf("BlockingWeight: got %v, want 20 (untouched)", updated.UrgencyOverrides.BlockingWeight)
	}
}

func TestUpdateUrgencyOverridesMergePatchClearAll(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("clear all")
	mustCreateTask(t, env.taskSvc, task)
	setOverrides(t, env, task.ID, &domain.UrgencyOverrides{
		PriorityWeight: float64Ptr(3),
		DueWeight:      float64Ptr(5),
		BlockingWeight: float64Ptr(20),
	})
	reloaded, err := env.bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: reloaded.ShortID,
		Version: reloaded.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			ClearAll: true,
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UrgencyOverrides != nil {
		t.Errorf("expected nil UrgencyOverrides after ClearAll, got %+v", updated.UrgencyOverrides)
	}

	persisted, err := env.bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload after update: %v", err)
	}
	if persisted.UrgencyOverrides != nil {
		t.Errorf("expected persisted NULL column after ClearAll, got %+v", persisted.UrgencyOverrides)
	}
}

func TestUpdateUrgencyOverridesPatchCombined(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("combined patch")
	mustCreateTask(t, env.taskSvc, task)
	setOverrides(t, env, task.ID, &domain.UrgencyOverrides{
		PriorityWeight: float64Ptr(99),
		DueWeight:      float64Ptr(99),
		BlockingWeight: float64Ptr(99),
	})
	reloaded, err := env.bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: reloaded.ShortID,
		Version: reloaded.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			ClearAll: true,
			Clear:    map[string]bool{"priority_weight": true},
			Set:      map[string]float64{"priority_weight": 5, "due_weight": 3},
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UrgencyOverrides == nil {
		t.Fatal("expected non-nil UrgencyOverrides")
	}
	if updated.UrgencyOverrides.PriorityWeight == nil || *updated.UrgencyOverrides.PriorityWeight != 5 {
		t.Errorf("PriorityWeight: got %v, want 5 (Set runs after Clear)", updated.UrgencyOverrides.PriorityWeight)
	}
	if updated.UrgencyOverrides.DueWeight == nil || *updated.UrgencyOverrides.DueWeight != 3 {
		t.Errorf("DueWeight: got %v, want 3", updated.UrgencyOverrides.DueWeight)
	}
	if updated.UrgencyOverrides.BlockingWeight != nil {
		t.Errorf("BlockingWeight: got %v, want nil (ClearAll wiped, not re-set)", updated.UrgencyOverrides.BlockingWeight)
	}
}

func TestUpdateUrgencyDeltaInheritsResolvedValue(t *testing.T) {
	env := newEffectiveWeightsEnvWithProjectOverride(t, &domain.UrgencyOverrides{
		BlockingWeight: float64Ptr(10),
	})
	ctx := context.Background()

	task := newMinimalTask("inherit baseline")
	mustCreateTask(t, env.taskSvc, task)

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:      task.ShortID,
		Version:      task.Version,
		UrgencyDelta: map[string]float64{"blocking_weight": 5},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UrgencyOverrides == nil {
		t.Fatal("expected non-nil UrgencyOverrides after delta")
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 15 {
		t.Errorf("BlockingWeight: got %v, want 15 (project 10 + delta 5)", updated.UrgencyOverrides.BlockingWeight)
	}
}

func TestUpdateUrgencyDeltaAdditiveOnExistingValue(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("additive delta")
	mustCreateTask(t, env.taskSvc, task)
	setOverrides(t, env, task.ID, &domain.UrgencyOverrides{BlockingWeight: float64Ptr(20)})
	reloaded, err := env.bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:      reloaded.ShortID,
		Version:      reloaded.Version,
		UrgencyDelta: map[string]float64{"blocking_weight": 5},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UrgencyOverrides == nil {
		t.Fatal("expected non-nil UrgencyOverrides")
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 25 {
		t.Errorf("BlockingWeight: got %v, want 25 (self 20 + delta 5)", updated.UrgencyOverrides.BlockingWeight)
	}
}

func TestUpdateUrgencyDeltaAfterPatch(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("patch then delta")
	mustCreateTask(t, env.taskSvc, task)

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			Set: map[string]float64{"blocking_weight": 10},
		},
		UrgencyDelta: map[string]float64{"blocking_weight": 3},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UrgencyOverrides == nil {
		t.Fatal("expected non-nil UrgencyOverrides")
	}
	if updated.UrgencyOverrides.BlockingWeight == nil || *updated.UrgencyOverrides.BlockingWeight != 13 {
		t.Errorf("BlockingWeight: got %v, want 13 (patch 10 + delta 3)", updated.UrgencyOverrides.BlockingWeight)
	}
}

func TestUpdateUrgencyConflictingFields(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("conflict")
	mustCreateTask(t, env.taskSvc, task)
	originalVersion := task.Version

	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:           task.ShortID,
		Version:           task.Version,
		UrgencyOverrides:  ptrToOverrides(&domain.UrgencyOverrides{BlockingWeight: float64Ptr(20)}),
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{Set: map[string]float64{"due_weight": 5}},
	})
	if err == nil {
		t.Fatal("expected error for conflicting fields, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q should mention 'mutually exclusive'", err)
	}

	persisted, err := env.bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if persisted.Version != originalVersion {
		t.Errorf("Version: got %d, want %d (no bump on rejected update)", persisted.Version, originalVersion)
	}
	if persisted.UrgencyOverrides != nil {
		t.Errorf("UrgencyOverrides: got %+v, want nil (no mutation on rejected update)", persisted.UrgencyOverrides)
	}
}

func TestUpdateUrgencyUnknownKey(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("unknown key")
	mustCreateTask(t, env.taskSvc, task)
	originalVersion := task.Version

	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			Set: map[string]float64{"bogus": 1},
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error %q should mention key 'bogus'", err)
	}

	persisted, err := env.bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if persisted.Version != originalVersion {
		t.Errorf("Version: got %d, want %d (no bump on rejected update)", persisted.Version, originalVersion)
	}
	if persisted.UrgencyOverrides != nil {
		t.Errorf("UrgencyOverrides: got %+v, want nil (no mutation on rejected update)", persisted.UrgencyOverrides)
	}
}

func TestUpdateUrgencyNormalizesEmpty(t *testing.T) {
	env := newEffectiveWeightsEnv(t)
	ctx := context.Background()

	task := newMinimalTask("normalize")
	mustCreateTask(t, env.taskSvc, task)
	setOverrides(t, env, task.ID, &domain.UrgencyOverrides{PriorityWeight: float64Ptr(5)})
	reloaded, err := env.bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: reloaded.ShortID,
		Version: reloaded.Version,
		UrgencyMergePatch: &domain.UrgencyOverridesPatch{
			Clear: map[string]bool{"priority_weight": true},
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UrgencyOverrides != nil {
		t.Errorf("expected nil UrgencyOverrides after clearing only key, got %+v", updated.UrgencyOverrides)
	}

	persisted, err := env.bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload after update: %v", err)
	}
	if persisted.UrgencyOverrides != nil {
		t.Errorf("expected NULL column (not empty {}), got %+v", persisted.UrgencyOverrides)
	}
}
