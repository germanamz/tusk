# Phase 4 — Service Routing & Fan-Out

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Initiative:** Project Management CLI (ROADMAP.md:556-564)
**Phase:** 4 of 4
**Prerequisites:** Phases 1, 2, and 3 complete.

---

## Inherits From

Phase 3 left the repository with:

- `sqlite.StoreRegistry` with `Get`, `Default`, `ProjectIDs`, `Close` — fully tested, used by `cmd/tusk/main.go` for the default store only
- `service.RepoBundle` struct (fields: `Store *sqlite.Store`, `Tasks`, `Annotations`, `Relations`, `Tags`, `Players`) — declared but **not yet consumed** by any service
- `service.BundleResolver func(ctx, projectID) (*RepoBundle, error)` — type declared, no production closure yet
- `service.ProjectLister func(ctx) ([]string, error)` — type declared, no production closure yet
- `domain.ErrCrossStoreRelation` — sentinel ready but never returned
- **Bridge in `cmd/tusk/main.go`** — `registry.Default()` is assigned to a local `store` variable and the legacy service wiring (direct `sqlite.NewTaskRepo(store.DB())`, `service.NewTaskService(taskRepo, ...)`, etc.) is unchanged. Phase 4 removes this bridge.
- Every service constructor unchanged from the pre-initiative baseline. `TaskService` holds individual repo fields. `RelationService` holds `relationRepo`, `taskRepo`, and `store` (tx provider). `TagService` holds only `tagRepo`.
- All service tests use real in-memory SQLite (`sqlite.NewTaskRepo(db)` where `db` points at `:memory:`) — there is no ad-hoc fake repo builder. Phase 4 tests follow the same approach.
- `syntax.FilterSet.GetField(key string) *FieldFilter` already exists and can extract a `project=<names>` clause from a parsed filter without any new walker.

---

## Goal

Flip the service layer from direct repo access to the `BundleResolver` pattern, wire the production resolver in `cmd/tusk/main.go`, make cross-project reads (`List`, `Available`, `Next`, `Pop`) fan out across stores, enforce the cross-store relation guard, and prove the whole thing end-to-end with a two-project e2e test. This is the phase that delivers the user-visible feature: `tusk project create backend db-path=/data/backend.db` actually puts backend tasks in `/data/backend.db`.

---

## User-Visible Behaviors Preserved

Every command that worked after Phase 3 must still work after Phase 4 — the refactor is behavior-preserving for the single-store case.

Newly enabled behavior:

- A project with `db_path=/some/file.db` set in config writes and reads all task, annotation, relation, and tag-junction data from `/some/file.db` instead of the default store
- `tusk list`, `tusk available`, `tusk next`, `tusk pop` with no project filter merge results from every known project's store and sort by urgency
- `tusk list project=<name>` reads from that project's store only
- `tusk link <src> blocks <dst>` where src and dst belong to projects in different SQLite files is rejected with `domain.ErrCrossStoreRelation`
- `tusk modify <task> project=<other>` where the new project lives in a different store is rejected (cross-store task migration is not supported in this initiative)

Acceptance: `make test test-race test-e2e vet lint` all green, plus the new `TestPerProjectDatabase` e2e scenarios.

---

## File Structure

**Create:**
- `tests/e2e/project_db_path_test.go` — two-project end-to-end scenarios

**Modify:**
- `service/task.go` — `TaskService` struct, `NewTaskService` signature, every method that reads from a repo
- `service/task_test.go` — update existing tests to match the new constructor and add new fan-out coverage
- `service/relation.go` — `RelationService` struct, `NewRelationService` signature, cross-store guard in `Add`
- `service/relation_test.go` — add cross-store rejection test
- `service/tag.go` — `TagService` struct, `NewTagService` signature, route tag-junction writes per task's bundle
- `service/tag_test.go` — update tests to match new constructor
- `cmd/tusk/main.go` — remove the Phase 3 bridge and replace with a production resolver + fan-out wiring

---

## Tasks

### Task 1: Refactor `TaskService` to use `BundleResolver`

**Files:**
- Modify: `service/task.go`
- Modify: `service/task_test.go`

Replace the five per-store fields (`taskRepo`, `annotationRepo`, `relationRepo`, `tagRepo`, `playerRepo`) and the `store` tx provider with:

- `resolve service.BundleResolver`
- `projects service.ProjectLister`

Every method that currently calls `s.taskRepo.X` becomes:

```go
bundle, err := s.resolve(ctx, projectID)
if err != nil { return nil, err }
return bundle.Tasks.X(...)
```

The tricky method is `GetByShortID` (and any other method that receives only a task ID or short ID without a project). These must iterate `s.projects(ctx)`, call `s.resolve` for each, and search each bundle's `Tasks.GetByShortID` until one returns `nil` (found) or all return `ErrNotFound`. Extract this into a helper:

