# Phase 3 — Tasks FK & `ProjectID` Typing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Change `domain.Task.ProjectID` from `string` (human name) to `uuid.UUID`, rebuild the `tasks` SQLite table with a real FK to `projects.id`, and plumb name→UUID resolution through the service, filter resolver, and TUI layers. At the end of this phase, every task in the database references a project by typed UUID, the database enforces referential integrity, and the user-facing CLI still accepts project names on input and prints them on output.

**Architecture:** The interface `repository.ProjectRepository` gains a `GetByID(uuid.UUID)` method so service call sites can round-trip UUIDs without going through names. `service.TaskService.Create` keeps accepting a `*domain.Task` directly, but a new helper `TaskService.ResolveProjectName(ctx, name)` translates the human-entered string into a UUID the caller stamps onto `task.ProjectID`. The filter resolver gains a `ProjectLookup` dependency so `project=backend` filters can pre-resolve the name into a UUID before the SQLite repo builds its `WHERE project_id = ?` clause. The TUI task renderer gains a per-invocation project-name cache keyed by UUID so it does one `GetByID` lookup per unique project when listing tasks. Migration `005_tasks_project_fk.up.sql` rebuilds the `tasks` table via the classic SQLite swap-table pattern, collapsing all pre-existing `project_id` values to the `_default` UUID (per user direction — no existing deployments need preservation).

**Tech Stack:** Go, SQLite, `github.com/google/uuid`, embedded SQL migrations.

## Inherits From

After Phase 2, the codebase state the implementer can rely on:

- `workflows` and `projects` tables exist in the SQLite schema via migrations 003 and 004. A fresh DB has both tables populated with the seeded `kanban` workflow and `_default` project (both at UUID `00000000-0000-0000-0000-000000000000`).
- `sqlite.WorkflowRepo` and `sqlite.ProjectRepo` exist with full CRUD + `CountByWorkflow`. Neither is wired into any service.
- `domain.Workflow` and `domain.Project` both carry `ID uuid.UUID`, `Version int`, `CreatedAt`, `UpdatedAt`. `domain.Project` also has `Name string`, `WorkflowID uuid.UUID`, and a compatibility `Workflow string` field.
- `repository.ProjectRepository` interface has `GetByName(ctx, string)` and `List(ctx)` only. Phase 2 renamed the old `GetByID(string)` to `GetByName(string)`.
- `inmem.ProjectRepository` and `inmem.WorkflowRepository` remain the live backing stores for `ProjectService` and `WorkflowService`. They still load from config at startup.
- `service/task.go` calls `projectRepo.GetByName(ctx, task.ProjectID)` — `task.ProjectID` is still `string` (holds the human name).
- `domain.Task.ProjectID` is still `string`. `domain.TaskFilter.ProjectID` is still `*string`.
- `sqlite.TaskRepo` still reads/writes `project_id` as raw text. The `tasks.project_id` column has no FK.

## Prerequisites

- Phases 1 and 2 must be merged. This phase's migration references `projects.id` from Phase 2.

---

## Task 1: Add `GetByID(uuid.UUID)` to `ProjectRepository` Interface

Promote `GetByID` onto the interface so service call sites can look projects up by typed UUID once `Task.ProjectID` becomes `uuid.UUID`. The `inmem` implementation gets a UUID→pointer secondary index so lookups stay O(1).

**Files:**
- Modify: `repository/project.go`
- Modify: `inmem/project.go`
- Modify: `inmem/project_test.go`

- [ ] **Step 1: Write the failing inmem test**

Append to `inmem/project_test.go`:

```go
func TestProjectRepository_GetByID(t *testing.T) {
	projects := map[string]config.ProjectConfig{
		"backend": {Workflow: "kanban"},
	}
	repo := inmem.NewProjectRepository(projects)
	ctx := context.Background()

	// Resolve name to get the synthesized UUID.
	p, err := repo.GetByName(ctx, "backend")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}

	byID, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if byID.Name != "backend" {
		t.Errorf("got name %q, want backend", byID.Name)
	}
}

func TestProjectRepository_GetByID_NotFound(t *testing.T) {
	repo := inmem.NewProjectRepository(map[string]config.ProjectConfig{})
	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}
```

