package service

import (
	"context"
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