```go
// bundleForShortID finds the bundle containing a task by short ID.
// Returns (bundle, task, nil) on success, (nil, nil, ErrNotFound) if no
// store has the task.
func (s *TaskService) bundleForShortID(ctx context.Context, shortID string) (*RepoBundle, *domain.Task, error) {
	ids, err := s.projects(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, pid := range ids {
		bundle, err := s.resolve(ctx, pid)
		if err != nil {
			return nil, nil, err
		}
		task, err := bundle.Tasks.GetByShortID(ctx, shortID)
		if err == nil {
			return bundle, task, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, nil, err
		}
	}
	return nil, nil, domain.ErrNotFound
}
```

Do the analogous thing for any method that takes a full UUID and needs to route without knowing the project ID.

Any method that receives a task struct already has `task.ProjectID` and can resolve directly.

- [ ] **Step 1: Write the failing test for resolver routing**

Append to `service/task_test.go`:

```go
func TestTaskService_CreateRoutesToProjectBundle(t *testing.T) {
	ctx := context.Background()
	defaultBundle := newTestBundle(t) // helper defined below
	backendBundle := newTestBundle(t)

	resolver := func(_ context.Context, projectID string) (*RepoBundle, error) {
		switch projectID {
		case "default":
			return defaultBundle, nil
		case "backend":
			return backendBundle, nil
		}
		return nil, fmt.Errorf("unknown project %q", projectID)
	}
	projects := func(context.Context) ([]string, error) {
		return []string{"default", "backend"}, nil
	}

	svc := newTaskServiceForTest(t, resolver, projects, []string{"default", "backend"})

	task, err := svc.Create(ctx, CreateTaskInput{Title: "t", Project: "backend"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got, _ := backendBundle.Tasks.GetByID(ctx, task.ID); got == nil {
		t.Fatal("expected task in backend bundle")
	}
	if got, _ := defaultBundle.Tasks.GetByID(ctx, task.ID); got != nil {
		t.Fatal("task should NOT be in default bundle")
	}
}

// newTestBundle creates an in-memory SQLite store and returns a RepoBundle
// wrapping real repositories against it. Callers should defer t.Cleanup
// (handled implicitly via t.TempDir).
func newTestBundle(t *testing.T) *RepoBundle {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.New(filepath.Join(dir, "test.db"), migrations.FS)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	db := store.DB()
	return &RepoBundle{
		Store:       store,
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
		Relations:   sqlite.NewRelationRepo(db),
		Tags:        sqlite.NewTagRepo(db),
		Players:     sqlite.NewPlayerRepo(db),
	}
}

// newTaskServiceForTest builds a TaskService with the given resolver,
// a config-seeded projectRepo containing the listed project IDs, and the
// default workflow service wired from an in-memory kanban workflow.
func newTaskServiceForTest(t *testing.T, resolver BundleResolver, projects ProjectLister, projectIDs []string) *TaskService {
	t.Helper()
	projConfig := map[string]config.ProjectConfig{}
	for _, id := range projectIDs {
		projConfig[id] = config.ProjectConfig{Workflow: "kanban"}
	}
	projectRepo := inmem.NewProjectRepository(projConfig)
	workflowRepo := inmem.NewWorkflowRepository(defaultKanbanWorkflows())
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)

	engine := NewUrgencyEngine(UrgencyWeights{ /* use defaults from existing tests */ })
	return NewTaskService(resolver, projects, projectRepo, workflowSvc, engine)
}
```

(`defaultKanbanWorkflows()` is a helper that likely already exists in `service/task_test.go` — if not, reuse whatever fixture the other existing tests use. Grep first: `grep -n 'NewWorkflowRepository' service/task_test.go`.)

- [ ] **Step 2: Run and confirm failure**

```bash
go test ./service -run TestTaskService_CreateRoutesToProjectBundle -v
```

Expected: FAIL — `NewTaskService` signature does not match (currently takes 9 args).

- [ ] **Step 3: Refactor the struct and constructor**

In `service/task.go`, replace the `TaskService` struct definition. Old shape (around line 29):

```go
type TaskService struct {
	taskRepo       repository.TaskRepository
	annotationRepo repository.AnnotationRepository
	relationRepo   repository.RelationRepository
	tagRepo        repository.TagRepository
	projectRepo    repository.ProjectRepository
	workflowSvc    *WorkflowService
	tx             TaskTxProvider
	engine         *UrgencyEngine
	playerRepo     repository.PlayerRepository
}
```

New shape:

```go
type TaskService struct {
	resolve     BundleResolver
	projects    ProjectLister
	projectRepo repository.ProjectRepository
	workflowSvc *WorkflowService
	engine      *UrgencyEngine
}
```

New constructor:

