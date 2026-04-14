# Phase 2 — Projects Schema & SQLite Repo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `projects` table with FK to `workflows`, extend `domain.Project` with persistent-entity fields, rename `repository.ProjectRepository.GetByID` to `GetByName` (the current string-keyed lookup is really a name lookup), and ship a full-CRUD `sqlite.ProjectRepo` with optimistic locking and a `CountByWorkflow` helper.

**Architecture:** The rename of `GetByID`→`GetByName` is mechanical but ripples through `service/task.go` (the largest user of the project repo). The `inmem.ProjectRepository` stays wired into `ProjectService` as the live backing store; this phase only adds the SQLite substrate behind it. Migration `004_projects.up.sql` creates the `projects` table with `workflow_id` FK to `workflows(id)` and seeds the builtin `_default` project (UUID all zeros) pointing at the kanban workflow (also UUID all zeros).

**Tech Stack:** Go, SQLite (via `modernc.org/sqlite`), `github.com/google/uuid`, embedded SQL migrations.

## Inherits From

After Phase 1, the codebase state the implementer can rely on:

- `domain.Workflow` already carries `ID`, `Version`, `CreatedAt`, `UpdatedAt` fields. `inmem.WorkflowRepository` synthesizes these from config.
- `migrations/003_workflows.up.sql` exists; a fresh DB always has a `workflows` table containing the seeded `kanban` row at UUID `00000000-0000-0000-0000-000000000000`.
- `sqlite.WorkflowRepo` exists with full CRUD (`Create`/`GetByID`/`GetByName`/`List`/`Update`/`Delete`) but is **not** wired into any service — `WorkflowService` still uses `inmem.WorkflowRepository`.
- `repository.WorkflowRepository` interface is unchanged (read-only: `GetByName`, `List`).
- `repository.ProjectRepository` interface is still read-only with `GetByID(ctx, string)` and `List(ctx)`. This phase renames `GetByID` → `GetByName`.

## Prerequisites

- Phase 1 must be merged. The `workflows` table and its seeded kanban row must exist before this phase's migration runs, because `projects.workflow_id` has `REFERENCES workflows(id) ON DELETE RESTRICT`.

---

## Task 1: Extend `domain.Project` with Persistent-Entity Fields

Pattern-match on Phase 1's extension of `domain.Workflow`. `domain.Project` currently has `ID string`, `Workflow string`, `Settings ProjectSettings`. Promote `ID` to `uuid.UUID`, add `Name string` (holds what `ID` used to hold), add `WorkflowID uuid.UUID` (new), keep `Workflow string` for now as a compatibility surface for in-memory consumers, and add `Version`, `CreatedAt`, `UpdatedAt`.

**Files:**
- Modify: `domain/project.go`
- Modify: `inmem/project.go` — builder and `copyProject`
- Modify: `inmem/project_test.go` — any test that constructs `domain.Project` literals

- [ ] **Step 1: Extend the domain type**

Replace the `Project` struct in `domain/project.go` with:

```go
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Project is a persisted container for tasks. Each project binds to a workflow
// and carries per-project settings (automation + urgency overrides).
type Project struct {
	ID         uuid.UUID
	Name       string
	WorkflowID uuid.UUID
	Workflow   string // Name of the bound workflow — retained for service-layer ergonomics.
	Settings   ProjectSettings
	Version    int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
```

Note: `Workflow string` is kept alongside `WorkflowID uuid.UUID` so service code that currently passes `project.Workflow` to `WorkflowService.GetByName` keeps compiling. It becomes redundant once the next initiative switches `WorkflowService` over to DB lookups, at which point it should be removed — but that cleanup is out of scope here.

- [ ] **Step 2: Update `inmem.ProjectRepository` builder**

Replace the body of `buildProjectMap` in `inmem/project.go` with:

