# Phase 4 — Remove `project.Workflow` Compat Field Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the compatibility-only `domain.Project.Workflow string` field introduced in Phase 2 by replacing every consumer with a typed UUID lookup. After this phase, `domain.Project` carries `WorkflowID uuid.UUID` as its only reference to a workflow, matching the persistent schema shape.

**Architecture:** `service/task.go` currently reads `project.Workflow` (a string name) in ~11 places and passes it to `WorkflowService.GetStatusByRole`, `GetStatuses`, `IsTransitionAllowed`, and similar name-keyed methods. To drop the field we add `WorkflowService.GetByID(uuid.UUID)` backed by a new `WorkflowRepository.GetByID(uuid.UUID)` method (implemented for `inmem.WorkflowRepository` via linear scan), introduce a small `workflowName(ctx, project)` helper in `service/task.go`, route every consumer through it, then remove the compat field and its population in `inmem.ProjectRepository.buildProjectMap`.

**Tech Stack:** Go, `github.com/google/uuid`. No new schema migrations or SQLite changes.

## Inherits From

After Phase 3, the codebase state the implementer can rely on:

- `workflows`, `projects`, and `tasks` tables exist with full FK integrity from migrations 003, 004, 005.
- `sqlite.WorkflowRepo` and `sqlite.ProjectRepo` exist with full CRUD. Neither is wired into any service — `WorkflowService` and `ProjectService` are still backed by the `inmem` repositories, which are still populated from config at startup.
- `domain.Task.ProjectID` is `uuid.UUID`. `domain.TaskFilter.ProjectID` is `*uuid.UUID`.
- `domain.DefaultProjectUUID` exists.
- `service.TaskService` has `ResolveProjectName(ctx, name)` and a private `defaultProjectID(ctx)` helper.
- `service.ProjectService` has `GetByID(ctx, uuid.UUID)`.
- `repository.ProjectRepository` has `GetByID(ctx, uuid.UUID)`, `GetByName(ctx, string)`, and `List(ctx)`.
- `repository.WorkflowRepository` has only `GetByName(ctx, string)` and `List(ctx)`. This phase adds `GetByID(ctx, uuid.UUID)` to it.
- `domain.Project` carries `ID uuid.UUID`, `Name string`, `WorkflowID uuid.UUID`, **and `Workflow string`** — the last of which this phase removes.
- `inmem.ProjectRepository.buildProjectMap` populates both `WorkflowID` (via `uuid.NewSHA1(uuid.Nil, []byte("workflow:"+cfg.Workflow))`) and `Workflow` (the raw string). The `inmem.WorkflowRepository` uses the same deterministic UUID scheme for workflow IDs, so `project.WorkflowID` always matches a real workflow row in `inmem.WorkflowRepository` — this phase depends on that invariant.
- `service/task.go` reads `project.Workflow` in these locations: lines near 120, 126, 138, 447, 500, 651, 673, 931, 950, 1021 (line numbers approximate; they may drift during editing).

## Prerequisites

- Phases 1, 2, and 3 must be merged.

---

## Task 1: Add `WorkflowRepository.GetByID(uuid.UUID)` to the Interface

Extend the workflow repository interface and implement it on both `inmem.WorkflowRepository` (linear scan) and — if needed for interface compatibility — `sqlite.WorkflowRepo` (already has it as a concrete method from Phase 1; ensure it still satisfies the interface).

**Files:**
- Modify: `repository/workflow.go`
- Modify: `inmem/workflow.go`
- Modify: `inmem/workflow_test.go`

- [ ] **Step 1: Write the failing inmem test**

Append to `inmem/workflow_test.go`:

```go
func TestWorkflowRepository_GetByID(t *testing.T) {
	workflows := map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: map[string]config.StatusConfig{
				"pending": {},
				"active":  {},
			},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
			},
		},
	}
	repo := inmem.NewWorkflowRepository(workflows)
	ctx := context.Background()

	wf, err := repo.GetByName(ctx, "kanban")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}

	got, err := repo.GetByID(ctx, wf.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "kanban" {
		t.Errorf("got name %q, want kanban", got.Name)
	}
}

func TestWorkflowRepository_GetByID_NotFound(t *testing.T) {
	repo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{})
	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}
```