```go
func NewTaskService(
	resolve BundleResolver,
	projects ProjectLister,
	projectRepo repository.ProjectRepository,
	workflowSvc *WorkflowService,
	engine *UrgencyEngine,
) *TaskService {
	return &TaskService{
		resolve:     resolve,
		projects:    projects,
		projectRepo: projectRepo,
		workflowSvc: workflowSvc,
		engine:      engine,
	}
}
```

Then refactor every method in `service/task.go` to use `s.resolve(ctx, projectID)` and the resulting bundle's repo. For tx-scoped methods, use `bundle.Store` instead of the removed `s.tx` field. Add the `bundleForShortID` helper described above for methods that only receive a short ID.

- [ ] **Step 4: Re-run all service/task tests and fix compilation fallout**

```bash
go test ./service/...
```

Every existing `service/task_test.go` test that calls `NewTaskService(...)` with 9 args will fail to compile. Update each to use `newTaskServiceForTest` (the helper introduced in Step 1). The invariant is: every test now needs an in-memory `RepoBundle` instead of direct `sqlite.NewTaskRepo(db)` wiring. The `newTestBundle` helper handles this.

Expected final state: all previously-passing `service/task` tests pass against the new constructor, plus the new `TestTaskService_CreateRoutesToProjectBundle` passes.

- [ ] **Step 5: Commit**

```bash
git add service/task.go service/task_test.go
git commit -m "refactor(service): route TaskService through BundleResolver"
```

---

### Task 2: Refactor `RelationService` + cross-store guard

**Files:**
- Modify: `service/relation.go`
- Modify: `service/relation_test.go`

`NewRelationService(relationRepo, taskRepo, store)` becomes `NewRelationService(resolve, projects)`. The `Add` method currently calls `s.taskRepo.GetByShortID` to resolve source and target. After refactor it uses the same `bundleForShortID`-style lookup to find the source and target bundles. If the two bundles are not pointer-equal, return `domain.ErrCrossStoreRelation`.

- [ ] **Step 1: Write the failing tests**

Append to `service/relation_test.go`:

```go
func TestRelationService_RejectsCrossStore(t *testing.T) {
	ctx := context.Background()
	defaultBundle := newTestBundle(t)
	backendBundle := newTestBundle(t)

	resolver := func(_ context.Context, projectID string) (*RepoBundle, error) {
		switch projectID {
		case "default":
			return defaultBundle, nil
		case "backend":
			return backendBundle, nil
		}
		return nil, fmt.Errorf("unknown project %q", projectID)
	}
	projects := func(context.Context) ([]string, error) {
		return []string{"default", "backend"}, nil
	}

	// Seed a task into each bundle.
	defaultTask := &domain.Task{ID: uuid.New(), ShortID: "aaaa1111", Title: "d", ProjectID: "default", Status: "pending"}
	if err := defaultBundle.Tasks.Create(ctx, defaultTask); err != nil {
		t.Fatal(err)
	}
	backendTask := &domain.Task{ID: uuid.New(), ShortID: "bbbb2222", Title: "b", ProjectID: "backend", Status: "pending"}
	if err := backendBundle.Tasks.Create(ctx, backendTask); err != nil {
		t.Fatal(err)
	}

	svc := NewRelationService(resolver, projects)
	_, err := svc.Add(ctx, "aaaa1111", "blocks", "bbbb2222")
	if !errors.Is(err, domain.ErrCrossStoreRelation) {
		t.Fatalf("expected ErrCrossStoreRelation, got %v", err)
	}
}

func TestRelationService_SameStoreAllowed(t *testing.T) {
	ctx := context.Background()
	bundle := newTestBundle(t)
	resolver := func(context.Context, string) (*RepoBundle, error) { return bundle, nil }
	projects := func(context.Context) ([]string, error) { return []string{"default"}, nil }

	src := &domain.Task{ID: uuid.New(), ShortID: "aaaa1111", Title: "s", ProjectID: "default", Status: "pending"}
	dst := &domain.Task{ID: uuid.New(), ShortID: "bbbb2222", Title: "d", ProjectID: "default", Status: "pending"}
	if err := bundle.Tasks.Create(ctx, src); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Tasks.Create(ctx, dst); err != nil {
		t.Fatal(err)
	}

	svc := NewRelationService(resolver, projects)
	if _, err := svc.Add(ctx, "aaaa1111", "blocks", "bbbb2222"); err != nil {
		t.Fatalf("same-store Add failed: %v", err)
	}
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
go test ./service -run TestRelationService -v
```

Expected: FAIL — constructor signature mismatch.

- [ ] **Step 3: Refactor `RelationService`**

```go
type RelationService struct {
	resolve  BundleResolver
	projects ProjectLister
}

func NewRelationService(resolve BundleResolver, projects ProjectLister) *RelationService {
	return &RelationService{resolve: resolve, projects: projects}
}
```

In `Add`, resolve source and target bundles. Reuse the `bundleForShortID` pattern from Task 1 — either copy the helper into a shared location (e.g. a new unexported method `findBundleByShortID` on a shared type) or duplicate it across services. Duplicating is acceptable here; keep them side-by-side.