```go
func buildProjectMap(cfgProjects map[string]config.ProjectConfig) map[string]*domain.Project {
	now := time.Now().UTC().Truncate(time.Millisecond)
	projects := make(map[string]*domain.Project, len(cfgProjects))
	for name, cfg := range cfgProjects {
		p := &domain.Project{
			ID:         uuid.NewSHA1(uuid.Nil, []byte("project:"+name)),
			Name:       name,
			WorkflowID: uuid.NewSHA1(uuid.Nil, []byte("workflow:"+cfg.Workflow)),
			Workflow:   cfg.Workflow,
			Version:    1,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if cfg.Settings.AutoCompleteParent != nil {
			p.Settings.AutoCompleteParent = &domain.AutoCompleteConfig{
				TriggerStatus: cfg.Settings.AutoCompleteParent.TriggerStatus,
				TargetStatus:  cfg.Settings.AutoCompleteParent.TargetStatus,
			}
		}
		if cfg.Settings.AutoRevertParent != nil {
			p.Settings.AutoRevertParent = &domain.AutoRevertConfig{
				TriggerStatus: cfg.Settings.AutoRevertParent.TriggerStatus,
				TargetStatus:  cfg.Settings.AutoRevertParent.TargetStatus,
			}
		}
		if cfg.Settings.Urgency != nil {
			p.Settings.Urgency = &domain.UrgencyOverrides{
				PriorityWeight:    cfg.Settings.Urgency.PriorityWeight,
				DueWeight:         cfg.Settings.Urgency.DueWeight,
				AgeWeight:         cfg.Settings.Urgency.AgeWeight,
				ActiveWeight:      cfg.Settings.Urgency.ActiveWeight,
				BlockingWeight:    cfg.Settings.Urgency.BlockingWeight,
				BlockedWeight:     cfg.Settings.Urgency.BlockedWeight,
				TagsWeight:        cfg.Settings.Urgency.TagsWeight,
				ProjectWeight:     cfg.Settings.Urgency.ProjectWeight,
				AnnotationsWeight: cfg.Settings.Urgency.AnnotationsWeight,
				WaitingWeight:     cfg.Settings.Urgency.WaitingWeight,
			}
		}
		projects[name] = p
	}
	return projects
}
```

Add the `time` and `uuid` imports at the top of `inmem/project.go`:

```go
import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)
```

Update `copyProject` to copy the new scalar fields:

```go
func copyProject(p *domain.Project) *domain.Project {
	cp := &domain.Project{
		ID:         p.ID,
		Name:       p.Name,
		WorkflowID: p.WorkflowID,
		Workflow:   p.Workflow,
		Version:    p.Version,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
	}
	if p.Settings.AutoCompleteParent != nil {
		acc := *p.Settings.AutoCompleteParent
		cp.Settings.AutoCompleteParent = &acc
	}
	if p.Settings.AutoRevertParent != nil {
		arc := *p.Settings.AutoRevertParent
		cp.Settings.AutoRevertParent = &arc
	}
	if p.Settings.Urgency != nil {
		uo := *p.Settings.Urgency
		cp.Settings.Urgency = &uo
	}
	return cp
}
```

Note: the map is now keyed by `name`, not `id`. The existing `GetByID(ctx, id string)` in `inmem/project.go` walks this map — Task 2 renames it to `GetByName` so the semantics are correct.

- [ ] **Step 3: Fix `service/workflow.go:GetWorkflowWithProjects`**

This function returns a `[]string` list of project identifiers that reference a workflow. It currently appends `p.ID` (which was the human name string) and sorts the result. After the domain retype, `p.ID` is `uuid.UUID` — incompatible with `[]string`. The semantically correct field is now `p.Name`, which holds the same human string the old `p.ID` did.

Edit `service/workflow.go`:

Before (around line 110-117):
```go
var projectIDs []string
for _, p := range projects {
    if p.Workflow == name {
        projectIDs = append(projectIDs, p.ID)
    }
}
sort.Strings(projectIDs)
return wf, projectIDs, nil
```

After:
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

The `p.Workflow == name` filter stays — that field is still populated in this phase via the compat surface on `domain.Project`. Phase 4 will replace it with a `WorkflowID` comparison.

- [ ] **Step 4: Compile and run tests**

Run: `make build`
Expected: clean. If a compile error surfaces on `domain.Project{ID: "name"}`-style literals in other files, fix them:

Run: `grep -rn "domain.Project{" --include='*.go' .`
Fix each call site to use named fields (`Name:` for the string, `ID:` for the UUID). Many test fixtures will need the `Name` field added; the `ID` field can be omitted and will default to `uuid.Nil`.

Run: `go test ./domain/... ./inmem/... ./service/...`
Expected: PASS. `service/workflow_test.go` uses `GetWorkflowWithProjects` — confirm the assertions there still pass (they compare human names as strings, which is the new behavior).

