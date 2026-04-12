# Phase 1 — Runtime Collapse to a Single Workspace Store

> **For the implementer:** This is the authoritative phase doc. The design spec in `PRODUCT.md` and the other phase doc in this directory (`phase-2-dead-code-and-docs.md`) are reference material only. Do **not** execute any Phase 2 work in this phase — Phase 2 is a separate implementer agent's responsibility.

**Goal:** Route every service operation through a single workspace bundle opened from `cfg.Storage.Path`, so that all projects in a config file share one database and cross-project reads/relations work without fan-out. Ship the behavior change end-to-end while leaving Phase 2's dead-code cleanup for the next implementer.

**Prerequisites:** None. This phase starts from `main` as of 2026-04-12.

**Tech Stack:** Go 1.22+, SQLite (WAL mode), Cobra CLI, existing `service.RepoBundle` / `service.BundleResolver` abstractions.

## Background the implementer needs

Read these before starting — they are the only parts of the codebase that should touch this phase:

- `cmd/tusk/main.go` — current wiring: opens `sqlite.StoreRegistry`, builds a resolver closure, calls `service.NewTaskService` / `NewRelationService` / `NewTagService`.
- `client.go` — the importable `tusk.NewClient` library entry point, parallel wiring to `cmd/tusk` but simpler.
- `service/task.go` — the full `TaskService`. Look at `bundleForShortID` (lines 62–81), `bundleForID` (lines 85–104), `List` (204–225), `listInBundle` (230–274), `targetProjects` (287–292), `projectNamesFromFilter` (298–340), `Next` (345–391), `Available` (820–858), `Pop` (898–932), `Update` (464–482 cross-store guard).
- `service/relation.go` — `findTask` (38–57) and the `sourceBundle != targetBundle` guards in `Add` (87–89) and `Remove` (136–138).
- `service/repos.go` — `RepoBundle`, `BundleResolver`, `ProjectLister` type declarations. Do **not** rename or delete these types.
- `service/task_routing_test.go` — existing multi-store routing tests using `twoProjectTaskSvc`. These will be rewritten.
- `service/bundle_helpers_test.go` — `newTestBundle`, `singleBundleResolver`, `multiBundleResolver`. `multiBundleResolver` will be deleted.
- `service/relation_crossstore_test.go` — the cross-store rejection test file. Delete the whole file.
- `tests/e2e/project_db_path_test.go` — three e2e scenarios that use `db-path=...` to set up per-project DBs. Delete the whole file.
- `domain/errors.go` — contains `ErrCrossStoreRelation` (around line 24). Delete the sentinel.
- `sqlite/registry.go` — the legacy `StoreRegistry` and `resolveDBPath` helper. **Do not touch this file in Phase 1.** Phase 2 deletes it. This phase leaves it as dead code (no longer imported by `cmd/tusk` or `client.go`, but still compiles on its own and its own tests still run).

The following pieces are **deliberately left alone** in Phase 1 and will be cleaned up in Phase 2:

- `ProjectConfig.DBPath` field (`config/config.go:88`) — still exists, still parsed from TOML, but now ignored at runtime.
- `ProjectMutation.DBPath` field (`config/project.go:66`) and its apply block.
- `internal/tui/project_parse.go` `db-path` branches — still accept the field, still write to `ProjectMutation.DBPath`, still no-op effect.
- `sqlite/registry.go` and `sqlite/registry_test.go`.

All four are bridge code, tagged for removal in **Phase 2**.

---

## User-visible behavior contract

After Phase 1 ships, the following must all work the same way they worked before:

- `tusk add`, `tusk list`, `tusk info`, `tusk tree`, `tusk modify`, `tusk start`, `tusk done`, `tusk delete`, `tusk pop`, `tusk claim`, `tusk release`, `tusk available`, `tusk next`, `tusk link`, `tusk unlink`, `tusk annotate`, `tusk project create`, `tusk project modify`, `tusk project delete`.
- Project filters: `tusk list project=backend` still returns only backend tasks (the filter now applies via SQL `WHERE project_id = ?`, not via store routing).
- Optimistic locking: unchanged.
- Workflow transitions and urgency scoring: unchanged.

Intentional behavior changes (document these in your completion notes):