```go
func (s *RelationService) Add(ctx context.Context, sourceShortID, kind, targetShortID string) (*domain.Relation, error) {
	sourceBundle, sourceTask, err := s.findTask(ctx, sourceShortID)
	if err != nil {
		return nil, err
	}
	targetBundle, targetTask, err := s.findTask(ctx, targetShortID)
	if err != nil {
		return nil, err
	}
	if sourceBundle != targetBundle {
		return nil, domain.ErrCrossStoreRelation
	}
	// Existing cycle-detection / dup / typed-edge logic runs against
	// sourceBundle.Relations and sourceBundle.Tasks unchanged.
	// ... existing body, with s.taskRepo replaced by sourceBundle.Tasks,
	// s.relationRepo replaced by sourceBundle.Relations,
	// s.tx (tx provider) replaced by sourceBundle.Store ...
}

func (s *RelationService) findTask(ctx context.Context, shortID string) (*RepoBundle, *domain.Task, error) {
	ids, err := s.projects(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, pid := range ids {
		bundle, err := s.resolve(ctx, pid)
		if err != nil {
			return nil, nil, err
		}
		task, err := bundle.Tasks.GetByShortID(ctx, shortID)
		if err == nil {
			return bundle, task, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, nil, err
		}
	}
	return nil, nil, domain.ErrNotFound
}
```

The point of comparing `sourceBundle != targetBundle` by pointer is that two projects sharing the default store return the same `*RepoBundle` pointer (Phase 3 guarantees this through the bundle cache in `cmd/tusk/main.go`). Same-store relations therefore still succeed.

- [ ] **Step 4: Re-run tests**

```bash
go test ./service -run TestRelationService -v
go test ./service/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add service/relation.go service/relation_test.go
git commit -m "feat(service): route RelationService through resolver and reject cross-store edges"
```

---

### Task 3: Refactor `TagService` + fan out `Task.List`, `Available`, `Next`, `Pop`

**Files:**
- Modify: `service/tag.go`
- Modify: `service/tag_test.go`
- Modify: `service/task.go` (already partially refactored in Task 1)
- Modify: `service/task_test.go`

`TagService` is the simplest refactor: it currently has one dependency (`tagRepo`) and its operations are not task-scoped by design (tags are a flat namespace per PRODUCT.md). **Keep tag definitions in the default store.** Replace `tagRepo` with a field that always resolves to the default bundle's `Tags` repo:

```go
type TagService struct {
	resolve BundleResolver
}

func NewTagService(resolve BundleResolver) *TagService {
	return &TagService{resolve: resolve}
}

func (s *TagService) defaultTags(ctx context.Context) (repository.TagRepository, error) {
	bundle, err := s.resolve(ctx, config.DefaultProjectID)
	if err != nil {
		return nil, err
	}
	return bundle.Tags, nil
}
```

Tag-to-task junctions live inside each task's own store (the junction rows are in the same `tags` table schema that lives in every SQLite file). When `TaskService.AddTag(taskID, tagName)` executes, it resolves the task's bundle and uses that bundle's `Tags` repo for the junction write; the tag definition lookup still goes through the default store. **This means tag definitions exist only in the default store; task-tag junctions exist per project store.** Document this in a comment inside `TagService` so future readers know the split.

The fan-out for `List`/`Available`/`Next`/`Pop` is the main implementation of this task.

- [ ] **Step 1: Write the failing fan-out tests**

Append to `service/task_test.go`:

```go
func TestTaskService_ListFansOutAcrossStores(t *testing.T) {
	ctx := context.Background()
	defaultBundle := newTestBundle(t)
	backendBundle := newTestBundle(t)

	resolver := func(_ context.Context, projectID string) (*RepoBundle, error) {
		switch projectID {
		case "default":
			return defaultBundle, nil
		case "backend":
			return backendBundle, nil
		}
		return nil, fmt.Errorf("unknown project %q", projectID)
	}
	projects := func(context.Context) ([]string, error) { return []string{"default", "backend"}, nil }
	svc := newTaskServiceForTest(t, resolver, projects, []string{"default", "backend"})

	if _, err := svc.Create(ctx, CreateTaskInput{Title: "d1", Project: "default"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, CreateTaskInput{Title: "d2", Project: "default"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, CreateTaskInput{Title: "b1", Project: "backend"}); err != nil {
		t.Fatal(err)
	}

	all, err := svc.List(ctx, domain.FilterExpr{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 merged tasks, got %d", len(all))
	}
}

func TestTaskService_ListProjectFilterNarrowsStores(t *testing.T) {
	ctx := context.Background()
	defaultBundle := newTestBundle(t)
	backendBundle := newTestBundle(t)

	resolver := func(_ context.Context, projectID string) (*RepoBundle, error) {
		switch projectID {
		case "default":
			return defaultBundle, nil
		case "backend":
			return backendBundle, nil
		}
		return nil, fmt.Errorf("unknown project %q", projectID)
	}
	projects := func(context.Context) ([]string, error) { return []string{"default", "backend"}, nil }
	svc := newTaskServiceForTest(t, resolver, projects, []string{"default", "backend"})

	if _, err := svc.Create(ctx, CreateTaskInput{Title: "d1", Project: "default"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, CreateTaskInput{Title: "b1", Project: "backend"}); err != nil {
		t.Fatal(err)
	}

	expr, err := filter.Parse("project=backend")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.List(ctx, expr)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "b1" {
		t.Fatalf("expected [b1], got %+v", got)
	}
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
go test ./service -run "TestTaskService_List" -v
```