- [ ] **Step 5: Commit**

```bash
git add domain/project.go inmem/project.go inmem/project_test.go service/workflow.go
# Add any other files touched by the grep in Step 4.
git commit -m "feat(domain): add id/version/timestamps to Project entity"
```

---

## Task 2: Rename `ProjectRepository.GetByID` → `GetByName`

The existing string-keyed lookup is semantically a name lookup — the current `ID` field is a human name, not an opaque identifier. Rename the interface method, update the `inmem` implementation, and update every caller. This is a mechanical rename across `service/task.go` (~7 call sites) and any helpers.

**Files:**
- Modify: `repository/project.go` — rename method in interface
- Modify: `inmem/project.go` — rename `GetByID` to `GetByName`
- Modify: `service/project.go` — rename `GetByID` wrapper if present
- Modify: `service/task.go` — every call site
- Modify: any other caller: run the grep below and update each hit

- [ ] **Step 1: Rename in the interface**

Edit `repository/project.go`. Replace the file contents with:

```go
package repository

import (
	"context"

	"github.com/germanamz/tusk/domain"
)

// ProjectRepository provides read access to projects.
// Write operations (Create/Update/Delete) are exposed as concrete methods
// on sqlite.ProjectRepo in Phase 2 and are not yet part of this interface;
// they will be promoted to the interface in the v0.11 Service Layer Migration.
type ProjectRepository interface {
	// GetByName returns a project by its human-readable name (e.g. "default", "backend").
	// Returns domain.ErrNotFound if the project doesn't exist.
	GetByName(ctx context.Context, name string) (*domain.Project, error)

	// List returns all projects, sorted by name.
	List(ctx context.Context) ([]*domain.Project, error)
}
```

- [ ] **Step 2: Rename in `inmem`**

Edit `inmem/project.go`. Rename the existing `GetByID` method to `GetByName`:

```go
// GetByName returns a defensive copy of the project. Returns domain.ErrNotFound if not found.
func (r *ProjectRepository) GetByName(_ context.Context, name string) (*domain.Project, error) {
	r.mu.RLock()
	p, ok := r.projects[name]
	r.mu.RUnlock()
	if !ok {
		return nil, domain.ErrNotFound
	}
	return copyProject(p), nil
}
```

- [ ] **Step 3: Rename in `service.ProjectService`**

Edit `service/project.go`. Replace the `GetByID` method with:

```go
// GetByName retrieves a project by its human-readable name.
// Returns domain.ErrNotFound if not found.
func (s *ProjectService) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	return s.projectRepo.GetByName(ctx, name)
}
```

- [ ] **Step 4: Rename every caller**

Run: `grep -rn 'projectRepo\.GetByID' --include='*.go' .`
Expected hits: `service/task.go` and possibly a few other services/tests.

For every hit, rename `GetByID` to `GetByName`. The arguments do not change — they were always human names. Example from `service/task.go:96`:

Before:
```go
project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
```

After:
```go
project, err := s.projectRepo.GetByName(ctx, task.ProjectID)
```

Also run: `grep -rn 'projectSvc\.GetByID\|ProjectService.*GetByID' --include='*.go' .`
Rename each to `GetByName`.

Also run: `grep -rn 'ProjectRepository.*GetByID' --include='*.go' .`
Update any compile-time interface assertions or docstrings.

- [ ] **Step 5: Compile and run unit tests**

Run: `make build`
Expected: clean. If a compile error mentions `GetByID`, grep again and fix the stragglers.

Run: `go test ./repository/... ./inmem/... ./service/...`
Expected: PASS. Any test using `projectRepo.GetByID(...)` must be renamed to `GetByName(...)`.

- [ ] **Step 6: Commit**

```bash
git add repository/project.go inmem/project.go service/project.go service/task.go
# Add any other files the grep surfaced.
git commit -m "refactor(repo): rename ProjectRepository.GetByID to GetByName"
```

---

## Task 3: Migration `004_projects` — Table + `_default` Seed

Create the SQL schema for projects and seed `_default` as a regular row bound to the kanban workflow from Phase 1.

**Files:**
- Create: `migrations/004_projects.up.sql`
- Create: `migrations/004_projects.down.sql`
- Create: `sqlite/project_seed_test.go`