- **Cross-project relations now succeed.** `tusk link taskA blocks taskB` across two projects inside the same workspace was rejected before with `ErrCrossStoreRelation`; it now works. This is the point of the initiative.
- **Cross-project task moves now succeed.** `tusk modify <short_id> project=backend` on a task that was in `default` used to fail; it now works.
- **Per-project `db_path` is silently ignored.** Users who had `[projects.backend].db_path = "/some/file.db"` will find that backend tasks now go to `cfg.Storage.Path` instead. This is a breaking change, but Phase 2 ships the migration doc. Do not attempt to warn at load time — Phase 2 may add that, but Phase 1 is intentionally silent.

---

## Task 1: Swap `cmd/tusk` and `client.go` wiring to a single workspace bundle

**Files:**
- Create: `sqlite/paths.go`
- Modify: `cmd/tusk/main.go:33-124`
- Modify: `client.go:130-177`

- [ ] **Step 1: Add `sqlite/paths.go` with `ResolveWorkspacePath`**

Create `sqlite/paths.go`:

```go
// Package sqlite — paths.go holds path-resolution helpers used when
// wiring a workspace store from a config file's storage.path.
package sqlite

import (
    "os"
    "path/filepath"
)

// ResolveWorkspacePath expands a leading ~ and returns an absolute path.
// Relative paths are resolved against baseDir. Absolute paths are
// returned untouched (cleaned only). Empty baseDir falls back to the
// process working directory.
func ResolveWorkspacePath(path, baseDir string) (string, error) {
    if len(path) > 1 && path[0] == '~' && (path[1] == '/' || path[1] == os.PathSeparator) {
        home, err := os.UserHomeDir()
        if err != nil {
            return "", err
        }
        path = filepath.Join(home, path[2:])
    }
    if filepath.IsAbs(path) {
        return filepath.Clean(path), nil
    }
    if baseDir == "" {
        return filepath.Abs(path)
    }
    return filepath.Clean(filepath.Join(baseDir, path)), nil
}
```

Note: `sqlite/registry.go` already contains an unexported `resolveDBPath` with the same body. Leave it alone — Phase 2 deletes the whole file. The duplication is intentional and short-lived.

- [ ] **Step 2: Replace `cmd/tusk/main.go` wiring**

In `cmd/tusk/main.go`, replace lines 50–104 (the `NewStoreRegistry` call through the end of the `projectLister` closure) with:

```go
baseDir := "."
if configPath != "" {
    baseDir = filepath.Dir(configPath)
}
absDB, err := sqlite.ResolveWorkspacePath(dbPath, baseDir)
if err != nil {
    return fmt.Errorf("resolving db path: %w", err)
}
if err := os.MkdirAll(filepath.Dir(absDB), 0o755); err != nil {
    return fmt.Errorf("creating db dir: %w", err)
}
store, err := sqlite.New(absDB, migrations.FS)
if err != nil {
    return fmt.Errorf("opening database: %w", err)
}
defer store.Close()

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

db := store.DB()
bundle := &service.RepoBundle{
    Store:       store,
    Tasks:       sqlite.NewTaskRepo(db),
    Annotations: sqlite.NewAnnotationRepo(db),
    Relations:   sqlite.NewRelationRepo(db),
    Tags:        sqlite.NewTagRepo(db),
    Players:     sqlite.NewPlayerRepo(db),
}

resolver := func(_ context.Context, projectID string) (*service.RepoBundle, error) {
    if _, ok := cfg.Projects[projectID]; !ok {
        return nil, fmt.Errorf("unknown project %q", projectID)
    }
    return bundle, nil
}
projectLister := func(context.Context) ([]string, error) {
    ids := make([]string, 0, len(cfg.Projects))
    for id := range cfg.Projects {
        ids = append(ids, id)
    }
    sort.Strings(ids)
    return ids, nil
}
```

Then in the imports block:
- Remove the `"sync"` import (no longer needed — the `bundleCache` / `bundleMu` pair is gone).
- Remove the `"github.com/germanamz/tusk/sqlite"` import **if** it is no longer referenced (it still is — keep it).
- Add `"sort"` if not already imported.