Expected: FAIL — `List` still hits a single repo.

- [ ] **Step 3: Implement fan-out**

In `service/task.go`:

```go
func (s *TaskService) List(ctx context.Context, filterExpr domain.FilterExpr) ([]*domain.Task, error) {
	projectIDs, err := s.targetProjects(ctx, filterExpr)
	if err != nil {
		return nil, err
	}
	var all []*domain.Task
	for _, pid := range projectIDs {
		bundle, err := s.resolve(ctx, pid)
		if err != nil {
			return nil, err
		}
		rows, err := bundle.Tasks.List(ctx, filterExpr)
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
	}
	s.sortByUrgency(ctx, all)
	return all, nil
}

// targetProjects returns the project IDs whose stores should be queried
// for this filter. If the filter contains a project=<names> clause, only
// those stores are queried; otherwise every known project is fanned out to.
func (s *TaskService) targetProjects(ctx context.Context, filterExpr domain.FilterExpr) ([]string, error) {
	if names := projectNamesFromFilter(filterExpr); len(names) > 0 {
		return names, nil
	}
	return s.projects(ctx)
}

// projectNamesFromFilter extracts project=<name>[,<name>...] values from
// a parsed filter. Returns nil if no project clause is present.
func projectNamesFromFilter(filterExpr domain.FilterExpr) []string {
	field := filterExpr.GetField("project") // see caveat below
	if field == nil {
		return nil
	}
	return field.Values
}
```

**Caveat:** `domain.FilterExpr` is the service-layer filter type. It may be a wrapper around `syntax.FilterSet` or a distinct AST. Before writing this code, open `domain/filter.go` (or wherever `FilterExpr` is defined) and confirm: (a) whether it exposes field lookup by key directly, (b) if not, which field on it holds the parsed `syntax.FilterSet` so you can delegate to `FilterSet.GetField("project")`. Adjust `projectNamesFromFilter` to match. If `FilterExpr` is just a thin wrapper like `type FilterExpr struct { Set *syntax.FilterSet }`, the body becomes `filterExpr.Set.GetField("project")` and extracting the values depends on `FieldFilter`'s shape.

The existing `sortByUrgency(ctx, tasks)` helper on `TaskService` may or may not exist in the current codebase — grep: `grep -n 'sortByUrgency\|urgency' service/task.go`. If no helper exists, extract the inline sorting logic from wherever tasks are currently urgency-sorted (likely inside `Available` or `Next`) and give it a name.

Apply the same fan-out pattern to `Available`, `Next`, and `Pop`:

```go
func (s *TaskService) Available(ctx context.Context, filterExpr domain.FilterExpr) ([]*domain.Task, error) {
	projectIDs, err := s.targetProjects(ctx, filterExpr)
	if err != nil {
		return nil, err
	}
	var all []*domain.Task
	for _, pid := range projectIDs {
		bundle, err := s.resolve(ctx, pid)
		if err != nil {
			return nil, err
		}
		// Re-use the existing availability filter body against bundle.Tasks / bundle.Relations.
		rows, err := s.availableInBundle(ctx, bundle, filterExpr)
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
	}
	s.sortByUrgency(ctx, all)
	return all, nil
}
```

Extract the existing single-store availability body into `availableInBundle(ctx, bundle, filterExpr)` so that `Available`, `Next`, and `Pop` all call the same shared worker.

For `Pop`:

```go
func (s *TaskService) Pop(ctx context.Context, playerID string, filterExpr domain.FilterExpr) (*domain.Task, error) {
	const maxRetries = 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		candidates, err := s.Available(ctx, filterExpr)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			return nil, domain.ErrNoAvailableTasks
		}
		top := candidates[0]
		bundle, err := s.resolve(ctx, top.ProjectID)
		if err != nil {
			return nil, err
		}
		claimed, err := s.claimAndStartInBundle(ctx, bundle, top, playerID)
		if errors.Is(err, domain.ErrConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return claimed, nil
	}
	return nil, domain.ErrConflict
}
```