Ensure the test file imports `github.com/google/uuid`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./inmem/... -run 'TestWorkflowRepository_GetByID' -v`
Expected: FAIL with "undefined: (*inmem.WorkflowRepository).GetByID".

- [ ] **Step 3: Extend the interface**

Edit `repository/workflow.go`:

```go
package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// WorkflowRepository provides read access to workflow definitions.
type WorkflowRepository interface {
	// GetByID returns a workflow by its typed UUID.
	// Returns domain.ErrNotFound if no workflow with that id exists.
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Workflow, error)

	// GetByName returns the workflow with the given name.
	// Returns domain.ErrNotFound if no workflow with that name exists.
	GetByName(ctx context.Context, name string) (*domain.Workflow, error)

	// List returns all workflows, sorted alphabetically by name.
	List(ctx context.Context) ([]*domain.Workflow, error)
}
```

- [ ] **Step 4: Implement `inmem.WorkflowRepository.GetByID`**

Append to `inmem/workflow.go`:

```go
// GetByID returns a defensive copy of the workflow matched by UUID.
// Returns domain.ErrNotFound if not found.
func (r *WorkflowRepository) GetByID(_ context.Context, id uuid.UUID) (*domain.Workflow, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, wf := range r.workflows {
		if wf.ID == id {
			return copyWorkflow(wf), nil
		}
	}
	return nil, domain.ErrNotFound
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./inmem/... -run 'TestWorkflowRepository_GetByID' -v`
Expected: PASS.

Run: `make build`
Expected: clean. `sqlite.WorkflowRepo.GetByID` from Phase 1 already matches the new interface signature, so no changes to `sqlite/workflow.go` are required.

- [ ] **Step 6: Commit**

```bash
git add repository/workflow.go inmem/workflow.go inmem/workflow_test.go
git commit -m "feat(repo): add GetByID(uuid.UUID) to WorkflowRepository interface"
```

---

## Task 2: Add `WorkflowService.GetByID(uuid.UUID)`

Expose the new repository method through the service layer so `service/task.go` can resolve a workflow from a `project.WorkflowID` without touching the repository directly.

**Files:**
- Modify: `service/workflow.go`
- Modify: `service/workflow_test.go`

- [ ] **Step 1: Write the failing test**

Append to `service/workflow_test.go`. Copy the existing test-setup helper (lines 15-30 or wherever `NewWorkflowService` is constructed in existing tests) and add:

```go
func TestWorkflowService_GetByID(t *testing.T) {
	workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: map[string]config.StatusConfig{
				"pending": {Roles: []string{"initial"}},
			},
			Transitions: []config.WorkflowTransitionConfig{},
		},
	})
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{})
	svc := service.NewWorkflowService(workflowRepo, projectRepo)

	byName, err := svc.GetByName(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}

	got, err := svc.GetByID(context.Background(), byName.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "kanban" {
		t.Errorf("got name %q, want kanban", got.Name)
	}
}
```

Ensure the test file imports `github.com/google/uuid` if not already present.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./service/... -run 'TestWorkflowService_GetByID' -v`
Expected: FAIL with "undefined: (*service.WorkflowService).GetByID".

- [ ] **Step 3: Implement `GetByID`**

Append to `service/workflow.go`:

```go
// GetByID returns a single workflow by typed UUID.
// Returns domain.ErrNotFound if the workflow does not exist.
func (s *WorkflowService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Workflow, error) {
	return s.workflowRepo.GetByID(ctx, id)
}
```