Leave the rest of the file (`taskSvc`, `tagSvc`, `relationSvc`, `projectSvc`, `playerSvc` construction at original lines 106–118, and `stripDBFlag` / `resolveDBPath` below) untouched.

- [ ] **Step 3: Collapse `client.go` wiring to match**

In `client.go`, replace the block at lines 147–156 (the `projectIDs` slice + resolver + lister) with:

```go
resolver := func(context.Context, string) (*service.RepoBundle, error) {
    return bundle, nil
}
projectLister := func(context.Context) ([]string, error) {
    ids := make([]string, 0, len(cfg.Projects))
    for id := range cfg.Projects {
        ids = append(ids, id)
    }
    return ids, nil
}
```

The bundle declaration immediately above (lines 139–146) already constructs exactly one workspace bundle, so no other changes are needed in `client.go`.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: PASS. Tests will fail at this point — that is expected; subsequent tasks fix them.

- [ ] **Step 5: Commit**

```bash
git add sqlite/paths.go cmd/tusk/main.go client.go
git commit -m "refactor(cmd,client): wire single workspace bundle"
```

---

## Task 2: Simplify `TaskService` — single-resolve reads and no cross-store guard

**Files:**
- Modify: `service/task.go` at the specific line ranges below

- [ ] **Step 1: Replace `bundleForShortID` (current lines 62–81)**

```go
func (s *TaskService) bundleForShortID(ctx context.Context, shortID string) (*RepoBundle, *domain.Task, error) {
    bundle, err := s.resolve(ctx, DefaultProjectID)
    if err != nil {
        return nil, nil, err
    }
    task, err := bundle.Tasks.GetByShortID(ctx, shortID)
    if err != nil {
        return nil, nil, err
    }
    return bundle, task, nil
}
```

- [ ] **Step 2: Replace `bundleForID` (current lines 85–104)**

```go
func (s *TaskService) bundleForID(ctx context.Context, id uuid.UUID) (*RepoBundle, *domain.Task, error) {
    bundle, err := s.resolve(ctx, DefaultProjectID)
    if err != nil {
        return nil, nil, err
    }
    task, err := bundle.Tasks.GetByID(ctx, id)
    if err != nil {
        return nil, nil, err
    }
    return bundle, task, nil
}
```

- [ ] **Step 3: Replace `List` (current lines 204–225)**

```go
func (s *TaskService) List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
    bundle, err := s.resolve(ctx, DefaultProjectID)
    if err != nil {
        return nil, err
    }
    return s.listInBundle(ctx, bundle, filter)
}
```

`targetProjects` and `projectNamesFromFilter` are now unreferenced by `List`, but `Available` still calls `targetProjects`. Do **not** delete the helpers yet — Step 5 replaces `Available`, and Step 6 is when the helpers are safe to remove.

- [ ] **Step 4: Replace `Next` (current lines 345–391)**

```go
func (s *TaskService) Next(ctx context.Context) (*domain.Task, error) {
    nonTerminal, err := s.collectNonTerminalStatuses(ctx)
    if err != nil {
        return nil, err
    }
    bundle, err := s.resolve(ctx, DefaultProjectID)
    if err != nil {
        return nil, err
    }
    filter := &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: nonTerminal}}
    tasks, err := s.listInBundle(ctx, bundle, filter)
    if err != nil {
        return nil, err
    }

    now := time.Now()
    for _, t := range tasks {
        if t.WaitUntil != nil && t.WaitUntil.After(now) {
            continue
        }
        blockedBy, err := bundle.Relations.CountBlockedByTasks(ctx, []uuid.UUID{t.ID})
        if err != nil {
            return nil, fmt.Errorf("checking blocked status: %w", err)
        }
        if blockedBy[t.ID] > 0 {
            continue
        }
        return t, nil
    }
    return nil, domain.ErrNotFound
}
```

- [ ] **Step 5: Replace `Available` (current lines 820–858)**