Ensure the test file imports `github.com/google/uuid`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./inmem/... -run 'TestProjectRepository_GetByID' -v`
Expected: FAIL with "undefined: (*inmem.ProjectRepository).GetByID".

- [ ] **Step 3: Extend the interface**

Edit `repository/project.go`:

```go
package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// ProjectRepository provides read access to projects.
type ProjectRepository interface {
	// GetByID returns a project by its typed UUID.
	// Returns domain.ErrNotFound if the project doesn't exist.
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)

	// GetByName returns a project by its human-readable name.
	// Returns domain.ErrNotFound if the project doesn't exist.
	GetByName(ctx context.Context, name string) (*domain.Project, error)

	// List returns all projects, sorted by name.
	List(ctx context.Context) ([]*domain.Project, error)
}
```

- [ ] **Step 4: Implement `inmem.ProjectRepository.GetByID`**

Edit `inmem/project.go`. Since `buildProjectMap` keys by `name`, add a linear scan for the UUID lookup — the project set is tiny (O(10) in practice) so a scan is cheap and avoids maintaining a second map:

```go
// GetByID returns a defensive copy of the project matched by UUID.
// Returns domain.ErrNotFound if not found.
func (r *ProjectRepository) GetByID(_ context.Context, id uuid.UUID) (*domain.Project, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.projects {
		if p.ID == id {
			return copyProject(p), nil
		}
	}
	return nil, domain.ErrNotFound
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./inmem/... -run 'TestProjectRepository_GetByID' -v`
Expected: PASS.

Run: `make build`
Expected: clean. If `sqlite.ProjectRepo` no longer satisfies the interface (it should, since it already has a matching `GetByID`), Go will surface it here.

- [ ] **Step 6: Commit**

```bash
git add repository/project.go inmem/project.go inmem/project_test.go
git commit -m "feat(repo): add GetByID(uuid.UUID) to ProjectRepository interface"
```

---

## Task 2: Re-type `domain.Task.ProjectID` and Update `sqlite.TaskRepo`

Change the Task entity's `ProjectID` from `string` to `uuid.UUID`, update the `TaskFilter.ProjectID` to `*uuid.UUID`, and fix `sqlite.TaskRepo` to serialize/deserialize the column as UUID strings. Service, filter, and TUI call sites are updated in subsequent tasks — this task keeps the project adjacent to the SQLite layer.

**Files:**
- Modify: `domain/task.go` — `Task.ProjectID` type
- Modify: `domain/filter.go` — `TaskFilter.ProjectID` type
- Modify: `sqlite/task.go` — scan + insert + update + filter binding
- Modify: `sqlite/task_test.go` — fixture construction

- [ ] **Step 1: Extend domain types**

Edit `domain/task.go`. Change the `ProjectID` field:

Before (find it in the `Task` struct):
```go
ProjectID string
```

After:
```go
ProjectID uuid.UUID
```

Ensure `domain/task.go` imports `github.com/google/uuid`.

Add a named default constant for clarity:

```go
// DefaultProjectUUID is the UUID of the built-in _default project seeded by
// migration 004_projects. Tasks created without an explicit project land here.
var DefaultProjectUUID = uuid.Nil
```

Edit `domain/filter.go`. Change `TaskFilter.ProjectID`:

Before:
```go
ProjectID *string
```

After:
```go
ProjectID *uuid.UUID
```

Add the `uuid` import if not already present.

- [ ] **Step 2: Update `sqlite/task.go`**

Locate the four touchpoints in `sqlite/task.go`:

1. **`Create`** (around line 38) — change:
```go
nullableUUID(task.ParentID), task.ProjectID,
```
to:
```go
nullableUUID(task.ParentID), task.ProjectID.String(),
```

2. **`Update`** (around line 75) — same change:
```go
nullableUUID(task.ParentID), task.ProjectID.String(),
```

3. **Filter WHERE clause** (around lines 178-180):
Before:
```go
if filter.ProjectID != nil {
    conditions = append(conditions, "project_id = ?")
    args = append(args, *filter.ProjectID)
}
```
After:
```go
if filter.ProjectID != nil {
    conditions = append(conditions, "project_id = ?")
    args = append(args, filter.ProjectID.String())
}
```

4. **Scan** (the `scanTask`/`scanOne` helper, around line 404) — find the local `projectID string` variable bound to the `project_id` column and change it to parse into a `uuid.UUID`:

Before (approximate — the exact shape depends on the existing scanner):
```go
var projectID string
// ... scan into &projectID ...
t.ProjectID = projectID
```

After:
```go
var projectID string
// ... scan into &projectID ... (no change)
parsedProjectID, err := uuid.Parse(projectID)
if err != nil {
    return nil, fmt.Errorf("parsing task.project_id: %w", err)
}
t.ProjectID = parsedProjectID
```

Note: the column is still raw text in this task — the FK migration happens in Task 6. Writing `uuid.Nil.String()` yields `"00000000-0000-0000-0000-000000000000"`, a valid text value that does not yet require a matching row because the column has no FK constraint at this point.

- [ ] **Step 3: Update `sqlite/task_test.go` fixtures**

Run: `grep -n 'ProjectID:' sqlite/task_test.go`

For every hit, change `ProjectID: "default"` (or similar string literal) to `ProjectID: domain.DefaultProjectUUID`. If the test constructs ad-hoc string project IDs like `"backend"`, replace them with fresh `uuid.New()` (and add a corresponding `sqlite.NewProjectRepo(store.DB())` Create call earlier in the test if the test actually asserts on the project lookup — otherwise `uuid.Nil` is fine because there is still no FK).

- [ ] **Step 4: Run the tests**

Run: `go test ./domain/... ./sqlite/... -run 'TaskRepo|TestTaskFilter' -v`
Expected: PASS.

Compilation errors will surface in `service/task.go`, `filter/`, and `internal/tui/*`. Those are addressed in Tasks 3-5. For this task it is acceptable for `make build` to fail with errors limited to the call sites fixed in Tasks 3-5.

Do **not** commit yet — Tasks 2 through 5 form a single logical change that only becomes compilation-safe at the end of Task 5. To avoid leaving a broken intermediate commit, stash nothing; stage each task's files and commit at the end of Task 5.

- [ ] **Step 5: Stage (do not commit yet)**

```bash
git add domain/task.go domain/filter.go sqlite/task.go sqlite/task_test.go
```

Tasks 2-5 form a single logical change that only becomes compile-clean at the end of Task 5. Do **not** attempt to commit at the end of Task 2 — the tree will not build until Task 5's final edits land. Execute Tasks 2, 3, 4, and 5 as one continuous edit pass and commit at the end of Task 5 under a single message. The plan's commit checkpoint lives in Task 5 Step 6.

---

## Task 3: Update `service/task.go` Call Sites

`service/task.go` currently passes `task.ProjectID` (string name) to `projectRepo.GetByName` and uses `DefaultProjectID = "default"`. After Task 2, `task.ProjectID` is a UUID. The service needs a mix: look up by UUID when it already has a UUID, look up by name when translating user input, and compare against `domain.DefaultProjectUUID` for the default fallback.

Introduce a small public helper `TaskService.ResolveProjectName(ctx, name)` so CLI/MCP callers that receive a name can stamp the resolved UUID onto the task before calling `Create`.

**Files:**
- Modify: `service/task.go`
- Modify: `service/task_test.go` and other service test files touching `Task.ProjectID`

- [ ] **Step 1: Update the default constant**

Edit `service/task.go`. Replace:

```go
// DefaultProjectID is the string ID of the default project from config.
// Tasks created without an explicit ProjectID are assigned to this project.
const DefaultProjectID = "default"
```

with:

```go
// DefaultProjectName is the name of the built-in default project.
// It resolves to domain.DefaultProjectUUID via ProjectRepository.GetByName.
const DefaultProjectName = "_default"
```

Search for every use of `DefaultProjectID` in the package and update:

Run: `grep -n 'DefaultProjectID' service/`
For each hit, decide: is the caller looking up a project by name (then use `DefaultProjectName` and `GetByName`), or is it comparing a `task.ProjectID` UUID value (then use `domain.DefaultProjectUUID` and `==`)?

- [ ] **Step 2: Update `Create` to look up by UUID**

Around line 92 in `service/task.go`:

Before:
```go
if task.ProjectID == "" {
    task.ProjectID = DefaultProjectID
}

project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
```

After:
```go
if task.ProjectID == uuid.Nil {
    // Caller did not resolve a project name; default to _default.
    defaultProj, err := s.projectRepo.GetByName(ctx, DefaultProjectName)
    if err != nil {
        return fmt.Errorf("looking up default project: %w", err)
    }
    task.ProjectID = defaultProj.ID
}

project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
```

Ensure `github.com/google/uuid` is imported in `service/task.go`.

- [ ] **Step 3: Update every other `projectRepo.GetByName` that passes a UUID**

Run: `grep -n 'projectRepo.GetByName' service/task.go`

For each call site, inspect the argument:
- If it passes `task.ProjectID` (now a UUID): change the call to `projectRepo.GetByID(ctx, task.ProjectID)`.
- If it passes `DefaultProjectName` or a literal string name: keep it as `GetByName`.
- If it passes a variable that came from user input (e.g. `input.Project`): keep it as `GetByName`.

The grep before Task 2 showed these locations in the pre-Phase-3 tree (line numbers approximate and may drift during edits): 96, 307, 433, 443, 496, 647, 669, 912, 996. After Phase 2's rename these are all `GetByName`. After this step, the ones passing `task.ProjectID` / `parent.ProjectID` / looped `projectID` vars become `GetByID`.

- [ ] **Step 4: Update the `resolve` helper and any name-keyed map**

`service/task.go` has a `resolve` method and a bundle cache keyed by project ID (see around line 300 where `seen[t.ProjectID] = true`). Audit this:

Run: `grep -n 'ProjectID\|projectID' service/task.go`

For every map keyed by `string` project ID, decide whether the map should now be keyed by `uuid.UUID` or whether it can be keyed by project name. Prefer `uuid.UUID` — it matches the new field type. Rename affected helper receivers (e.g. `resolve(ctx, projectID uuid.UUID)`). `s.resolve(ctx, DefaultProjectID)` call sites (around lines 55, 59, 71, 180, 244, 701) pass the default string today; change each to pass a UUID — resolve `DefaultProjectName` once near the start of the function or add a helper `s.defaultProjectID(ctx) (uuid.UUID, error)` that does the lookup.

Simpler helper addition at the top of `service/task.go`:

```go
// defaultProjectID resolves DefaultProjectName to its stored UUID.
// Used by entry points that need the _default project but did not receive
// a specific project from the caller.
func (s *TaskService) defaultProjectID(ctx context.Context) (uuid.UUID, error) {
	p, err := s.projectRepo.GetByName(ctx, DefaultProjectName)
	if err != nil {
		return uuid.Nil, fmt.Errorf("looking up default project: %w", err)
	}
	return p.ID, nil
}
```

Replace every `s.resolve(ctx, DefaultProjectID)` with:

```go
defID, err := s.defaultProjectID(ctx)
if err != nil {
    return ..., err
}
bundle, err := s.resolve(ctx, defID)
```

- [ ] **Step 5: Add the public `ResolveProjectName` helper**

Append to `service/task.go` (near the other read-only helpers):

```go
// ResolveProjectName looks up a project by name and returns its UUID.
// CLI and MCP callers use this to translate user-entered project names
// into the typed Task.ProjectID value before calling Create.
// Returns domain.ErrNotFound if the project does not exist.
func (s *TaskService) ResolveProjectName(ctx context.Context, name string) (uuid.UUID, error) {
	if name == "" {
		return s.defaultProjectID(ctx)
	}
	p, err := s.projectRepo.GetByName(ctx, name)
	if err != nil {
		return uuid.Nil, err
	}
	return p.ID, nil
}
```

- [ ] **Step 6: Update `TaskUpdate.ProjectID` flow**

Around lines 372 and 429, `TaskUpdate` applies `*upd.ProjectID` to `task.ProjectID`. If `TaskUpdate.ProjectID` is `*string` today, change it to `*uuid.UUID` in `service/task.go`'s `TaskUpdate` type definition. Every caller that stuffs a name into `upd.ProjectID` must first call `ResolveProjectName` — this is handled in Task 4 (filter) and the TUI/MCP layers (Task 5 / out of scope fallback).

Define the updated `TaskUpdate` field:

Before:
```go
ProjectID **string
```

After:
```go
ProjectID **uuid.UUID
```

Update the double-pointer unpacking at lines 372 and 429 to match; the SQL column remains nullable text so `nil` still means "don't change" and `*nil` means "set NULL" — both remain expressible with `**uuid.UUID`.

- [ ] **Step 7: Update service tests**

Run: `grep -rn 'ProjectID:\s*"' service/`

For each hit in a test file, change `ProjectID: "default"` to `ProjectID: domain.DefaultProjectUUID`, and change other string literals to fresh UUIDs (either `uuid.Nil` where the test only needs the default, or a pre-generated `uuid.MustParse(...)` fixture).

Also search for `"default"` and `"backend"` etc. being passed as the project-id arg to `projectRepo.GetByName` in test helpers — update as needed. The tests must compile; semantic expectations should not need to change because the default project UUID maps to the same logical project.

- [ ] **Step 8: Run service tests**

Run: `go test ./service/... -v`
Expected: PASS. If a test relies on looking up a project-name-as-ID string, it needs to be updated to use `GetByName` or to store the resolved UUID.

- [ ] **Step 9: Stage**

```bash
git add service/task.go service/task_test.go service/task_claim_test.go service/task_routing_test.go service/bundle_helpers_test.go
# Add any other service/ files the grep surfaced.
```

Still no commit — hold until Task 5 finishes.

---

## Task 4: Update `filter/resolve.go` for Name→UUID Resolution

The `project=<name>` filter currently stores the bare string in `domain.TaskFilter.ProjectID`. After Task 2 that field is `*uuid.UUID`. The resolver needs to translate the name into a UUID via `ProjectRepository.GetByName`, and the resolver constructor needs a `ProjectLookup` dependency.

**Files:**
- Modify: `filter/resolve.go` — add `ProjectLookup`, resolve `project=`
- Modify: `filter/resolve_test.go` — provide a fake `ProjectLookup`
- Modify: `filter/integration_test.go` — same
- Modify: `internal/tui/app.go:90` — pass project service into `filter.NewResolver`
- Modify: `internal/mcp/server.go:780` — same

- [ ] **Step 1: Introduce the `ProjectLookup` interface and wire it into the resolver**

Edit `filter/resolve.go`. Add near the existing `TaskLookup` interface:

```go
// ProjectLookup is the subset of project operations the Resolver needs.
type ProjectLookup interface {
	GetByName(ctx context.Context, name string) (*domain.Project, error)
}
```

Update the `Resolver` struct:

```go
type Resolver struct {
	taskLookup      TaskLookup
	projectLookup   ProjectLookup
	defaultStatuses []string
}

func NewResolver(taskLookup TaskLookup, projectLookup ProjectLookup, defaultStatuses []string) *Resolver {
	return &Resolver{
		taskLookup:      taskLookup,
		projectLookup:   projectLookup,
		defaultStatuses: defaultStatuses,
	}
}
```

Update the `project` case in `resolveField`:

Before:
```go
case "project":
    id := field.Value
    tf.ProjectID = &id
```

After:
```go
case "project":
    proj, err := r.projectLookup.GetByName(ctx, field.Value)
    if err != nil {
        if errors.Is(err, domain.ErrNotFound) {
            return fmt.Errorf("project %q not found", field.Value)
        }
        return fmt.Errorf("looking up project %q: %w", field.Value, err)
    }
    id := proj.ID
    tf.ProjectID = &id
```

- [ ] **Step 2: Update existing filter tests**

Edit `filter/resolve_test.go`. The test helper that constructs a `Resolver` must now pass a `ProjectLookup`. Add a minimal fake near the top of the file (only if one does not already exist):

```go
type fakeProjectLookup struct {
	byName map[string]*domain.Project
}

func (f *fakeProjectLookup) GetByName(_ context.Context, name string) (*domain.Project, error) {
	p, ok := f.byName[name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return p, nil
}
```

Construct it in each test that uses `NewResolver`:

```go
defaultUUID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
projects := &fakeProjectLookup{
	byName: map[string]*domain.Project{
		"default": {ID: defaultUUID, Name: "default"},
	},
}
r := filter.NewResolver(taskLookup, projects, []string{"pending", "active"})
```

Update the existing assertion:

Before (`filter/resolve_test.go:81-85`):
```go
if tf.ProjectID == nil {
    t.Fatal("expected ProjectID to be set")
}
if *tf.ProjectID != "default" {
    t.Fatalf("expected ProjectID=%q, got %q", "default", *tf.ProjectID)
}
```

After:
```go
if tf.ProjectID == nil {
    t.Fatal("expected ProjectID to be set")
}
if *tf.ProjectID != defaultUUID {
    t.Fatalf("expected ProjectID=%v, got %v", defaultUUID, *tf.ProjectID)
}
```

Update the "unknown project" test (`resolve_test.go:99-100`) so it expects `ErrNotFound`-wrapped error from the resolver (the resolver now returns an error for unknown projects, because it must look them up). Adjust to whatever the test's intent is — typically, assert that an error was collected by `Resolve` and that `tf.ProjectID` is nil.

Do the same in `filter/integration_test.go:54-55` — change the assertion so it expects a UUID, not the string `"default"`.

- [ ] **Step 3: Update `internal/tui/app.go`**

At `internal/tui/app.go:90`:

Before:
```go
a.resolver = filter.NewResolver(taskSvc, collectNonTerminalStatuses(workflowSvc))
```

After:
```go
a.resolver = filter.NewResolver(taskSvc, projectSvc, collectNonTerminalStatuses(workflowSvc))
```

`projectSvc` must be in scope at this point. Check a few lines up in the `NewApp` constructor (or wherever `app.go:90` lives) — if the project service is not already a field on the app, thread it through the constructor. The signature shape for `ProjectService.GetByName` already matches `filter.ProjectLookup`, so it satisfies the interface directly without a wrapper.

- [ ] **Step 4: Update `internal/mcp/server.go`**

At `internal/mcp/server.go:780`:

Before:
```go
return filter.NewResolver(s.taskSvc, defaults)
```

After:
```go
return filter.NewResolver(s.taskSvc, s.projectSvc, defaults)
```

If `s.projectSvc` does not exist on the MCP `Server` struct, add it alongside `s.taskSvc` and populate it in the server constructor. Check `internal/mcp/server.go` for where the MCP server is wired and pass the existing `*service.ProjectService` through.

- [ ] **Step 5: Run filter and internal tests**

Run: `go test ./filter/... ./internal/tui/... ./internal/mcp/...`
Expected: PASS.

- [ ] **Step 6: Stage**

```bash
git add filter/resolve.go filter/resolve_test.go filter/integration_test.go internal/tui/app.go internal/mcp/server.go
```

Still no commit — hold until Task 5 finishes.

---

## Task 5: Update `internal/tui` Task Rendering

Anywhere the TUI displays a task's project, it needs to print the human name, not the UUID. The typed `Task.ProjectID` is a UUID, so the renderer must look up the name via `ProjectService.GetByID` at render time. For list views, cache the lookup per invocation so N tasks sharing a project hit the DB once.

**Files:**
- Modify: `internal/tui/*` — every place a task's project is rendered or printed
- Modify: `tests/e2e/*` — if any e2e scenario greps for the literal string `"default"` in JSON output, the output shape needs to be confirmed

- [ ] **Step 1: Find every render and write site**

Run: `grep -rn 'task\.ProjectID\|ProjectID' internal/tui/ internal/mcp/`

Expected touchpoints (line numbers are approximate and may drift during edits):

| File:line | Current | What to do |
|-----------|---------|------------|
| `internal/tui/commands.go:182` | `task.ProjectID = f.Value` (string from inline field) | Resolve the value via `taskSvc.ResolveProjectName(ctx, f.Value)` and stamp the returned UUID onto `task.ProjectID`. |
| `internal/tui/render.go:477-478` | `if task.ProjectID != "" { ... %s, task.ProjectID }` | Replace the zero-check with `task.ProjectID != uuid.Nil`, and print `cache.name(task.ProjectID)` from the name-cache helper below. |
| `internal/mcp/tools.go:137` | `task.ProjectID = projectID` (where `projectID` is the user-supplied string name from the MCP tool input) | Same pattern as the TUI commands case — call `taskSvc.ResolveProjectName(ctx, projectID)` and stamp the UUID. |
| `internal/mcp/tools.go:722` | `r.ProjectID = task.ProjectID` (copying into an MCP response struct field, currently `string`) | Change the MCP response struct's `ProjectID` field to `string` rendered from the project's name (call `projectSvc.GetByID(ctx, task.ProjectID).Name` once) — MCP clients expect human-readable values. Keep the JSON field name unchanged to preserve the MCP tool's output contract. |

Classify any additional hits:
- **Display site** (format string, JSON output struct field, `fmt.Sprintf("project: %s", task.ProjectID)`): convert via the per-invocation name cache before display.
- **Filter/query site** (passing a project to a filter or service method): already handled by Task 4.
- **Test fixture**: update to use `domain.DefaultProjectUUID` or a stable test UUID.

- [ ] **Step 2: Add `ProjectService.GetByID` and a per-invocation name cache**

First add the service helper. Edit `service/project.go`:

```go
// GetByID retrieves a project by its typed UUID.
// Returns domain.ErrNotFound if not found.
func (s *ProjectService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	return s.projectRepo.GetByID(ctx, id)
}
```

Ensure `service/project.go` imports `github.com/google/uuid`.

Then introduce a per-invocation project-name cache local to the TUI render layer. Place it next to the list-rendering function that needs it — for the current layout this is likely `internal/tui/task.go` or `internal/tui/list.go`. If unclear, run `grep -rn 'task.*List\|renderTask' internal/tui/` and pick the file that owns the list rendering.

```go
import (
	"context"

	"github.com/germanamz/tusk/service"
	"github.com/google/uuid"
)

// projectNameCache resolves project UUIDs to names for the duration of one
// rendering pass. It avoids N+1 lookups when listing many tasks in the same
// project. On lookup failure it falls back to the stringified UUID rather
// than failing the render.
type projectNameCache struct {
	svc   *service.ProjectService
	ctx   context.Context
	cache map[uuid.UUID]string
}

func newProjectNameCache(ctx context.Context, svc *service.ProjectService) *projectNameCache {
	return &projectNameCache{svc: svc, ctx: ctx, cache: make(map[uuid.UUID]string)}
}

func (c *projectNameCache) name(id uuid.UUID) string {
	if n, ok := c.cache[id]; ok {
		return n
	}
	proj, err := c.svc.GetByID(c.ctx, id)
	if err != nil {
		c.cache[id] = id.String()
		return id.String()
	}
	c.cache[id] = proj.Name
	return proj.Name
}
```

- [ ] **Step 3: Rewrite each render site**

For every display site identified in Step 1, replace raw `task.ProjectID` formatting with a `cache.name(task.ProjectID)` call. If the renderer produces JSON output, decide whether the JSON field should be the name (human-readable) or an object `{id, name}` — recommend keeping the existing field shape but with the resolved name as the value, so scripts that parsed the old `"project_id": "default"` shape keep working.

- [ ] **Step 4: Update CLI command wiring**

Commands that construct a `*domain.Task` from flags/inline syntax (look in `internal/tui/add.go` or wherever `tusk task create` is implemented) receive a `project` string from the parser. Replace the current direct assignment `task.ProjectID = projectStr` with:

```go
projID, err := taskSvc.ResolveProjectName(ctx, projectStr)
if err != nil {
    return fmt.Errorf("resolving project: %w", err)
}
task.ProjectID = projID
```

Same pattern for `tusk task modify` when the `project=` inline field is supplied: resolve the name, then stamp onto the `**uuid.UUID` in `TaskUpdate`.

- [ ] **Step 5: Run the full build and test suite**

Run: `make build`
Expected: clean.

Run: `make test`
Expected: PASS.

Run: `make test-race`
Expected: PASS.

If any e2e scenario in `tests/e2e/` asserts on the literal string `"default"` appearing in output, update it to `"_default"` (or the resolved name, depending on the scenario intent). Run:

```bash
grep -rn '"default"' tests/e2e/
```

and update each hit that refers to the default project. Do not change hits that refer to unrelated literals.

- [ ] **Step 6: Commit Tasks 2-5 as a single changeset**

```bash
git add domain/task.go domain/filter.go sqlite/task.go sqlite/task_test.go \
        service/task.go service/project.go service/task_test.go \
        service/task_claim_test.go service/task_routing_test.go service/bundle_helpers_test.go \
        filter/resolve.go filter/resolve_test.go filter/integration_test.go \
        internal/tui internal/mcp tests/e2e
git commit -m "feat(task): type Task.ProjectID as uuid.UUID and resolve names end-to-end"
```

Add or remove paths from the `git add` line to match the set of files actually modified.

---

## Task 6: Migration `005_tasks_project_fk` — Rebuild `tasks` with FK

Rebuild the `tasks` table via the classic SQLite table-swap pattern to add a real FK on `project_id`. Per user direction, no data-preservation logic is needed — any pre-existing `project_id` value that is not already a UUID gets collapsed to `domain.DefaultProjectUUID`.

**Files:**
- Create: `migrations/005_tasks_project_fk.up.sql`
- Create: `migrations/005_tasks_project_fk.down.sql`

- [ ] **Step 1: Write the up-migration**

Create `migrations/005_tasks_project_fk.up.sql`. Note the migration runner strips `PRAGMA` lines (see `sqlite/store.go` `stripPragmas`) — the `foreign_keys=OFF/ON` bracketing must therefore be done at the session level, not in the migration file. SQLite's table-rebuild guidance is to wrap the operation in a transaction and rely on deferred FK checks; since the migration runner already uses `BEGIN`/`COMMIT`, the temporary-disable is unnecessary as long as the new table's FK references a valid row. Every task row is collapsed to `DefaultProjectUUID`, which was seeded in migration 004, so the FK is always satisfied at insert time.

```sql
CREATE TABLE tasks_new (
    id TEXT PRIMARY KEY,
    short_id TEXT NOT NULL UNIQUE,
    parent_id TEXT REFERENCES tasks_new(id) ON DELETE SET NULL,
    project_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000'
        REFERENCES projects(id) ON DELETE RESTRICT,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    due_at TEXT,
    wait_until TEXT,
    recurrence_rule TEXT,
    uda TEXT DEFAULT '{}',
    created_at TEXT NOT NULL,
    modified_at TEXT NOT NULL,
    claimed_by TEXT REFERENCES players(id),
    claimed_at TEXT
);

INSERT INTO tasks_new
SELECT id, short_id, parent_id,
       '00000000-0000-0000-0000-000000000000',
       title, description, status, priority, version, due_at, wait_until,
       recurrence_rule, uda, created_at, modified_at, claimed_by, claimed_at
FROM tasks;

DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;

CREATE INDEX idx_tasks_short_id    ON tasks(short_id);
CREATE INDEX idx_tasks_parent_id   ON tasks(parent_id);
CREATE INDEX idx_tasks_project_id  ON tasks(project_id);
CREATE INDEX idx_tasks_status      ON tasks(status);
CREATE INDEX idx_tasks_due_at      ON tasks(due_at);
CREATE INDEX idx_tasks_wait_until  ON tasks(wait_until);
CREATE INDEX idx_tasks_claimed_by  ON tasks(claimed_by);
```

- [ ] **Step 2: Write the down-migration**

Create `migrations/005_tasks_project_fk.down.sql`:

```sql
CREATE TABLE tasks_old (
    id TEXT PRIMARY KEY,
    short_id TEXT NOT NULL UNIQUE,
    parent_id TEXT REFERENCES tasks_old(id) ON DELETE SET NULL,
    project_id TEXT NOT NULL DEFAULT 'default',
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    due_at TEXT,
    wait_until TEXT,
    recurrence_rule TEXT,
    uda TEXT DEFAULT '{}',
    created_at TEXT NOT NULL,
    modified_at TEXT NOT NULL,
    claimed_by TEXT REFERENCES players(id),
    claimed_at TEXT
);

INSERT INTO tasks_old SELECT
    id, short_id, parent_id, project_id, title, description, status, priority,
    version, due_at, wait_until, recurrence_rule, uda, created_at, modified_at,
    claimed_by, claimed_at FROM tasks;

DROP TABLE tasks;
ALTER TABLE tasks_old RENAME TO tasks;

CREATE INDEX idx_tasks_short_id   ON tasks(short_id);
CREATE INDEX idx_tasks_parent_id  ON tasks(parent_id);
CREATE INDEX idx_tasks_project_id ON tasks(project_id);
CREATE INDEX idx_tasks_status     ON tasks(status);
CREATE INDEX idx_tasks_due_at     ON tasks(due_at);
CREATE INDEX idx_tasks_wait_until ON tasks(wait_until);
CREATE INDEX idx_tasks_claimed_by ON tasks(claimed_by);
```

- [ ] **Step 3: Add a migration verification test**

Create `sqlite/task_fk_test.go`:

```go
package sqlite_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

func TestMigration005_TasksHaveFKToProjects(t *testing.T) {
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	defer store.Close()

	// Inserting a task bound to the seeded _default project must succeed.
	tr := sqlite.NewTaskRepo(store.DB())
	now := time.Now().UTC().Truncate(time.Millisecond)
	ok := &domain.Task{
		ID:         uuid.New(),
		ShortID:    "abcd1234",
		Title:      "fk-ok",
		ProjectID:  domain.DefaultProjectUUID,
		Status:     "pending",
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
	}
	if err := tr.Create(context.Background(), ok); err != nil {
		t.Fatalf("insert with seeded project: %v", err)
	}

	// Inserting a task bound to an unknown project UUID must fail the FK.
	bad := &domain.Task{
		ID:         uuid.New(),
		ShortID:    "abcd5678",
		Title:      "fk-bad",
		ProjectID:  uuid.New(), // not in projects table
		Status:     "pending",
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
	}
	err = tr.Create(context.Background(), bad)
	if err == nil {
		t.Fatalf("expected FK violation, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("expected foreign-key error, got: %v", err)
	}
}
```

- [ ] **Step 4: Run the verification test**

Run: `go test ./sqlite/... -run TestMigration005_TasksHaveFKToProjects -v`
Expected: PASS.

- [ ] **Step 5: Run the full test suite**

Run: `make test`
Expected: PASS.

Run: `make test-race`
Expected: PASS.

Run: `make vet`
Expected: clean.

Run: `make lint`
Expected: clean.

- [ ] **Step 6: Manual smoke-test (non-gated)**

Build the binary and run a minimal end-to-end sequence:

```bash
make build
rm -f /tmp/tusk-phase3-smoke.db
./bin/tusk --db /tmp/tusk-phase3-smoke.db task create "hello world"
./bin/tusk --db /tmp/tusk-phase3-smoke.db task list
./bin/tusk --db /tmp/tusk-phase3-smoke.db task list --output json
```

Expected: task is created and listed; the JSON output shows the project name (`_default`) or the UUID, consistent with Task 5's rendering choices.

- [ ] **Step 7: Commit**

```bash
git add migrations/005_tasks_project_fk.up.sql migrations/005_tasks_project_fk.down.sql sqlite/task_fk_test.go
git commit -m "feat(sqlite): add tasks.project_id FK and rebuild tasks table"
```

---

## Acceptance Criteria — User-Visible Behavior Still Works

At the end of this phase, every one of these must still hold:

- `make build`, `make test`, `make test-race`, `make vet`, `make lint`: clean.
- `tusk task create "x"` creates a task bound to the `_default` project (seeded UUID all zeros).
- `tusk task create "x" project=backend` creates a task bound to the backend project, after the user has defined backend via config. The name is resolved to a UUID; the task row stores the UUID.
- `tusk task list` prints task rows with the project column showing human names, not UUIDs.
- `tusk task list project=backend` filters correctly — the filter resolver translates `backend` to its UUID before hitting the DB.
- `tusk task list --output json` emits the same top-level field shape it did before this phase (the project field value may now be a name rather than a string-as-ID, but the JSON key is unchanged).
- `tusk project list` / `tusk workflow list` still work through the config-driven `inmem` repositories; their output is unchanged.
- E2E scenarios that construct tasks, filter by project, and modify tasks all pass.
- Attempting to insert a task with a project UUID that does not exist in `projects` surfaces a SQLite foreign-key error at the repository layer.

## Changes Introduced

**New files:**
- `migrations/005_tasks_project_fk.up.sql`
- `migrations/005_tasks_project_fk.down.sql`
- `sqlite/task_fk_test.go`

**Modified interfaces / types:**
- `domain.Task.ProjectID` retyped from `string` to `uuid.UUID`.
- `domain.TaskFilter.ProjectID` retyped from `*string` to `*uuid.UUID`.
- `domain.DefaultProjectUUID` (`var` = `uuid.Nil`) added to `domain/task.go`.
- `service.TaskUpdate.ProjectID` retyped from `**string` to `**uuid.UUID`.
- `service.DefaultProjectID` (string `"default"`) removed; `service.DefaultProjectName` (`"_default"`) takes its place.
- `service.TaskService.ResolveProjectName(ctx, name) (uuid.UUID, error)` added as a public helper for CLI/MCP callers.
- `service.TaskService.defaultProjectID(ctx)` added as an internal helper.
- `service.ProjectService.GetByID(ctx, uuid.UUID)` added.
- `repository.ProjectRepository.GetByID(ctx, uuid.UUID)` added to the interface.
- `inmem.ProjectRepository.GetByID(uuid.UUID)` added.
- `filter.Resolver` gains a `projectLookup ProjectLookup` field; `filter.NewResolver` signature changes to accept a `ProjectLookup` as the second argument.

**Schema migrations:**
- `005_tasks_project_fk` — rebuilds `tasks` via table-swap to add FK `project_id → projects(id) ON DELETE RESTRICT`, and collapses every existing `tasks.project_id` value to `00000000-0000-0000-0000-000000000000`.

**Dependencies:**
- None.

**Bridge code:**
- `domain.Project.Workflow string` (inherited from Phase 2) is still present after this phase. Its removal target is **Phase 4** of this plan.

**User-visible behavior that carries forward unchanged:**
- CLI syntax for creating, filtering, modifying, and viewing tasks is identical.
- Human-name semantics on every user-facing surface (list output, JSON output, filter input, inline `project=` values).
- Config-driven project and workflow definitions are still the authoritative source for `ProjectService` and `WorkflowService`.