- [ ] **Step 1: Write the up-migration**

Create `migrations/004_projects.up.sql`:

```sql
CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE RESTRICT,
    settings TEXT NOT NULL DEFAULT '{}',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_projects_name ON projects(name);
CREATE INDEX idx_projects_workflow_id ON projects(workflow_id);

INSERT INTO projects (id, name, workflow_id) VALUES (
    '00000000-0000-0000-0000-000000000000',
    '_default',
    '00000000-0000-0000-0000-000000000000'
);
```

- [ ] **Step 2: Write the down-migration**

Create `migrations/004_projects.down.sql`:

```sql
DROP INDEX IF EXISTS idx_projects_workflow_id;
DROP INDEX IF EXISTS idx_projects_name;
DROP TABLE IF EXISTS projects;
```

- [ ] **Step 3: Write a seed-row test**

Create `sqlite/project_seed_test.go`:

```go
package sqlite_test

import (
	"testing"

	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
)

func TestMigration004_SeedsDefaultProject(t *testing.T) {
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	defer store.Close()

	var name, workflowID string
	err = store.DB().QueryRow(
		`SELECT name, workflow_id FROM projects WHERE id = '00000000-0000-0000-0000-000000000000'`,
	).Scan(&name, &workflowID)
	if err != nil {
		t.Fatalf("querying seed row: %v", err)
	}
	if name != "_default" {
		t.Errorf("got name %q, want %q", name, "_default")
	}
	if workflowID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("got workflow_id %q, want kanban UUID", workflowID)
	}
}
```

- [ ] **Step 4: Run the seed test**

Run: `go test ./sqlite/... -run TestMigration004_SeedsDefaultProject -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add migrations/004_projects.up.sql migrations/004_projects.down.sql sqlite/project_seed_test.go
git commit -m "feat(sqlite): add projects table and seed _default project"
```

---

## Task 4: `sqlite.ProjectRepo` Read Operations (`Create`, `GetByID`, `GetByName`, `List`)

Follow the `sqlite.WorkflowRepo` conventions from Phase 1. `GetByID` (new — UUID-keyed) is a concrete method on the repo, not part of the interface. Phase 3 promotes it to the interface once `Task.ProjectID` is UUID-typed.

**Files:**
- Create: `sqlite/project.go`
- Create: `sqlite/project_test.go`

- [ ] **Step 1: Write the failing tests**

Create `sqlite/project_test.go`:

```go
package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

var defaultUUID = uuid.MustParse("00000000-0000-0000-0000-000000000000")

func newTestProjectRepo(t *testing.T) *sqlite.ProjectRepo {
	t.Helper()
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return sqlite.NewProjectRepo(store.DB())
}

func sampleProject(name string) *domain.Project {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return &domain.Project{
		ID:         uuid.New(),
		Name:       name,
		WorkflowID: defaultUUID, // bind to seeded kanban
		Workflow:   "kanban",
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestProjectRepo_CreateAndGetByID(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()

	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "backend" {
		t.Errorf("got name %q, want %q", got.Name, "backend")
	}
	if got.WorkflowID != defaultUUID {
		t.Errorf("got workflow_id %v, want kanban UUID", got.WorkflowID)
	}
}

func TestProjectRepo_GetByName_Seed(t *testing.T) {
	repo := newTestProjectRepo(t)
	got, err := repo.GetByName(context.Background(), "_default")
	if err != nil {
		t.Fatalf("GetByName _default: %v", err)
	}
	if got.ID != defaultUUID {
		t.Errorf("got ID %v, want defaultUUID", got.ID)
	}
}

func TestProjectRepo_GetByID_NotFound(t *testing.T) {
	repo := newTestProjectRepo(t)
	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByID: got %v, want ErrNotFound", err)
	}
}

func TestProjectRepo_List_ContainsSeed(t *testing.T) {
	repo := newTestProjectRepo(t)
	ps, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ps) < 1 {
		t.Fatalf("want >= 1 project, got %d", len(ps))
	}
	found := false
	for _, p := range ps {
		if p.Name == "_default" {
			found = true
		}
	}
	if !found {
		t.Errorf("_default seed not in list")
	}
}

func TestProjectRepo_Create_DuplicateName(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()
	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	p2 := sampleProject("backend")
	err := repo.Create(ctx, p2)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("dup Create: got %v, want ErrConflict", err)
	}
}

func TestProjectRepo_Create_UnknownWorkflow(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()
	p := sampleProject("backend")
	p.WorkflowID = uuid.New() // not in workflows table
	err := repo.Create(ctx, p)
	if err == nil {
		t.Fatalf("expected FK violation, got nil")
	}
	// Not required to be a sentinel — FK violations surface as raw SQLite errors.
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./sqlite/... -run TestProjectRepo -v`
Expected: FAIL with "undefined: sqlite.ProjectRepo".