```go
func (s *TaskService) Available(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
    nonTerminal, err := s.collectNonTerminalStatuses(ctx)
    if err != nil {
        return nil, err
    }
    baseFilter := &domain.AndFilter{
        Children: []domain.FilterExpr{
            &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: nonTerminal}},
            &domain.TermFilter{TaskFilter: domain.TaskFilter{Unclaimed: ptr(true)}},
        },
    }
    if filter != nil {
        baseFilter.Children = append(baseFilter.Children, filter)
    }

    bundle, err := s.resolve(ctx, DefaultProjectID)
    if err != nil {
        return nil, err
    }
    return s.availableInBundle(ctx, bundle, baseFilter)
}
```

- [ ] **Step 6: Delete `targetProjects`, `projectNamesFromFilter`, and `sortTasksByUrgency`**

All three helpers are now unreferenced by any method in `service/task.go`:

- `targetProjects` was only called by the old `List` / `Available`.
- `projectNamesFromFilter` was only called by `targetProjects`.
- `sortTasksByUrgency` was only called by the old multi-bundle `List` and `Next`. The new versions rely on `listInBundle` for ordering inside a single bundle, so the helper has no remaining callers.

Before deleting, confirm:

Run: `rg -n "targetProjects|projectNamesFromFilter|sortTasksByUrgency" service/`

Expected: matches are limited to the function definitions themselves inside `service/task.go`. Delete all three functions.

Then drop the now-unused `"sort"` import from the top of `service/task.go`. Verify first:

Run: `rg -n "\\bsort\\." service/task.go`

Expected: zero matches (the only user was the `sort.SliceStable` call inside `sortTasksByUrgency`, which you just deleted). If any match remains, keep the import and investigate — a method you did not touch may still use it.

If any other file references `targetProjects` or `projectNamesFromFilter`, stop and flag this to the planning agent — the assumption is wrong and the phase needs revisiting.

- [ ] **Step 7: Update the doc comment on `Pop`**

The current comment (around line 895) says "fanning out across every known project store". Replace with:

```go
// Pop claims and starts the highest-urgency available task for the
// given player. Retries on claim-conflict and optimistic-lock errors.
// Returns domain.ErrNoAvailableTasks if nothing can be claimed.
```

The body of `Pop` is unchanged — it already just calls `Available` and iterates candidates.

- [ ] **Step 8: Delete the cross-store guard in `Update` (current lines 474–482)**

Find and delete this block inside `Update`:

```go
if upd.ProjectID != nil && *upd.ProjectID != task.ProjectID {
    newBundle, err := s.resolve(ctx, *upd.ProjectID)
    if err != nil {
        return nil, err
    }
    if bundle != newBundle {
        return nil, fmt.Errorf("moving task between project stores is not supported: %w", domain.ErrCrossStoreRelation)
    }
}
```

Project existence is still validated further down the function by `s.projectRepo.GetByID(ctx, task.ProjectID)` — that check stays.

- [ ] **Step 9: Build**

Run: `go build ./service/...`
Expected: PASS. Tests still fail; that is fine.

- [ ] **Step 10: Commit**

```bash
git add service/task.go
git commit -m "refactor(service): collapse TaskService reads to a single bundle"
```

---

## Task 3: Simplify `RelationService` and delete `ErrCrossStoreRelation`

**Files:**
- Modify: `service/relation.go:38-141`
- Modify: `domain/errors.go`
- Delete: `service/relation_crossstore_test.go`

- [ ] **Step 1: Replace `findTask` (current lines 38–57)**

```go
func (s *RelationService) findTask(ctx context.Context, shortID string) (*RepoBundle, *domain.Task, error) {
    bundle, err := s.resolve(ctx, "default")
    if err != nil {
        return nil, nil, err
    }
    task, err := bundle.Tasks.GetByShortID(ctx, shortID)
    if err != nil {
        return nil, nil, err
    }
    return bundle, task, nil
}
```

- [ ] **Step 2: Delete cross-store guards in `Add` and `Remove`**

In `Add` (current lines 66–115), delete:

```go
if sourceBundle != targetBundle {
    return nil, domain.ErrCrossStoreRelation
}
```

In `Remove` (current lines 119–141), delete the equivalent block. Also delete the sentences in the `Add` and `Remove` doc comments that mention the cross-store constraint. The `Add` comment should read:

```go
// Add creates a new relation between two tasks identified by short IDs.
//
// For "blocks" relations, the creation is wrapped in a transaction with
// cycle detection. For other types, no cycle check is needed.
```