`claimAndStartInBundle` is the current body of `Pop` below the availability selection, parameterized on `bundle` instead of `s.taskRepo` / `s.tx`.

- [ ] **Step 4: Re-run tests**

```bash
go test ./service -run "TestTaskService" -v
go test ./service/...
```

Expected: PASS.

- [ ] **Step 5: Refactor `TagService`**

Replace `service/tag.go`:

```go
package service

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/repository"
)

// TagService manages the global tag namespace. Tag definitions live in
// the default store; task-tag junctions live in each task's own store
// (written by TaskService.AddTag).
type TagService struct {
	resolve BundleResolver
}

func NewTagService(resolve BundleResolver) *TagService {
	return &TagService{resolve: resolve}
}

func (s *TagService) definitionsRepo(ctx context.Context) (repository.TagRepository, error) {
	bundle, err := s.resolve(ctx, config.DefaultProjectID)
	if err != nil {
		return nil, fmt.Errorf("resolving default store for tags: %w", err)
	}
	return bundle.Tags, nil
}

// Replace every method body that currently uses s.tagRepo with a call to
// s.definitionsRepo(ctx) first, then use the returned repo. Preserve
// existing method signatures exactly.
```

Update `service/tag_test.go` to construct the service via `NewTagService(resolver)` where the resolver is the one-bundle form from the earlier tests.

- [ ] **Step 6: Run the full service suite and iterate**

```bash
go test ./service/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add service/task.go service/task_test.go service/tag.go service/tag_test.go
git commit -m "feat(service): fan out Task read paths and route TagService via resolver"
```

---

### Task 4: Guard cross-store project migration in `TaskService.Update`

**Files:**
- Modify: `service/task.go`
- Modify: `service/task_test.go`

`domain.TaskUpdate.ProjectID` exists and is applied by `TaskService.Update` around line 360-362 (pre-refactor lines). Today it lets a task change projects freely. With per-project stores, this is dangerous — the row is in the old project's SQLite file, and `Update` currently writes back into the same store it read from. Honoring `ProjectID=<new>` would leave a stale row in the old store and create nothing in the new store.

Scope decision: **reject** cross-store project changes. Migration is out of scope for this initiative. Same-store moves (both projects share the default store) are still allowed.

- [ ] **Step 1: Write the failing test**

Append to `service/task_test.go`:

```go
func TestTaskService_UpdateRejectsCrossStoreProjectChange(t *testing.T) {
	ctx := context.Background()
	defaultBundle := newTestBundle(t)
	backendBundle := newTestBundle(t)
	resolver := func(_ context.Context, projectID string) (*RepoBundle, error) {
		switch projectID {
		case "default":
			return defaultBundle, nil
		case "backend":
			return backendBundle, nil
		}
		return nil, fmt.Errorf("unknown project %q", projectID)
	}
	projects := func(context.Context) ([]string, error) { return []string{"default", "backend"}, nil }
	svc := newTaskServiceForTest(t, resolver, projects, []string{"default", "backend"})

	task, err := svc.Create(ctx, CreateTaskInput{Title: "t", Project: "default"})
	if err != nil {
		t.Fatal(err)
	}
	newProj := "backend"
	_, err = svc.Update(ctx, task.ID, domain.TaskUpdate{ProjectID: &newProj, Version: task.Version})
	if !errors.Is(err, domain.ErrCrossStoreRelation) {
		t.Fatalf("expected ErrCrossStoreRelation, got %v", err)
	}
}
```

(The error reuses `ErrCrossStoreRelation` — the semantic is the same: "operation would span two SQLite files." If you prefer a separate sentinel, add `ErrCrossStoreMove` in `domain/errors.go` in this step and use it instead. Either works; pick one and document it in the error message.)

- [ ] **Step 2: Run and confirm failure**

```bash
go test ./service -run TestTaskService_UpdateRejectsCrossStoreProjectChange -v
```

Expected: FAIL.

- [ ] **Step 3: Add the guard**

In `service/task.go` `Update`, before applying `ProjectID`:

```go
if update.ProjectID != nil && *update.ProjectID != task.ProjectID {
	oldBundle, err := s.resolve(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	newBundle, err := s.resolve(ctx, *update.ProjectID)
	if err != nil {
		return nil, err
	}
	if oldBundle != newBundle {
		return nil, fmt.Errorf("moving task between project stores is not supported: %w", domain.ErrCrossStoreRelation)
	}
}
```

- [ ] **Step 4: Re-run tests**

```bash
go test ./service -run TestTaskService_Update -v
go test ./service/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add service/task.go service/task_test.go
git commit -m "feat(service): reject cross-store task project migration in Update"
```

---

### Task 5: Remove the Phase 3 bridge and wire the real resolver

**Files:**
- Modify: `cmd/tusk/main.go`