Ensure `service/workflow.go` imports `github.com/google/uuid`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./service/... -run 'TestWorkflowService_GetByID' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add service/workflow.go service/workflow_test.go
git commit -m "feat(service): add WorkflowService.GetByID(uuid.UUID)"
```

---

## Task 3: Introduce `workflowName` Helper in `service/task.go`

Add a private helper that resolves a project's workflow UUID to a workflow name. Every current `project.Workflow` read site becomes a call to this helper. This concentrates the lookup in one place so the subsequent field deletion (Task 5) only needs to change one function, not 11.

**Files:**
- Modify: `service/task.go`

- [ ] **Step 1: Add the helper**

Append to `service/task.go` (near the existing private helpers like `defaultProjectID` added in Phase 3):

```go
// workflowName resolves the project's workflow UUID to its name via the
// workflow service. It exists to centralize the name-lookup so callers that
// previously read project.Workflow (a compat string field removed in Phase 4)
// can share one code path.
func (s *TaskService) workflowName(ctx context.Context, project *domain.Project) (string, error) {
	wf, err := s.workflowSvc.GetByID(ctx, project.WorkflowID)
	if err != nil {
		return "", fmt.Errorf("looking up workflow %v: %w", project.WorkflowID, err)
	}
	return wf.Name, nil
}
```

Ensure `service/task.go` already imports `fmt` (it does) and `context` (it does).

- [ ] **Step 2: Replace every `project.Workflow` read with a helper call**

Run: `grep -n 'project\.Workflow\b' service/task.go`

Expected hits (approximate line numbers):

| Line | Current | Replacement |
|------|---------|-------------|
| ~120 | `s.workflowSvc.GetStatusByRole(ctx, project.Workflow, domain.RoleInitial)` | Replace with a two-step: `wfName, err := s.workflowName(ctx, project)` then use `wfName` |
| ~126 | `s.workflowSvc.GetStatuses(ctx, project.Workflow)` | Same pattern |
| ~138 | `fmt.Errorf("status %q is not valid for workflow %q", task.Status, project.Workflow)` | Use `wfName` from the earlier call in the function |
| ~447 | `s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, oldStatus, task.Status)` | Same pattern |
| ~500 | `s.workflowSvc.GetStatusByRole(ctx, project.Workflow, domain.RoleStart)` | Same pattern |
| ~651 | `s.workflowSvc.GetStatusByRole(ctx, project.Workflow, domain.RoleDone)` | Same pattern |
| ~673 | `s.workflowSvc.GetStatusByRole(ctx, project.Workflow, domain.RoleDelete)` | Same pattern |
| ~931 | `s.workflowSvc.GetDeleteStatus(ctx, project.Workflow)` | Same pattern |
| ~950 | `s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, parent.Status, cfg.TargetStatus)` | Same pattern |
| ~1021 | `s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, parent.Status, revertCfg.TargetStatus)` | Same pattern |

For each function that has multiple `project.Workflow` reads, resolve `workflowName` once at the top of the function after the project is loaded, reuse the result. Example pattern for the `Create` path (around line 96):

Before:
```go
project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
if err != nil {
    if errors.Is(err, domain.ErrNotFound) {
        return fmt.Errorf("project not found: %w", err)
    }
    return fmt.Errorf("looking up project: %w", err)
}

bundle, err := s.resolve(ctx, task.ProjectID)
if err != nil {
    return fmt.Errorf("resolving project store: %w", err)
}

// ... parent check ...

if task.Status == "" {
    initialStatus, err := s.workflowSvc.GetStatusByRole(ctx, project.Workflow, domain.RoleInitial)
    if err != nil {
        return fmt.Errorf("resolving initial status: %w", err)
    }
    task.Status = initialStatus
}
statuses, err := s.workflowSvc.GetStatuses(ctx, project.Workflow)
// ... validation ...
if !validStatus {
    return fmt.Errorf("status %q is not valid for workflow %q", task.Status, project.Workflow)
}
```

After:
```go
project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
if err != nil {
    if errors.Is(err, domain.ErrNotFound) {
        return fmt.Errorf("project not found: %w", err)
    }
    return fmt.Errorf("looking up project: %w", err)
}

wfName, err := s.workflowName(ctx, project)
if err != nil {
    return err
}

bundle, err := s.resolve(ctx, task.ProjectID)
if err != nil {
    return fmt.Errorf("resolving project store: %w", err)
}

// ... parent check ...

if task.Status == "" {
    initialStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleInitial)
    if err != nil {
        return fmt.Errorf("resolving initial status: %w", err)
    }
    task.Status = initialStatus
}
statuses, err := s.workflowSvc.GetStatuses(ctx, wfName)
// ... validation ...
if !validStatus {
    return fmt.Errorf("status %q is not valid for workflow %q", task.Status, wfName)
}
```

Apply the same pattern to every function the grep surfaced. For functions that load `project` from `parent.ProjectID` (around lines 912 and 996), call `workflowName(ctx, project)` right after the `projectRepo.GetByID` call in that function.

- [ ] **Step 3: Update `service/workflow.go:GetWorkflowWithProjects`**

`service/workflow.go` also reads `p.Workflow` — in `GetWorkflowWithProjects`, the function filters projects by their workflow name. Phase 2 already changed the append to use `p.Name` instead of `p.ID`, so only the filter predicate needs updating here.

Locate the loop in `GetWorkflowWithProjects` (currently around line 110-117):

Before:
```go
var projectIDs []string
for _, p := range projects {
    if p.Workflow == name {
        projectIDs = append(projectIDs, p.Name)
    }
}
sort.Strings(projectIDs)
return wf, projectIDs, nil
```

After (replace the filter with a `WorkflowID` comparison, and resolve `name` to a workflow UUID up front):
```go
targetWF, err := s.workflowRepo.GetByName(ctx, name)
if err != nil {
    return nil, nil, fmt.Errorf("looking up workflow %q: %w", name, err)
}