Apply the parallel trim to `Remove`.

- [ ] **Step 3: Delete `ErrCrossStoreRelation` from `domain/errors.go`**

Find the line declaring `ErrCrossStoreRelation` (around line 24) and delete it along with any adjacent doc comment that describes the cross-store constraint. Leave every other sentinel untouched.

- [ ] **Step 4: Delete `service/relation_crossstore_test.go`**

Run: `rm service/relation_crossstore_test.go`

- [ ] **Step 5: Grep for stragglers**

Run: `rg -n "ErrCrossStoreRelation|CrossStoreRelation" --type go`
Expected: zero matches. Any hit is a missed cleanup — remove it before moving on.

- [ ] **Step 6: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add service/relation.go domain/errors.go service/relation_crossstore_test.go
git commit -m "refactor(service): drop cross-store constraint from RelationService"
```

---

## Task 4: Rewrite service routing tests for single-store reality

**Files:**
- Modify: `service/bundle_helpers_test.go:49-68`
- Modify: `service/task_routing_test.go:1-169`

- [ ] **Step 1: Delete `multiBundleResolver` in `bundle_helpers_test.go`**

Remove the entire `multiBundleResolver` function (lines 49–68). It has no remaining callers after this task. Keep `newTestBundle` and `singleBundleResolver` untouched.

- [ ] **Step 2: Rewrite `service/task_routing_test.go`**

Overwrite the file contents with:

```go
package service

import (
    "context"
    "testing"

    "github.com/germanamz/tusk/config"
    "github.com/germanamz/tusk/domain"
    "github.com/germanamz/tusk/inmem"
)

// multiProjectKanban builds a ProjectRepository with the given project
// IDs, all bound to the kanban workflow used by the test suite.
func multiProjectKanban(projectIDs ...string) *inmem.ProjectRepository {
    cfg := map[string]config.ProjectConfig{}
    for _, id := range projectIDs {
        cfg[id] = config.ProjectConfig{Workflow: "kanban"}
    }
    return inmem.NewProjectRepository(cfg)
}

func multiProjectWorkflowSvc(projectRepo *inmem.ProjectRepository) *WorkflowService {
    workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{
        "kanban": {
            Statuses: map[string]config.StatusConfig{
                "pending":   {Roles: []string{config.RoleInitial}},
                "active":    {Roles: []string{config.RoleStart, config.RoleHighlight}},
                "completed": {Roles: []string{config.RoleTerminal, config.RoleDone, config.RoleDim}},
                "deleted":   {Roles: []string{config.RoleTerminal, config.RoleDelete, config.RoleDim}},
            },
            Transitions: []config.WorkflowTransitionConfig{
                {From: "pending", To: "active"},
                {From: "pending", To: "deleted"},
                {From: "active", To: "completed"},
                {From: "active", To: "pending"},
                {From: "active", To: "deleted"},
                {From: "completed", To: "pending"},
            },
        },
    })
    return NewWorkflowService(workflowRepo, projectRepo)
}

// multiProjectTaskSvc wires a TaskService over a single workspace bundle
// that answers for multiple project IDs.
func multiProjectTaskSvc(t *testing.T) (*TaskService, *RepoBundle) {
    t.Helper()
    bundle := newTestBundle(t)
    resolver, projects := singleBundleResolver(bundle, "default", "backend")
    projectRepo := multiProjectKanban("default", "backend")
    workflowSvc := multiProjectWorkflowSvc(projectRepo)
    svc := NewTaskService(resolver, projects, projectRepo, workflowSvc, nil)
    return svc, bundle
}

func TestTaskService_CreateRoutesToWorkspaceBundle(t *testing.T) {
    ctx := context.Background()
    svc, bundle := multiProjectTaskSvc(t)

    task := &domain.Task{Title: "backend task", ProjectID: "backend"}
    if err := svc.Create(ctx, task); err != nil {
        t.Fatalf("Create: %v", err)
    }

    got, err := bundle.Tasks.GetByID(ctx, task.ID)
    if err != nil {
        t.Fatalf("expected task in workspace bundle: %v", err)
    }
    if got.Title != "backend task" || got.ProjectID != "backend" {
        t.Fatalf("unexpected task %+v", got)
    }
}