**This task removes the bridge code tagged in Phase 3 Task 4.** After this task, `cmd/tusk/main.go` no longer constructs individual repos directly — every service receives a `BundleResolver` closure backed by `StoreRegistry` with a per-store bundle cache.

- [ ] **Step 1: Replace the wiring block**

Find the Phase 3 bridge comment (`// BRIDGE (remove in Phase 4 Task 5):`). Replace everything from `defaultStore, err := registry.Default()` through the last `service.New*(...)` call with:

```go
bundleCache := struct {
	sync.Mutex
	m map[*sqlite.Store]*service.RepoBundle
}{m: map[*sqlite.Store]*service.RepoBundle{}}

bundleFor := func(store *sqlite.Store) *service.RepoBundle {
	bundleCache.Lock()
	defer bundleCache.Unlock()
	if b, ok := bundleCache.m[store]; ok {
		return b
	}
	db := store.DB()
	b := &service.RepoBundle{
		Store:       store,
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
		Relations:   sqlite.NewRelationRepo(db),
		Tags:        sqlite.NewTagRepo(db),
		Players:     sqlite.NewPlayerRepo(db),
	}
	bundleCache.m[store] = b
	return b
}

resolver := func(ctx context.Context, projectID string) (*service.RepoBundle, error) {
	store, err := registry.Get(projectID)
	if err != nil {
		return nil, err
	}
	return bundleFor(store), nil
}

projectLister := func(ctx context.Context) ([]string, error) {
	return registry.ProjectIDs(), nil
}

projectRepo := inmem.NewProjectRepository(cfg.Projects)
workflowRepo := inmem.NewWorkflowRepository(cfg.Workflows)
workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)

urgencyEngine := service.NewUrgencyEngine(service.UrgencyWeights{
	Priority:    cfg.Urgency.PriorityWeight,
	Due:         cfg.Urgency.DueWeight,
	Age:         cfg.Urgency.AgeWeight,
	Active:      cfg.Urgency.ActiveWeight,
	Blocking:    cfg.Urgency.BlockingWeight,
	Blocked:     cfg.Urgency.BlockedWeight,
	Tags:        cfg.Urgency.TagsWeight,
	Project:     cfg.Urgency.ProjectWeight,
	Annotations: cfg.Urgency.AnnotationsWeight,
	Waiting:     cfg.Urgency.WaitingWeight,
})

taskSvc := service.NewTaskService(resolver, projectLister, projectRepo, workflowSvc, urgencyEngine)
tagSvc := service.NewTagService(resolver)
relationSvc := service.NewRelationService(resolver, projectLister)
projectSvc := service.NewProjectService(projectRepo)

// PlayerService historically takes a PlayerRepository; resolve the default
// bundle's Players repo since players are a global resource.
defaultBundle, err := resolver(context.Background(), config.DefaultProjectID)
if err != nil {
	return fmt.Errorf("resolving default bundle for players: %w", err)
}
playerSvc := service.NewPlayerService(defaultBundle.Players)
```

Add the `sync` and `context` imports if they are not already present.

- [ ] **Step 2: Build and run the full suite**

```bash
go build ./cmd/tusk
make test test-race test-e2e vet lint
```

Expected: PASS.

If any test fails, the most likely causes are:
- A service method that still calls a removed `s.taskRepo` / `s.tx` field (Task 1 missed it) — re-run `grep -n 'taskRepo\|relationRepo\|s.tx\b' service/`
- `PlayerService` is created before `registry.Default()` is reachable because players are resolved lazily elsewhere — adjust wiring order
- `ProjectService` no longer accepts the arguments above — re-read its current signature

- [ ] **Step 3: Manual smoke**

```bash
./bin/tusk add "smoke after phase 4"
./bin/tusk list
./bin/tusk pop --player smoke-player
```

Expected: same behavior as before the phase for the single-store case.

- [ ] **Step 4: Commit**

```bash
git add cmd/tusk/main.go
git commit -m "refactor(cmd): wire resolver-backed services and remove Phase 3 bridge"
```

---

### Task 6: End-to-end test with two project databases

**Files:**
- Create: `tests/e2e/project_db_path_test.go`

- [ ] **Step 1: Inspect harness for DB-path support**

```bash
grep -n 'TempDir\|ConfigFile\|Setup\|Capture' tests/e2e/harness.go
```

Confirm whether the harness gives scenarios a way to (a) register setup steps, (b) substitute temp paths into command args, (c) capture task short IDs for later steps. Adapt Step 2 below to the actual field names.

- [ ] **Step 2: Write the scenarios**