- [ ] **Step 3: Implement `sqlite.ProjectRepo` read path**

Create `sqlite/project.go`:

```go
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

const projectColumns = `id, name, workflow_id, settings, version, created_at, updated_at`

// ProjectRepo implements project persistence using SQLite.
type ProjectRepo struct {
	db DBTX
}

// NewProjectRepo creates a ProjectRepo.
func NewProjectRepo(db DBTX) *ProjectRepo {
	return &ProjectRepo{db: db}
}

// Create inserts a new project. Returns domain.ErrConflict on unique-name collision.
// FK violations on workflow_id surface as the raw SQLite error.
func (r *ProjectRepo) Create(ctx context.Context, p *domain.Project) error {
	settingsJSON, err := json.Marshal(p.Settings)
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	_, err = r.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO projects (%s) VALUES (?, ?, ?, ?, ?, ?, ?)`, projectColumns),
		p.ID.String(), p.Name, p.WorkflowID.String(), string(settingsJSON), p.Version,
		p.CreatedAt.UTC().Format(timeFormat),
		p.UpdatedAt.UTC().Format(timeFormat),
	)
	if err != nil {
		if _, lookupErr := r.GetByName(ctx, p.Name); lookupErr == nil {
			return domain.ErrConflict
		}
		return err
	}
	return nil
}

// GetByID retrieves a project by UUID. Returns domain.ErrNotFound if missing.
func (r *ProjectRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM projects WHERE id = ?`, projectColumns),
		id.String())
	return scanProject(row)
}

// GetByName retrieves a project by name. Returns domain.ErrNotFound if missing.
func (r *ProjectRepo) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM projects WHERE name = ?`, projectColumns),
		name)
	return scanProject(row)
}

// List returns all projects ordered by name.
func (r *ProjectRepo) List(ctx context.Context) ([]*domain.Project, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM projects ORDER BY name`, projectColumns))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*domain.Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

type projectScanner interface {
	Scan(dest ...any) error
}

func scanProject(s projectScanner) (*domain.Project, error) {
	var (
		p            domain.Project
		idStr        string
		workflowStr  string
		settingsJSON string
		createdAt    string
		updatedAt    string
	)
	err := s.Scan(&idStr, &p.Name, &workflowStr, &settingsJSON, &p.Version, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	p.ID, err = uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parsing project id: %w", err)
	}
	p.WorkflowID, err = uuid.Parse(workflowStr)
	if err != nil {
		return nil, fmt.Errorf("parsing workflow_id: %w", err)
	}
	if err := json.Unmarshal([]byte(settingsJSON), &p.Settings); err != nil {
		return nil, fmt.Errorf("decoding settings: %w", err)
	}
	p.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, err
	}
	p.UpdatedAt, err = time.Parse(timeFormat, updatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
```

Note: the `scanProject` method intentionally leaves `p.Workflow` (the string name) empty — SQLite-backed rows carry only `WorkflowID`. Consumers that need the workflow name join through `workflows.name`; no current service does this inside the SQLite path in this phase.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./sqlite/... -run TestProjectRepo -v`
Expected: PASS on all read tests (`CreateAndGetByID`, `GetByName_Seed`, `GetByID_NotFound`, `List_ContainsSeed`, `Create_DuplicateName`, `Create_UnknownWorkflow`).

- [ ] **Step 5: Commit**

```bash
git add sqlite/project.go sqlite/project_test.go
git commit -m "feat(sqlite): implement ProjectRepo read operations"
```

---

## Task 5: `sqlite.ProjectRepo.Update` and `CountByWorkflow`

Version-checked update and a helper that counts projects referencing a given workflow. `CountByWorkflow` powers the workflow delete guard in the next initiative — it is added here so the SQLite substrate is complete.

**Files:**
- Modify: `sqlite/project.go` — add `Update` and `CountByWorkflow`
- Modify: `sqlite/project_test.go` — add tests

- [ ] **Step 1: Write the failing tests**

Append to `sqlite/project_test.go`:

```go
func TestProjectRepo_Update_IncrementsVersion(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()
	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	priority := 15.0
	p.Settings.Urgency = &domain.UrgencyOverrides{BlockingWeight: &priority}
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p.Version != 2 {
		t.Errorf("local version: got %d, want 2", p.Version)
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("stored version: got %d, want 2", got.Version)
	}
	if got.Settings.Urgency == nil || got.Settings.Urgency.BlockingWeight == nil {
		t.Errorf("urgency override lost round-trip")
	}
}

func TestProjectRepo_Update_StaleVersion(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()
	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	stale := *p
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	err := repo.Update(ctx, &stale)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale Update: got %v, want ErrConflict", err)
	}
}