// ...

var projectIDs []string
for _, p := range projects {
    if p.WorkflowID == targetWF.ID {
        projectIDs = append(projectIDs, p.Name)
    }
}
sort.Strings(projectIDs)
return wf, projectIDs, nil
```

Note: `GetWorkflowWithProjects` already calls `s.workflowRepo.GetByName(ctx, name)` at the top of the function and assigns it to `wf`. Reuse that existing lookup instead of a second one — `targetWF` above is illustrative; in the real edit, just use `wf.ID` in the filter.

- [ ] **Step 4: Compile and run unit tests**

Run: `make build`
Expected: clean.

Run: `go test ./service/...`
Expected: PASS. Any test that asserted on workflow-name-based filtering in `GetWorkflowWithProjects` must still pass — the behavior is identical, only the matching field is different.

- [ ] **Step 5: Commit**

```bash
git add service/task.go service/workflow.go
# Add test files if any changed.
git commit -m "refactor(service): resolve workflow names via WorkflowID lookup"
```

---

## Task 4: Remove `domain.Project.Workflow` Field

With every reader gone, delete the field itself. Delete its population in `inmem.ProjectRepository.buildProjectMap` and `copyProject`. Verify no stray reference survives.

**Files:**
- Modify: `domain/project.go`
- Modify: `inmem/project.go`
- Modify: `service/project_test.go` (update assertion that reads `p.Workflow`)
- Modify: any other file the grep below surfaces

- [ ] **Step 1: Grep for any residual reader**

Run: `grep -rn '\.Workflow\b' --include='*.go' . | grep -v 'WorkflowID\|WorkflowRepository\|WorkflowService\|WorkflowConfig\|WorkflowTransition\|workflowSvc\|workflowRepo\|workflowName'`
Expected: the remaining hits all read `.Workflow` as the compat string field. If the grep returns anything in non-test code under `service/` or `internal/`, fix that reader first — do **not** proceed to Step 2 until non-test code is clean.

Expected residual hits (test files and inmem builder):
- `inmem/project.go` — builder populates the field and `copyProject` copies it
- `service/project_test.go` — asserts on `p.Workflow == "kanban"`
- Possibly a handful of other tests (check `service/*_test.go`, `inmem/*_test.go`)

- [ ] **Step 2: Delete the field from the domain type**

Edit `domain/project.go`. Remove the `Workflow string` line from the `Project` struct:

```go
type Project struct {
	ID         uuid.UUID
	Name       string
	WorkflowID uuid.UUID
	Settings   ProjectSettings
	Version    int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
```

- [ ] **Step 3: Update `inmem.ProjectRepository.buildProjectMap`**

Edit `inmem/project.go`. Remove the `Workflow: cfg.Workflow,` line from the `domain.Project{...}` literal. The `WorkflowID` field continues to be populated via the SHA1 name hash — that stays.

Update `copyProject` to drop the `Workflow: p.Workflow,` line.

- [ ] **Step 4: Update residual test assertions**

Edit `service/project_test.go:34-35`:

Before:
```go
if p.Workflow != "kanban" {
    t.Fatalf("expected Workflow 'kanban', got %q", p.Workflow)
}
```

After:
```go
// Compare via WorkflowID; resolve via the workflow service if the test needs
// the name. For this assertion, the UUID is sufficient — the builder uses
// uuid.NewSHA1(uuid.Nil, []byte("workflow:kanban")).
expectedWorkflowID := uuid.NewSHA1(uuid.Nil, []byte("workflow:kanban"))
if p.WorkflowID != expectedWorkflowID {
    t.Fatalf("expected WorkflowID for kanban, got %v", p.WorkflowID)
}
```

Ensure the test file imports `github.com/google/uuid`.

Run the grep again from Step 1 and fix any remaining hits the same way.

- [ ] **Step 5: Compile and run the full test suite**

Run: `make build`
Expected: clean. If a compile error surfaces on `.Workflow`, that is a reader Step 1 missed — fix it.

Run: `make test`
Expected: PASS.

Run: `make test-race`
Expected: PASS.

Run: `make vet`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add domain/project.go inmem/project.go service/project_test.go
# Add any other files the grep surfaced.
git commit -m "refactor(domain): remove Project.Workflow compat field"
```

---

## Task 5: Final Verification Sweep

Run the full test suite, build the binary, and smoke-test the CLI to confirm no name-keyed workflow lookup regressed.

**Files:**
- None (verification only)

- [ ] **Step 1: Full test sweep**

Run: `make test`
Expected: PASS.

Run: `make test-race`
Expected: PASS.

Run: `make vet`
Expected: clean.

Run: `make lint`
Expected: clean.

- [ ] **Step 2: E2E sweep**

Run: `make test-e2e`
Expected: PASS. If any scenario relies on a workflow-name lookup that used to go through `project.Workflow`, confirm it still resolves correctly via the new `workflowName` helper.

- [ ] **Step 3: Manual CLI smoke test**

```bash
make build
rm -f /tmp/tusk-phase4-smoke.db
./bin/tusk --db /tmp/tusk-phase4-smoke.db task create "round-trip test"
./bin/tusk --db /tmp/tusk-phase4-smoke.db task list
./bin/tusk --db /tmp/tusk-phase4-smoke.db task start $(./bin/tusk --db /tmp/tusk-phase4-smoke.db task list --output json | jq -r '.[0].short_id')
./bin/tusk --db /tmp/tusk-phase4-smoke.db task done $(./bin/tusk --db /tmp/tusk-phase4-smoke.db task list --output json | jq -r '.[0].short_id')
./bin/tusk --db /tmp/tusk-phase4-smoke.db task list
```

Expected: a task is created, started, and completed through the full workflow lifecycle. Any failure implies a `project.Workflow` reader was missed.

- [ ] **Step 4: Residual-reference grep**

Run: `grep -rn 'project\.Workflow\b\|Project\.Workflow\b' --include='*.go' .`
Expected: no hits. Any hit is a missed update.

Run: `grep -rn '"Workflow string"\|Workflow +string' --include='*.go' .`
Expected: no hits on `domain.Project`. Other structs named `Workflow` (if any — e.g. workflow-related types in `config/`) are unaffected.

- [ ] **Step 5: No commit required — verification only**

This task makes no code changes. If any step surfaces a problem, return to Task 3 or Task 4 and fix the root cause. Do not introduce a patch commit from this task.

---

## Acceptance Criteria — User-Visible Behavior Still Works

At the end of this phase, every one of these must still hold:

- `make build`, `make test`, `make test-race`, `make vet`, `make lint`, `make test-e2e`: clean.
- `tusk task create "x"` creates a task bound to the `_default` project.
- `tusk task start`, `tusk task done`, `tusk task delete` transition statuses through the kanban workflow exactly as they did in Phase 3.
- `tusk task modify <id> status=active` (or any direct status modify that triggers an `IsTransitionAllowed` check) rejects invalid transitions with the same error message shape as before — only the workflow-name source has moved from `project.Workflow` to `workflowName(ctx, project)`.
- `tusk workflow list` and `tusk workflow info <name>` still show the config-driven workflows unchanged.
- Auto-complete and auto-revert parent-propagation behaviors (which depend on `IsTransitionAllowed` calls at lines ~950 and ~1021) continue to function.

## Changes Introduced

**New files:**
- None.

**Modified interfaces / types:**
- `repository.WorkflowRepository.GetByID(ctx, uuid.UUID)` added to the interface.
- `inmem.WorkflowRepository.GetByID(uuid.UUID)` added.
- `service.WorkflowService.GetByID(uuid.UUID)` added.
- `service.TaskService.workflowName(ctx, project)` added as a private helper.
- `service.WorkflowService.GetWorkflowWithProjects` rewritten to match by `WorkflowID` rather than string name (internal refactor; callers still pass a string name).
- `domain.Project.Workflow string` **deleted**.
- `inmem.ProjectRepository.buildProjectMap` no longer populates `Workflow`.
- `inmem.ProjectRepository.copyProject` no longer copies `Workflow`.

**Schema migrations:**
- None.

**Dependencies:**
- None.

**Bridge code:**
- None introduced. This phase only **removes** the `domain.Project.Workflow` bridge field introduced in Phase 2. The bridge ledger for this entire plan is now empty.