func TestTaskService_ListReturnsAllProjectsFromWorkspace(t *testing.T) {
    ctx := context.Background()
    svc, _ := multiProjectTaskSvc(t)

    for _, title := range []string{"d1", "d2"} {
        if err := svc.Create(ctx, &domain.Task{Title: title, ProjectID: "default"}); err != nil {
            t.Fatalf("Create default %q: %v", title, err)
        }
    }
    if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: "backend"}); err != nil {
        t.Fatalf("Create backend: %v", err)
    }

    all, err := svc.List(ctx, nil)
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(all) != 3 {
        t.Fatalf("expected 3 tasks, got %d", len(all))
    }
}

func TestTaskService_ListProjectFilterNarrowsResult(t *testing.T) {
    ctx := context.Background()
    svc, _ := multiProjectTaskSvc(t)

    if err := svc.Create(ctx, &domain.Task{Title: "d1", ProjectID: "default"}); err != nil {
        t.Fatalf("Create default: %v", err)
    }
    if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: "backend"}); err != nil {
        t.Fatalf("Create backend: %v", err)
    }

    backendProject := "backend"
    filter := &domain.TermFilter{TaskFilter: domain.TaskFilter{ProjectID: &backendProject}}
    got, err := svc.List(ctx, filter)
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(got) != 1 || got[0].Title != "b1" {
        t.Fatalf("expected only 'b1', got %+v", got)
    }
}

func TestTaskService_AvailableReturnsAllProjects(t *testing.T) {
    ctx := context.Background()
    svc, _ := multiProjectTaskSvc(t)

    if err := svc.Create(ctx, &domain.Task{Title: "d1", ProjectID: "default"}); err != nil {
        t.Fatalf("Create default: %v", err)
    }
    if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: "backend"}); err != nil {
        t.Fatalf("Create backend: %v", err)
    }

    avail, err := svc.Available(ctx, nil)
    if err != nil {
        t.Fatalf("Available: %v", err)
    }
    if len(avail) != 2 {
        t.Fatalf("expected 2 available, got %d", len(avail))
    }
}

func TestTaskService_UpdateAllowsProjectMoveWithinWorkspace(t *testing.T) {
    ctx := context.Background()
    svc, bundle := multiProjectTaskSvc(t)

    task := &domain.Task{Title: "t", ProjectID: "default"}
    if err := svc.Create(ctx, task); err != nil {
        t.Fatalf("Create: %v", err)
    }
    newProj := "backend"
    updated, err := svc.Update(ctx, domain.TaskUpdate{
        ShortID:   task.ShortID,
        Version:   task.Version,
        ProjectID: &newProj,
    })
    if err != nil {
        t.Fatalf("Update: %v", err)
    }
    if updated.ProjectID != "backend" {
        t.Fatalf("expected project=backend, got %q", updated.ProjectID)
    }

    got, err := bundle.Tasks.GetByID(ctx, task.ID)
    if err != nil {
        t.Fatalf("GetByID: %v", err)
    }
    if got.ProjectID != "backend" {
        t.Fatalf("stored project mismatch: %q", got.ProjectID)
    }
}
```

- [ ] **Step 3: Run the service test package**

Run: `go test ./service/... -count=1`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add service/task_routing_test.go service/bundle_helpers_test.go
git commit -m "test(service): rewrite routing tests for single workspace bundle"
```

---

## Task 5: Delete obsolete e2e scenarios and verify the phase end-to-end

**Files:**
- Delete: `tests/e2e/project_db_path_test.go`
- No other source changes. This task is the phase-level gate.

- [ ] **Step 1: Remove the per-project DB e2e file**

Run: `rm tests/e2e/project_db_path_test.go`

- [ ] **Step 2: Confirm no leftover references**

Run: `rg -n "db-path|perProjectDBConfig|TestPerProjectDatabase" tests/ --type go`
Expected: zero matches.

- [ ] **Step 3: Build and run the full suite with the race detector**

Run: `make build && make test-race`
Expected: PASS.

- [ ] **Step 4: Vet and lint**

Run: `make vet && make lint`
Expected: PASS. If lint reports unused imports or unused local variables in `cmd/tusk/main.go`, delete them (most likely candidates: `sync`).