func TestProjectRepo_CountByWorkflow(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()

	n, err := repo.CountByWorkflow(ctx, defaultUUID)
	if err != nil {
		t.Fatalf("CountByWorkflow seed: %v", err)
	}
	if n != 1 {
		t.Errorf("seed count: got %d, want 1 (the _default project)", n)
	}

	for _, name := range []string{"backend", "frontend"} {
		p := sampleProject(name)
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}

	n, err = repo.CountByWorkflow(ctx, defaultUUID)
	if err != nil {
		t.Fatalf("CountByWorkflow after inserts: %v", err)
	}
	if n != 3 {
		t.Errorf("count after inserts: got %d, want 3", n)
	}

	n, err = repo.CountByWorkflow(ctx, uuid.New())
	if err != nil {
		t.Fatalf("CountByWorkflow unknown workflow: %v", err)
	}
	if n != 0 {
		t.Errorf("unknown workflow count: got %d, want 0", n)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./sqlite/... -run 'TestProjectRepo_Update|TestProjectRepo_CountByWorkflow' -v`
Expected: FAIL with "undefined" errors for `Update` and `CountByWorkflow`.

- [ ] **Step 3: Implement `Update` and `CountByWorkflow`**

Append to `sqlite/project.go`:

```go
// Update persists changes to a project with optimistic locking.
// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
func (r *ProjectRepo) Update(ctx context.Context, p *domain.Project) error {
	settingsJSON, err := json.Marshal(p.Settings)
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	nowStr := now.Format(timeFormat)
	res, err := r.db.ExecContext(ctx, `
		UPDATE projects SET
			name = ?, workflow_id = ?, settings = ?,
			version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`,
		p.Name, p.WorkflowID.String(), string(settingsJSON), nowStr,
		p.ID.String(), p.Version,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		err := r.db.QueryRowContext(ctx,
			`SELECT 1 FROM projects WHERE id = ?`, p.ID.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	p.Version++
	p.UpdatedAt = now
	return nil
}

// CountByWorkflow returns how many projects reference the given workflow.
// Used by the workflow delete guard.
func (r *ProjectRepo) CountByWorkflow(ctx context.Context, workflowID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM projects WHERE workflow_id = ?`,
		workflowID.String()).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./sqlite/... -run 'TestProjectRepo_Update|TestProjectRepo_CountByWorkflow' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sqlite/project.go sqlite/project_test.go
git commit -m "feat(sqlite): implement ProjectRepo.Update and CountByWorkflow"
```

---

## Task 6: `sqlite.ProjectRepo.Delete`

Version-checked delete. The FK constraint from `tasks.project_id` (added in Phase 3) will surface at `DELETE` time as a SQLite FK error for projects still referenced by tasks, but Phase 2 does not yet have that FK — so the test below only exercises the version-check path and the bare delete.

**Files:**
- Modify: `sqlite/project.go` — add `Delete` method
- Modify: `sqlite/project_test.go` — add delete tests

- [ ] **Step 1: Write the failing delete tests**

Append to `sqlite/project_test.go`:

```go
func TestProjectRepo_Delete(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()

	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, p.ID, p.Version); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := repo.GetByID(ctx, p.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("after Delete, GetByID: got %v, want ErrNotFound", err)
	}
}

func TestProjectRepo_Delete_StaleVersion(t *testing.T) {
	repo := newTestProjectRepo(t)
	ctx := context.Background()

	p := sampleProject("backend")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	err := repo.Delete(ctx, p.ID, 1)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale Delete: got %v, want ErrConflict", err)
	}
}

func TestProjectRepo_Delete_NotFound(t *testing.T) {
	repo := newTestProjectRepo(t)
	err := repo.Delete(context.Background(), uuid.New(), 1)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete missing: got %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./sqlite/... -run 'TestProjectRepo_Delete' -v`
Expected: FAIL with "undefined: (*sqlite.ProjectRepo).Delete".

- [ ] **Step 3: Implement `Delete`**

Append to `sqlite/project.go`:

```go
// Delete removes a project with optimistic locking on version.
// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
// After Phase 3, this call may also surface a SQLite FK error when the project
// is still referenced by tasks — that is expected and handled by the service layer.
func (r *ProjectRepo) Delete(ctx context.Context, id uuid.UUID, version int) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM projects WHERE id = ? AND version = ?`,
		id.String(), version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		err := r.db.QueryRowContext(ctx,
			`SELECT 1 FROM projects WHERE id = ?`, id.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./sqlite/... -run 'TestProjectRepo_Delete' -v`
Expected: PASS.

- [ ] **Step 5: Full test sweep**

Run: `make test`
Expected: PASS.

Run: `make test-race`
Expected: PASS.

Run: `make vet`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add sqlite/project.go sqlite/project_test.go
git commit -m "feat(sqlite): implement ProjectRepo.Delete with optimistic locking"
```

---

## Acceptance Criteria — User-Visible Behavior Still Works

At the end of this phase, every one of these must still hold:

- `make build`, `make test`, `make test-race`, `make vet`: clean.
- `tusk project list` shows the same output it did before this phase (still powered by `inmem.ProjectRepository` reading from config).
- `tusk project create backend workflow=kanban` still writes to the config file, unchanged.
- `tusk task create "x" project=backend` still creates a task bound to the config-defined backend project.
- All E2E tests in `tests/e2e/` pass without modification.

## Changes Introduced

**New files:**
- `migrations/004_projects.up.sql`
- `migrations/004_projects.down.sql`
- `sqlite/project.go`
- `sqlite/project_test.go`
- `sqlite/project_seed_test.go`

**Modified interfaces / types:**
- `domain.Project` gains `Name string`, `WorkflowID uuid.UUID`, `Version int`, `CreatedAt time.Time`, `UpdatedAt time.Time`. `ID` changes type from `string` to `uuid.UUID`. The string workflow name is retained on the new `Workflow` field for compatibility with existing service code.
- `repository.ProjectRepository.GetByID(ctx, string)` renamed to `GetByName(ctx, string)`. The interface is still read-only; `Create`, `Update`, `Delete`, `GetByID(uuid.UUID)`, and `CountByWorkflow` live on the concrete `sqlite.ProjectRepo` type only — they are promoted to the interface in Phase 3 (for `GetByID`) and in the next initiative (for the rest).
- `inmem.ProjectRepository.GetByID` renamed to `GetByName`; builder now synthesizes `ID uuid.UUID` deterministically.
- `service.WorkflowService.GetWorkflowWithProjects` now appends `p.Name` instead of `p.ID` in its result slice. The slice still contains the human project names as strings — no observable behavior change at the call site, since the old `p.ID` held the same human string.

**Schema migrations:**
- `004_projects` — creates `projects` table with FK `workflow_id → workflows(id) ON DELETE RESTRICT`. Seeds `_default` with UUID `00000000-0000-0000-0000-000000000000`, bound to the kanban workflow seeded in Phase 1.

**Dependencies:**
- None.

**Bridge code:**
- `domain.Project.Workflow string` field is a compatibility surface for services that currently call `WorkflowService.GetStatusByRole(ctx, project.Workflow, ...)` and similar name-keyed lookups in `service/task.go`. It is populated by `inmem.ProjectRepository.buildProjectMap` from the config's workflow name so every current service consumer keeps compiling.
  - **Removal target:** Phase 4 of this plan — "Remove `project.Workflow` Compat Field". Phase 4 adds `WorkflowRepository.GetByID(uuid.UUID)` and a `workflowName` helper in `service/task.go`, then deletes the field.