```go
package e2e

import "testing"

func TestPerProjectDatabase(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "isolated_stores_merged_on_read",
			Steps: []Step{
				{Args: []string{"project", "create", "backend", "workflow=kanban", "db-path=$TMP/backend.db"}},
				{Args: []string{"project", "create", "frontend", "workflow=kanban", "db-path=$TMP/frontend.db"}},
				{Args: []string{"add", "backend task", "project=backend", "priority=4"}},
				{Args: []string{"add", "frontend task", "project=frontend", "priority=2"}},
				{Args: []string{"list"}, ExpectContains: []string{"backend task", "frontend task"}},
				{Args: []string{"pop", "--player", "german"}, ExpectContains: []string{"backend task"}},
			},
		},
		{
			Name: "cross_store_relation_rejected",
			Steps: []Step{
				{Args: []string{"project", "create", "backend", "workflow=kanban", "db-path=$TMP/backend.db"}},
				{Args: []string{"project", "create", "frontend", "workflow=kanban", "db-path=$TMP/frontend.db"}},
				{Args: []string{"add", "backend task", "project=backend"}, Capture: "b"},
				{Args: []string{"add", "frontend task", "project=frontend"}, Capture: "f"},
				{Args: []string{"link", "$b.short_id", "blocks", "$f.short_id"}, ExpectError: true, ExpectContains: []string{"cross-store"}},
			},
		},
		{
			Name: "cross_store_project_move_rejected",
			Steps: []Step{
				{Args: []string{"project", "create", "backend", "workflow=kanban", "db-path=$TMP/backend.db"}},
				{Args: []string{"add", "task", "project=default"}, Capture: "t"},
				{Args: []string{"modify", "$t.short_id", "project=backend"}, ExpectError: true, ExpectContains: []string{"cross-store"}},
			},
		},
	}
	RunScenarios(t, scenarios)
}
```

`$TMP` is a placeholder — if the harness provides a per-scenario temp dir token, use its literal form (e.g. `{{.TempDir}}` or `$TEMP`). Otherwise add a helper that constructs the path via `t.TempDir()` before `RunScenarios` is called.

- [ ] **Step 3: Run and iterate until green**

```bash
go test ./tests/e2e -run TestPerProjectDatabase -v
make test test-race test-e2e vet lint
```

Expected: all targets pass.

- [ ] **Step 4: Update `ROADMAP.md`**

Check the boxes under the Project Management CLI initiative (ROADMAP.md lines 547-564) and mark the initiative complete alongside the other completed initiatives in the same section.

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/project_db_path_test.go ROADMAP.md
git commit -m "test(e2e): cover per-project SQLite databases end-to-end"
```

---

## Changes Introduced

**New files:**
- `tests/e2e/project_db_path_test.go`

**Modified files:**
- `service/task.go` — `TaskService` struct and constructor signature changed; every repo access routed through `BundleResolver`; `List`/`Available`/`Next`/`Pop` now fan out across project stores; cross-store project move in `Update` rejected
- `service/task_test.go` — every `NewTaskService` call site updated to use `newTaskServiceForTest`; new tests for routing, fan-out, project-filter narrowing, and cross-store move guard
- `service/relation.go` — `RelationService` struct and constructor signature changed; `Add` uses pointer-equal bundle check to enforce `ErrCrossStoreRelation`
- `service/relation_test.go` — constructor call sites updated; cross-store and same-store coverage added
- `service/tag.go` — `TagService` now holds a `BundleResolver`; operates on the default bundle's `Tags` repo
- `service/tag_test.go` — updated constructor call sites
- `cmd/tusk/main.go` — **Phase 3 bridge removed**; new resolver/bundle-cache wiring; every service built via `BundleResolver`
- `ROADMAP.md` — Project Management CLI initiative marked complete

**New user-visible feature:**
- Per-project SQLite databases work end-to-end. Tasks created in a project with `db_path=<file>` live in that file. Cross-project reads merge results. Cross-store relations and cross-store project moves are rejected with clean errors.

**Bridge code introduced:** None. (This phase removes Phase 3's bridge.)

**Migrations / env vars / dependencies:** None added. The embedded migrations run on every new per-project file on first open (already handled by `sqlite.New`, re-used through the registry).

**Outstanding caveats the implementer should note:**

1. **Tag definition / junction split.** Tag definitions live in the default store; tag-task junctions live in each task's own store. Listing tags for a task in a non-default project still works because `TaskService.ListTagsForTask` resolves the task's bundle and reads the junction table locally; fetching full tag metadata (color, etc.) requires a second hop to the default store. The Phase 4 test coverage does not exercise tag color rendering in multi-store scenarios — flag this as a known limitation if it surfaces in manual testing.

2. **Performance of `bundleForShortID`.** Every `tusk info <short_id>` call now iterates up to N stores. Typical N is single digits; if a user defines many projects, this becomes hot. The plan does not optimize this — if profiling later shows it matters, introduce a `short_id → project_id` lookup table in the default store and query it first.

3. **Transactions across stores.** Transactions are per-store only. `TaskService.Create` runs inside a single bundle's `Store`; `RelationService.Add` runs inside the same store for both endpoints (guard enforces this). No operation in this initiative spans multiple transactions across stores, so this is consistent.