- [ ] **Step 5: Smoke test cross-project relations**

Use a fresh temp DB to avoid polluting your local state:

```bash
TUSK_DB="$(mktemp -t tusk-phase1-XXXX.db)"
export TUSK_DB
./bin/tusk add "first" project=default
./bin/tusk project create backend workflow=kanban
./bin/tusk add "second" project=backend
./bin/tusk list
./bin/tusk list project=backend
FIRST=$(./bin/tusk list --output json | jq -r '.[0].short_id')
SECOND=$(./bin/tusk list --output json | jq -r '.[1].short_id')
./bin/tusk link "$FIRST" blocks "$SECOND"
./bin/tusk tree
rm "$TUSK_DB"* 2>/dev/null || true
unset TUSK_DB
```

Expected: `list` returns two tasks, `list project=backend` returns one, `link` succeeds (the call would have returned a cross-store error before this phase), `tree` shows the blocking relation.

If any step fails, stop and investigate before committing.

- [ ] **Step 6: Commit any lint cleanup**

```bash
git add -u
git commit -m "test(e2e): delete per-project database scenarios"
```

If the only staged change is `tests/e2e/project_db_path_test.go` (no cleanup was required), the message above still applies. If you also had to touch `cmd/tusk/main.go` for a stray unused import, combine both into the same commit with the same message — the cleanup is incidental to the test removal.

---

## Changes Introduced

**New files:**
- `sqlite/paths.go` — exports `ResolveWorkspacePath(path, baseDir string) (string, error)`.

**Deleted files:**
- `service/relation_crossstore_test.go`
- `tests/e2e/project_db_path_test.go`

**Modified interfaces:**
- `service.TaskService.{bundleForShortID, bundleForID, List, Next, Available, Update}` — implementations collapse to a single `resolve()` call. Signatures unchanged.
- `service.RelationService.{findTask, Add, Remove}` — cross-store guard removed. Signatures unchanged.
- `service.BundleResolver` and `service.ProjectLister` type aliases are **preserved** so downstream initiatives can rewire them; only the implementations wired in `cmd/tusk/main.go` and `client.go` change.

**Deleted symbols:**
- `domain.ErrCrossStoreRelation` (sentinel error).
- `service.TaskService.targetProjects`, `service.projectNamesFromFilter`, and `service.sortTasksByUrgency` (internal helpers, no external callers).
- `"sort"` import in `service/task.go` (the `sort.SliceStable` call inside `sortTasksByUrgency` was its only user).
- `service.multiBundleResolver` test helper.

**Bridge code left in place, tagged for removal in Phase 2:**
| Symbol | Location | Remove in |
|--------|----------|-----------|
| `config.ProjectConfig.DBPath` field | `config/config.go:88` | Phase 2, Task 2 |
| `config.ProjectMutation.DBPath` field + apply block | `config/project.go:66, 159-161` | Phase 2, Task 2 |
| `db-path` case in `applyProjectField` | `internal/tui/project_parse.go:57-58` | Phase 2, Task 3 |
| `db-path` case in `parseProjectModify` | `internal/tui/project_parse.go:137-139` | Phase 2, Task 3 |
| `sqlite.StoreRegistry` type + constructors | `sqlite/registry.go` (whole file) | Phase 2, Task 1 |
| `TestStoreRegistry_*` tests | `sqlite/registry_test.go` (whole file) | Phase 2, Task 1 |

None of these bridges are executed at runtime after Task 1 of this phase — `cmd/tusk/main.go` and `client.go` no longer import `sqlite.StoreRegistry`, and the `DBPath` field is never read by any service. They compile and their local tests still pass, but they are dead weight until Phase 2 removes them.

**No new environment variables, schema migrations, or dependencies.**

**User-visible acceptance criteria for this phase:**
- Every CLI verb listed in the "User-visible behavior contract" above still works.
- Cross-project `tusk link` succeeds inside a workspace.
- Cross-project `tusk modify <id> project=<other>` succeeds inside a workspace.
- `tusk list project=backend` still scopes to a single project via SQL filter.
- `make test-race`, `make vet`, `make lint` all green.
- The smoke script in Task 5 Step 5 runs end-to-end.
