# Config-based Projects — Revised Phase 2: Full Type Migration

**Goal:** Change projects from UUID-based DB entities to config-backed string-keyed structs. This changes `domain.Project`, `Task.ProjectID`, `Workflow.ProjectID`, and ALL consumers in one phase so the code compiles and tests pass at the end.

**Outcome:** After this phase, projects are read-only and loaded from config. The `projects` table is dropped from the DB. Task project_id is a plain string (not UUID). All existing unit tests pass. E2E tests are updated in Phase 3.

**Prerequisite:** Phase 1 (config types, validation, auto-creation) must be complete.

**Design spec:** `docs/superpowers/specs/2026-04-04-config-based-projects-design.md`

---

## Overview of all files changed

This phase touches ~20 files. The changes are organized bottom-up (domain → repository → storage → service → filter → CLI → MCP → wiring) so you can verify compilation at each layer boundary.

| Layer | Files | Nature of change |
|-------|-------|-----------------|
| Domain | `domain/project.go`, `domain/task.go`, `domain/filter.go`, `domain/workflow.go` | Type changes |
| Repository | `repository/project.go`, `repository/workflow.go` | Interface changes |
| Storage (new) | `inmem/project.go`, `inmem/project_test.go` | New package |
| Storage (SQLite) | `sqlite/project.go` (DELETE), `sqlite/task.go`, `sqlite/workflow.go`, `sqlite/store.go` | Remove project repo, adapt for string IDs |
| Migration | `migrations/001_initial.up.sql` (REWRITE), `002_*.sql` and `003_*.sql` (DELETE) | Drop projects table, string project_id |
| Service | `service/project.go`, `service/task.go`, `service/workflow.go` | Adapt for string IDs |
| Filter | `filter/resolve.go` | Remove ProjectLookup |
| CLI | `tui/project.go`, `tui/commands.go`, `tui/render.go`, `tui/app.go` | Remove create/modify, adapt for string IDs |
| MCP | `mcp/tools.go`, `mcp/server.go`, `mcp/resources.go` | Remove project_create, adapt for string IDs |
| Wiring | `cmd/tusk/main.go` | Use inmem repo |

---

## Task 1: Domain layer type changes

These changes will break compilation of downstream packages. That is expected — we fix them in subsequent tasks.

### Step 1: Rewrite `internal/domain/project.go`

Replace the ENTIRE contents of this file with:

```go
package domain

// Project is a config-driven container for tasks. Projects are defined in
// config.toml and loaded into memory at startup. They are immutable at runtime.
type Project struct {
	ID       string          // Human-readable identifier from config key (e.g. "default", "backend")
	Workflow string          // Name of the workflow for this project (e.g. "kanban")
	Settings ProjectSettings // Automation settings (auto-complete/revert parent)
}
```

### Step 2: Change `Task.ProjectID` in `internal/domain/task.go`

**Edit 1** — In the `Task` struct, find:
```go
	ProjectID      *uuid.UUID
```
Replace with:
```go
	ProjectID      string
```

**Edit 2** — In the `TaskUpdate` struct, find:
```go
	ProjectID      **uuid.UUID
```
Replace with:
```go
	ProjectID      *string
```

The `uuid` import stays — it's still used for `ID uuid.UUID` and `ParentID *uuid.UUID`.

### Step 3: Change `TaskFilter.ProjectID` in `internal/domain/filter.go`

Find:
```go
	ProjectID   *uuid.UUID
```
Replace with:
```go
	ProjectID   *string
```

The `uuid` import stays — still used for `ParentID *uuid.UUID` and `RootID *uuid.UUID`.

### Step 4: Change `Workflow.ProjectID` in `internal/domain/workflow.go`

Find:
```go
	ProjectID uuid.UUID
```
Replace with:
```go
	ProjectID string
```

The `uuid` import stays — still used for `ID uuid.UUID` in both `Workflow` and `WorkflowTransition`.

### Step 5: Verify domain compiles

Run: `go build ./internal/domain/...`
Expected: PASS. Domain has no dependencies on other internal packages.

---

## Task 2: Repository layer changes

### Step 1: Rewrite `internal/repository/project.go`

Replace the ENTIRE contents with:

```go
package repository

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
)

// ProjectRepository provides read-only access to projects.
// Projects are config-driven and immutable at runtime.
type ProjectRepository interface {
	// GetByID returns a project by its human-readable ID (e.g. "default", "backend").
	// Returns domain.ErrNotFound if the project doesn't exist.
	GetByID(ctx context.Context, id string) (*domain.Project, error)

	// List returns all projects, sorted by ID.
	List(ctx context.Context) ([]*domain.Project, error)
}
```

### Step 2: Change `WorkflowRepository.GetByProjectAndName` in `internal/repository/workflow.go`

Find:
```go
	GetByProjectAndName(ctx context.Context, projectID uuid.UUID, name string) (*domain.Workflow, error)
```
Replace with:
```go
	GetByProjectAndName(ctx context.Context, projectID string, name string) (*domain.Workflow, error)
```

Also remove the `uuid` import line:
```go
	"github.com/google/uuid"
```

The `uuid` import is only needed if there are other UUID references in this file. Check: `GetTransitions` uses `uuid.UUID` for `workflowID`, so the import STAYS. Only remove it if GetTransitions also doesn't use uuid — but it does: `GetTransitions(ctx context.Context, workflowID uuid.UUID)`. So **keep the uuid import**.

### Step 3: Verify repository compiles

Run: `go build ./internal/repository/...`
Expected: PASS.

---

## Task 3: Create in-memory ProjectRepository

### Step 1: Create `internal/inmem/project.go`

Create the directory `internal/inmem/` and this file:

```go
package inmem

import (
	"context"
	"sort"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
)

// Compile-time check that ProjectRepository implements the interface.
var _ repository.ProjectRepository = (*ProjectRepository)(nil)

// ProjectRepository is a read-only, in-memory implementation of
// repository.ProjectRepository backed by config data.
type ProjectRepository struct {
	projects map[string]*domain.Project
}

// NewProjectRepository builds an in-memory project repository from config.
func NewProjectRepository(cfgProjects map[string]config.ProjectConfig) *ProjectRepository {
	projects := make(map[string]*domain.Project, len(cfgProjects))
	for id, cfg := range cfgProjects {
		p := &domain.Project{
			ID:       id,
			Workflow: cfg.Workflow,
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
		projects[id] = p
	}
	return &ProjectRepository{projects: projects}
}

// GetByID returns a defensive copy of the project. Returns domain.ErrNotFound if not found.
func (r *ProjectRepository) GetByID(_ context.Context, id string) (*domain.Project, error) {
	p, ok := r.projects[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	// Return a copy so callers can't mutate our internal state
	cp := *p
	return &cp, nil
}

// List returns all projects sorted by ID. Each project is a defensive copy.
func (r *ProjectRepository) List(_ context.Context) ([]*domain.Project, error) {
	result := make([]*domain.Project, 0, len(r.projects))
	for _, p := range r.projects {
		cp := *p
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}
```

### Step 2: Create `internal/inmem/project_test.go`

```go
package inmem_test

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/inmem"
)

func TestProjectRepository_GetByID(t *testing.T) {
	projects := map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {
			Workflow: "kanban",
			Settings: config.ProjectSettingsConfig{
				AutoCompleteParent: &config.AutoCompleteParentConfig{
					TriggerStatus: "completed",
					TargetStatus:  "completed",
				},
			},
		},
	}

	repo := inmem.NewProjectRepository(projects)
	ctx := context.Background()

	t.Run("existing project", func(t *testing.T) {
		p, err := repo.GetByID(ctx, "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.ID != "default" {
			t.Errorf("expected ID 'default', got %q", p.ID)
		}
		if p.Workflow != "kanban" {
			t.Errorf("expected Workflow 'kanban', got %q", p.Workflow)
		}
	})

	t.Run("project with settings", func(t *testing.T) {
		p, err := repo.GetByID(ctx, "backend")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Settings.AutoCompleteParent == nil {
			t.Fatal("expected AutoCompleteParent settings")
		}
		if p.Settings.AutoCompleteParent.TriggerStatus != "completed" {
			t.Errorf("expected trigger_status 'completed', got %q", p.Settings.AutoCompleteParent.TriggerStatus)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := repo.GetByID(ctx, "nonexistent")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestProjectRepository_List(t *testing.T) {
	projects := map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {Workflow: "kanban"},
		"mobile":  {Workflow: "kanban"},
	}

	repo := inmem.NewProjectRepository(projects)
	ctx := context.Background()

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(list))
	}

	// Verify sorted by ID
	if list[0].ID != "backend" {
		t.Errorf("expected first project 'backend', got %q", list[0].ID)
	}
	if list[1].ID != "default" {
		t.Errorf("expected second project 'default', got %q", list[1].ID)
	}
	if list[2].ID != "mobile" {
		t.Errorf("expected third project 'mobile', got %q", list[2].ID)
	}
}
```

### Step 3: Verify inmem compiles and tests pass

Run: `go test -v ./internal/inmem/...`
Expected: PASS.

---

## Task 4: Update SQLite layer

### Step 1: Delete `internal/sqlite/project.go`

Delete the entire file. Projects are no longer stored in SQLite.

### Step 2: Rewrite migrations (breaking change — users must delete their DB)

This is a breaking change. Tusk is pre-release with no users, so we rewrite the initial migration and delete the incremental ones.

**Delete these files:**
- `migrations/002_project_settings.up.sql`
- `migrations/002_project_settings.down.sql`
- `migrations/003_project_version.up.sql`
- `migrations/003_project_version.down.sql`

**Rewrite `migrations/001_initial.up.sql`** — replace the ENTIRE file with:

```sql
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    short_id TEXT NOT NULL UNIQUE,
    parent_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
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
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    modified_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_tasks_short_id ON tasks(short_id);
CREATE INDEX idx_tasks_parent_id ON tasks(parent_id);
CREATE INDEX idx_tasks_project_id ON tasks(project_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_due_at ON tasks(due_at);
CREATE INDEX idx_tasks_wait_until ON tasks(wait_until);

CREATE TABLE annotations (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_annotations_task_id ON annotations(task_id);

CREATE TABLE relations (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    relation_type TEXT NOT NULL CHECK (relation_type IN ('blocks', 'relates_to', 'duplicates')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(source_id, target_id, relation_type)
);

CREATE INDEX idx_relations_source ON relations(source_id);
CREATE INDEX idx_relations_target ON relations(target_id);

CREATE TABLE tags (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    color TEXT
);

CREATE TABLE tag_assignments (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, tag_id)
);

CREATE INDEX idx_tag_assignments_tag ON tag_assignments(tag_id);

-- Workflow tables kept until Declarative Workflows initiative.
-- project_id is a plain string (no FK — projects are config-driven).
CREATE TABLE workflows (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    name TEXT NOT NULL,
    statuses TEXT NOT NULL DEFAULT '["pending","active","completed","deleted"]',
    UNIQUE(project_id, name)
);

CREATE TABLE workflow_transitions (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    from_status TEXT NOT NULL,
    to_status TEXT NOT NULL,
    UNIQUE(workflow_id, from_status, to_status)
);

-- Seed default workflow for the "default" project (string ID, not UUID)
INSERT INTO workflows (id, project_id, name, statuses)
VALUES ('00000000-0000-0000-0000-000000000001',
        'default',
        'default',
        '["pending","active","completed","deleted"]');

INSERT INTO workflow_transitions (id, workflow_id, from_status, to_status) VALUES
    ('00000000-0000-0000-0000-100000000001', '00000000-0000-0000-0000-000000000001', 'pending', 'active'),
    ('00000000-0000-0000-0000-100000000002', '00000000-0000-0000-0000-000000000001', 'pending', 'deleted'),
    ('00000000-0000-0000-0000-100000000003', '00000000-0000-0000-0000-000000000001', 'active', 'completed'),
    ('00000000-0000-0000-0000-100000000004', '00000000-0000-0000-0000-000000000001', 'active', 'pending'),
    ('00000000-0000-0000-0000-100000000005', '00000000-0000-0000-0000-000000000001', 'active', 'deleted'),
    ('00000000-0000-0000-0000-100000000006', '00000000-0000-0000-0000-000000000001', 'completed', 'pending');
```

Key differences from old migration:
- **No `projects` table** — projects are config-driven
- **`tasks.project_id`** is `TEXT NOT NULL DEFAULT 'default'` — no FK to projects
- **`workflows.project_id`** is `TEXT NOT NULL` — no FK to projects (was `REFERENCES projects(id)`)
- **Seed data** uses `'default'` as project_id instead of the nil UUID

### Step 3: Update `internal/sqlite/task.go` — scanTask function

**Edit 1** — In `scanTask`, find:
```go
		projectID  sql.NullString
```
Replace with:
```go
		projectID  string
```

**Edit 2** — In `scanTask`, find:
```go
	t.ProjectID, err = parseUUID(projectID)
	if err != nil {
		return nil, fmt.Errorf("parsing project_id: %w", err)
	}
```
Replace with:
```go
	t.ProjectID = projectID
```

### Step 4: Update `internal/sqlite/task.go` — Create function

Find (line ~37):
```go
		nullableUUID(task.ParentID), nullableUUID(task.ProjectID),
```
Replace with:
```go
		nullableUUID(task.ParentID), task.ProjectID,
```

### Step 5: Update `internal/sqlite/task.go` — Update function

Find (line ~72):
```go
		nullableUUID(task.ParentID), nullableUUID(task.ProjectID),
```
Replace with:
```go
		nullableUUID(task.ParentID), task.ProjectID,
```

### Step 6: Update `internal/sqlite/task.go` — buildFilter function

Find (line ~179):
```go
	if filter.ProjectID != nil {
		conditions = append(conditions, "project_id = ?")
		args = append(args, filter.ProjectID.String())
	}
```
Replace with:
```go
	if filter.ProjectID != nil {
		conditions = append(conditions, "project_id = ?")
		args = append(args, *filter.ProjectID)
	}
```

### Step 7: Update `internal/sqlite/workflow.go` — GetByProjectAndName

Change the function signature parameter from `projectID uuid.UUID` to `projectID string`.

Then update the body: find all `projectID.String()` and replace with `projectID`. Also, the scan for `project_id` should go into `wf.ProjectID` directly (it's now a string), without UUID parsing.

Find the query line that currently does something like:
```go
		projectID.String(), name,
```
Replace with:
```go
		projectID, name,
```

And where it parses `project_id` from the scan result back into a UUID — replace that with a direct string assignment to `wf.ProjectID`.

### Step 8: Update `internal/sqlite/workflow.go` — Create

Find where it converts `wf.ProjectID.String()` and replace with just `wf.ProjectID`.

### Step 9: Update `internal/sqlite/store.go`

**Edit 1** — Delete the `Projects()` method on `Tx`:
```go
// Projects returns a ProjectRepo operating within this transaction.
func (t *Tx) Projects() *ProjectRepo { return NewProjectRepo(t.tx) }
```

**Edit 2** — Simplify `WithTaskTx`. Find:
```go
func (s *Store) WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, pr repository.ProjectRepository, wr repository.WorkflowRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Tasks(), tx.Projects(), tx.Workflows())
	})
}
```
Replace with:
```go
func (s *Store) WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Tasks())
	})
}
```

### Step 10: Verify SQLite layer compiles independently

Run: `go build ./internal/sqlite/...`
This should pass now that project.go is deleted and the remaining files use string IDs.

---

## Task 5: Update service layer

### Step 1: Rewrite `internal/service/project.go`

Replace the ENTIRE file with:

```go
package service

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
)

// ProjectService provides read-only access to projects.
// Projects are config-driven — there are no create/update/delete operations.
type ProjectService struct {
	projectRepo repository.ProjectRepository
}

// NewProjectService creates a new ProjectService.
func NewProjectService(projectRepo repository.ProjectRepository) *ProjectService {
	return &ProjectService{projectRepo: projectRepo}
}

// GetByID retrieves a project by its human-readable ID (e.g. "default", "backend").
// Returns domain.ErrNotFound if not found.
func (s *ProjectService) GetByID(ctx context.Context, id string) (*domain.Project, error) {
	return s.projectRepo.GetByID(ctx, id)
}

// List returns all projects from config.
func (s *ProjectService) List(ctx context.Context) ([]*domain.Project, error) {
	return s.projectRepo.List(ctx)
}
```

### Step 2: Update `internal/service/workflow.go`

Change ALL method signatures from `projectID uuid.UUID` to `projectID string`:

**Edit 1** — `IsTransitionAllowed`: change `projectID uuid.UUID` to `projectID string`
**Edit 2** — `GetStatuses`: change `projectID uuid.UUID` to `projectID string`
**Edit 3** — `GetTransitions`: change `projectID uuid.UUID` to `projectID string`

Also remove the `uuid` import if it's no longer used in this file. Check: if the file only uses `uuid` for the projectID parameter, remove it. If it imports uuid for something else, keep it.

### Step 3: Update `internal/service/task.go`

This is the most complex file. Make these changes in order:

**Edit 1** — Replace `DefaultProjectID`:
Find:
```go
var DefaultProjectID = uuid.MustParse("00000000-0000-0000-0000-000000000000")
```
Replace with:
```go
const DefaultProjectID = "default"
```

**Edit 2** — Simplify `TaskTxProvider` interface:
Find:
```go
type TaskTxProvider interface {
	WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, pr repository.ProjectRepository, wr repository.WorkflowRepository) error) error
}
```
Replace with:
```go
type TaskTxProvider interface {
	WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository) error) error
}
```

**Edit 3** — In `Create`, update default project assignment:
Find:
```go
	if task.ProjectID == nil {
		id := DefaultProjectID
		task.ProjectID = &id
	}
```
Replace with:
```go
	if task.ProjectID == "" {
		task.ProjectID = DefaultProjectID
	}
```

**Edit 4** — In `Create`, update project lookup:
Find:
```go
	project, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
```
Replace with:
```go
	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
```

**Edit 5** — In `Create`, update workflow calls:
Find:
```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, *task.ProjectID, project.DefaultWorkflow)
```
Replace with:
```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, task.ProjectID, project.Workflow)
```

Find:
```go
		return fmt.Errorf("status %q is not valid for workflow %q", task.Status, project.DefaultWorkflow)
```
Replace with:
```go
		return fmt.Errorf("status %q is not valid for workflow %q", task.Status, project.Workflow)
```

**Edit 6** — In `Update`, update project validation:
Find:
```go
	if upd.ProjectID != nil {
		if task.ProjectID == nil {
			return nil, fmt.Errorf("task must belong to a project")
		}
		_, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
```
Replace with:
```go
	if upd.ProjectID != nil {
		if task.ProjectID == "" {
			return nil, fmt.Errorf("task must belong to a project")
		}
		_, err := s.projectRepo.GetByID(ctx, task.ProjectID)
```

**Edit 7** — In `Update`, update workflow validation:
Find:
```go
		project, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("looking up project for workflow: %w", err)
		}
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, *task.ProjectID, project.DefaultWorkflow, oldStatus, task.Status)
```
Replace with:
```go
		project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("looking up project for workflow: %w", err)
		}
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, task.ProjectID, project.Workflow, oldStatus, task.Status)
```

**Edit 8** — In `Update`, simplify the WithTaskTx callback:
Find:
```go
		err := s.txProvider.WithTaskTx(ctx, func(txTaskRepo repository.TaskRepository, txProjectRepo repository.ProjectRepository, txWorkflowRepo repository.WorkflowRepository) error {
```
Replace with:
```go
		err := s.txProvider.WithTaskTx(ctx, func(txTaskRepo repository.TaskRepository) error {
```

**Edit 9** — In `Update`, update checkAutoComplete and checkAutoRevert calls:
Find:
```go
			if err := s.checkAutoComplete(ctx, updated, txTaskRepo, txProjectRepo, txWorkflowRepo); err != nil {
				return err
			}
			return s.checkAutoRevert(ctx, updated, oldStatus, txTaskRepo, txProjectRepo, txWorkflowRepo)
```
Replace with:
```go
			if err := s.checkAutoComplete(ctx, updated, txTaskRepo); err != nil {
				return err
			}
			return s.checkAutoRevert(ctx, updated, oldStatus, txTaskRepo)
```

**Edit 10** — Update `checkAutoComplete` signature:
Find:
```go
func (s *TaskService) checkAutoComplete(
	ctx context.Context,
	task *domain.Task,
	txTaskRepo repository.TaskRepository,
	txProjectRepo repository.ProjectRepository,
	txWorkflowRepo repository.WorkflowRepository,
) error {
```
Replace with:
```go
func (s *TaskService) checkAutoComplete(
	ctx context.Context,
	task *domain.Task,
	txTaskRepo repository.TaskRepository,
) error {
```

**Edit 11** — In `checkAutoComplete` body, update project lookup:
Find:
```go
		if parent.ProjectID == nil {
			return nil
		}

		project, err := txProjectRepo.GetByID(ctx, *parent.ProjectID)
```
Replace with:
```go
		if parent.ProjectID == "" {
			return nil
		}

		project, err := s.projectRepo.GetByID(ctx, parent.ProjectID)
```

**Edit 12** — In `checkAutoComplete` body, update workflow call:
Find:
```go
		txWorkflowSvc := NewWorkflowService(txWorkflowRepo)
		allowed, err := txWorkflowSvc.IsTransitionAllowed(ctx, *parent.ProjectID, project.DefaultWorkflow, parent.Status, cfg.TargetStatus)
```
Replace with:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, cfg.TargetStatus)
```

**Edit 13** — Update `checkAutoRevert` signature:
Find:
```go
func (s *TaskService) checkAutoRevert(
	ctx context.Context,
	task *domain.Task,
	oldStatus string,
	txTaskRepo repository.TaskRepository,
	txProjectRepo repository.ProjectRepository,
	txWorkflowRepo repository.WorkflowRepository,
) error {
```
Replace with:
```go
func (s *TaskService) checkAutoRevert(
	ctx context.Context,
	task *domain.Task,
	oldStatus string,
	txTaskRepo repository.TaskRepository,
) error {
```

**Edit 14** — In `checkAutoRevert` body, update project lookup:
Find:
```go
		if parent.ProjectID == nil {
			return nil
		}

		project, err := txProjectRepo.GetByID(ctx, *parent.ProjectID)
```
Replace with:
```go
		if parent.ProjectID == "" {
			return nil
		}

		project, err := s.projectRepo.GetByID(ctx, parent.ProjectID)
```

**Edit 15** — In `checkAutoRevert` body, update workflow call:
Find:
```go
		txWorkflowSvc := NewWorkflowService(txWorkflowRepo)
		allowed, err := txWorkflowSvc.IsTransitionAllowed(ctx, *parent.ProjectID, project.DefaultWorkflow, parent.Status, revertCfg.TargetStatus)
```
Replace with:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, revertCfg.TargetStatus)
```

### Step 4: Verify service layer compiles

Run: `go build ./internal/service/...`
Expected: PASS.

---

## Task 6: Update filter layer

### Step 1: Update `internal/filter/resolve.go`

**Edit 1** — Delete the `ProjectLookup` interface:
```go
// ProjectLookup is the subset of project operations the Resolver needs.
type ProjectLookup interface {
	GetByName(ctx context.Context, name string) (*domain.Project, error)
}
```

**Edit 2** — Remove `projectLookup` from the `Resolver` struct:
Find:
```go
type Resolver struct {
	projectLookup ProjectLookup
	taskLookup    TaskLookup
}
```
Replace with:
```go
type Resolver struct {
	taskLookup TaskLookup
}
```

**Edit 3** — Update `NewResolver`:
Find:
```go
func NewResolver(projectLookup ProjectLookup, taskLookup TaskLookup) *Resolver {
	return &Resolver{
		projectLookup: projectLookup,
		taskLookup:    taskLookup,
	}
}
```
Replace with:
```go
func NewResolver(taskLookup TaskLookup) *Resolver {
	return &Resolver{
		taskLookup: taskLookup,
	}
}
```

**Edit 4** — Update the `project` case in `Resolve`:
Find:
```go
		case "project":
			project, err := r.projectLookup.GetByName(ctx, field.Value)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					errs = append(errs, fmt.Errorf("project %q not found", field.Value))
				} else {
					errs = append(errs, fmt.Errorf("looking up project %q: %w", field.Value, err))
				}
				continue
			}
			tf.ProjectID = &project.ID
```
Replace with:
```go
		case "project":
			id := field.Value
			tf.ProjectID = &id
```

**Edit 5** — Clean up unused imports. The `errors` import may no longer be needed IF it was only used in the project lookup block. Check: `errors.Is` is also used in the `parent` and `tree` cases, so keep `errors`. The `domain` import is used for `domain.TaskFilter` and `domain.ErrNotFound`, so keep it.

### Step 2: Verify filter compiles

Run: `go build ./internal/filter/...`
Expected: PASS.

---

## Task 7: Update CLI layer

### Step 1: Rewrite `internal/tui/project.go`

Replace the ENTIRE file with:

```go
package tui

import (
	"github.com/spf13/cobra"
)

// buildProjectCmd creates the `tusk project` command group.
// Projects are config-driven — only list is available.
func (a *App) buildProjectCmd() *cobra.Command {
	projectCmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}

	projectCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all projects",
		Args:  cobra.NoArgs,
		RunE:  a.runProjectList,
	})

	return projectCmd
}

func (a *App) runProjectList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	projects, err := a.projectSvc.List(ctx)
	if err != nil {
		return err
	}
	return renderProjectList(cmd.OutOrStdout(), projects, a.format)
}
```

### Step 2: Update `internal/tui/commands.go`

**Edit 1** — In `runAdd`, update project handling. Find:
```go
	// Project
	if f, ok := fs.GetField("project"); ok {
		project, err := a.projectSvc.GetByName(ctx, f.Value)
		if err != nil {
			return fmt.Errorf("project %q not found", f.Value)
		}
		task.ProjectID = &project.ID
	}
```
Replace with:
```go
	// Project
	if f, ok := fs.GetField("project"); ok {
		task.ProjectID = f.Value
	}
```

**Edit 2** — In `runInfo`, update project name resolution. Find:
```go
	// Resolve project name for display
	var projectName string
	if task.ProjectID != nil {
		project, err := a.projectSvc.GetByID(ctx, *task.ProjectID)
		if err == nil {
			projectName = project.Name
		}
	}
```
Replace with:
```go
	// Project ID is now the human-readable name
	projectName := task.ProjectID
```

**Edit 3** — In `runModify`, update project handling. Find:
```go
	// Project
	if f, ok := fs.GetField("project"); ok {
		project, err := a.projectSvc.GetByName(ctx, f.Value)
		if err != nil {
			return fmt.Errorf("project %q not found", f.Value)
		}
		pid := project.ID
		pp := &pid
		upd.ProjectID = &pp
	}
```
Replace with:
```go
	// Project
	if f, ok := fs.GetField("project"); ok {
		upd.ProjectID = &f.Value
	}
```

### Step 3: Update `internal/tui/render.go`

**Edit 1** — Replace `projectJSON` struct:
Find:
```go
type projectJSON struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	DefaultWorkflow string                 `json:"default_workflow"`
	Settings        domain.ProjectSettings `json:"settings"`
	Version         int                    `json:"version"`
	CreatedAt       string                 `json:"created_at"`
}

func toProjectJSON(p *domain.Project) projectJSON {
	return projectJSON{
		ID:              p.ID.String(),
		Name:            p.Name,
		Description:     p.Description,
		DefaultWorkflow: p.DefaultWorkflow,
		Settings:        p.Settings,
		Version:         p.Version,
		CreatedAt:       p.CreatedAt.Format(time.RFC3339),
	}
}
```
Replace with:
```go
type projectJSON struct {
	ID       string                 `json:"id"`
	Workflow string                 `json:"workflow"`
	Settings domain.ProjectSettings `json:"settings"`
}

func toProjectJSON(p *domain.Project) projectJSON {
	return projectJSON{
		ID:       p.ID,
		Workflow: p.Workflow,
		Settings: p.Settings,
	}
}
```

**Edit 2** — Update `renderProjectList` text output:
Find:
```go
	if _, err := fmt.Fprintf(w, "%-20s %-30s %-10s %s\n", "NAME", "DESCRIPTION", "WORKFLOW", "SETTINGS"); err != nil {
		return err
	}
	for _, p := range projects {
		if _, err := fmt.Fprintf(w, "%-20s %-30s %-10s %s\n",
			p.Name,
			truncate(p.Description, 30),
			p.DefaultWorkflow,
			formatSettingsSummary(p.Settings),
		); err != nil {
			return err
		}
	}
```
Replace with:
```go
	if _, err := fmt.Fprintf(w, "%-20s %-10s %s\n", "ID", "WORKFLOW", "SETTINGS"); err != nil {
		return err
	}
	for _, p := range projects {
		if _, err := fmt.Fprintf(w, "%-20s %-10s %s\n",
			p.ID,
			p.Workflow,
			formatSettingsSummary(p.Settings),
		); err != nil {
			return err
		}
	}
```

**Edit 3** — Delete the `renderProjectResult` function entirely (it was used by create/modify which no longer exist).

**Edit 4** — In `toTaskJSON`, update ProjectID handling:
Find:
```go
	if t.ProjectID != nil {
		s := t.ProjectID.String()
		tj.ProjectID = &s
	}
```
Replace with:
```go
	if t.ProjectID != "" {
		tj.ProjectID = &t.ProjectID
	}
```

**Edit 5** — In `renderTaskInfo`, update project display:
Find:
```go
	if task.ProjectID != nil {
		projectDisplay := task.ProjectID.String()
		if projectName != "" {
			projectDisplay = fmt.Sprintf("%s (%s)", projectName, task.ProjectID.String())
		}
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Project:", projectDisplay); err != nil {
			return err
		}
	}
```
Replace with:
```go
	if task.ProjectID != "" {
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Project:", task.ProjectID); err != nil {
			return err
		}
	}
```

**Edit 6** — Clean up unused imports. The `time` import is still used (timestamps). Check if removing `renderProjectResult` removed the last usage of any import.

### Step 4: Update `internal/tui/app.go`

Find:
```go
	a.resolver = filter.NewResolver(projectSvc, taskSvc)
```
Replace with:
```go
	a.resolver = filter.NewResolver(taskSvc)
```

### Step 5: Verify CLI layer compiles

Run: `go build ./internal/tui/...`
Expected: PASS.

---

## Task 8: Update MCP layer

### Step 1: Update `internal/mcp/tools.go`

**Edit 1** — In `toTaskResponse`, update ProjectID:
Find:
```go
	if t.ProjectID != nil {
		s := t.ProjectID.String()
		r.ProjectID = &s
	}
```
Replace with:
```go
	if t.ProjectID != "" {
		r.ProjectID = &t.ProjectID
	}
```

**Edit 2** — In `handleTaskCreate`, update project handling:
Find:
```go
	if projectName, err := request.RequireString("project"); err == nil {
		project, lookupErr := s.projectSvc.GetByName(ctx, projectName)
		if lookupErr != nil {
			return toolError(lookupErr, "project "+projectName), nil
		}
		task.ProjectID = &project.ID
	}
```
Replace with:
```go
	if projectID, err := request.RequireString("project"); err == nil {
		task.ProjectID = projectID
	}
```

**Edit 3** — In `handleTaskList`, update project handling:
Find:
```go
	if projectName, err := request.RequireString("project"); err == nil {
		project, lookupErr := s.projectSvc.GetByName(ctx, projectName)
		if lookupErr != nil {
			return toolError(lookupErr, "project "+projectName), nil
		}
		filter.ProjectID = &project.ID
	}
```
Replace with:
```go
	if projectID, err := request.RequireString("project"); err == nil {
		filter.ProjectID = &projectID
	}
```

**Edit 4** — In `handleTaskModify`, update project handling:
Find:
```go
	if projectName, err := request.RequireString("project"); err == nil {
		project, lookupErr := s.projectSvc.GetByName(ctx, projectName)
		if lookupErr != nil {
			return toolError(lookupErr, "project "+projectName), nil
		}
		pid := project.ID
		pp := &pid
		upd.ProjectID = &pp
	}
```
Replace with:
```go
	if projectID, err := request.RequireString("project"); err == nil {
		upd.ProjectID = &projectID
	}
```

**Edit 5** — In `toTreeNodeResponse`, update ProjectID:
Find:
```go
	if task.ProjectID != nil {
		s := task.ProjectID.String()
		r.ProjectID = &s
	}
```
Replace with:
```go
	if task.ProjectID != "" {
		r.ProjectID = &task.ProjectID
	}
```

**Edit 6** — Replace `projectResponse` and `toProjectResponse`:
Find:
```go
type projectResponse struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	DefaultWorkflow string                 `json:"default_workflow"`
	Settings        domain.ProjectSettings `json:"settings"`
	Version         int                    `json:"version"`
	CreatedAt       string                 `json:"created_at"`
}

func toProjectResponse(p *domain.Project) projectResponse {
	return projectResponse{
		ID:              p.ID.String(),
		Name:            p.Name,
		Description:     p.Description,
		DefaultWorkflow: p.DefaultWorkflow,
		Settings:        p.Settings,
		Version:         p.Version,
		CreatedAt:       p.CreatedAt.Format(time.RFC3339),
	}
}
```
Replace with:
```go
type projectResponse struct {
	ID       string                 `json:"id"`
	Workflow string                 `json:"workflow"`
	Settings domain.ProjectSettings `json:"settings"`
}

func toProjectResponse(p *domain.Project) projectResponse {
	return projectResponse{
		ID:       p.ID,
		Workflow: p.Workflow,
		Settings: p.Settings,
	}
}
```

**Edit 7** — Delete the entire `handleProjectCreate` function.

### Step 2: Update `internal/mcp/server.go`

**Edit 1** — Delete the `tusk_project_create` tool registration block:
```go
	s.addTool("project",
		mcp.NewTool("tusk_project_create",
			mcp.WithDescription("Create a new project"),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Project name (must be unique)"),
			),
			mcp.WithString("description",
				mcp.Description("Project description"),
			),
		),
		s.handleProjectCreate,
	)
```

**Edit 2** — Remove `"tusk_project_create": true` from `validToolNames` map.

### Step 3: Update `internal/mcp/resources.go`

**Edit 1** — In `handleProjectResource`, change `GetByName` to `GetByID`:
Find:
```go
	project, err := s.projectSvc.GetByName(ctx, name)
```
Replace with:
```go
	project, err := s.projectSvc.GetByID(ctx, name)
```

**Edit 2** — In `handleWorkflowResource`, update project lookup and field references:
Find:
```go
	project, err := s.projectSvc.GetByName(ctx, name)
```
Replace with:
```go
	project, err := s.projectSvc.GetByID(ctx, name)
```

Find:
```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, project.ID, project.DefaultWorkflow)
```
Replace with:
```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, project.ID, project.Workflow)
```

Find:
```go
	transitions, err := s.workflowSvc.GetTransitions(ctx, project.ID, project.DefaultWorkflow)
```
Replace with:
```go
	transitions, err := s.workflowSvc.GetTransitions(ctx, project.ID, project.Workflow)
```

Find:
```go
	resp := workflowResponse{
		ProjectName: project.Name,
		Workflow:    project.DefaultWorkflow,
```
Replace with:
```go
	resp := workflowResponse{
		ProjectName: project.ID,
		Workflow:    project.Workflow,
```

### Step 4: Verify MCP layer compiles

Run: `go build ./internal/mcp/...`
Expected: PASS.

---

## Task 9: Update DI wiring and final verification

### Step 1: Update `cmd/tusk/main.go`

**Edit 1** — Add import:
```go
	"github.com/germanamz/tusk/internal/inmem"
```

**Edit 2** — Replace project repo creation:
Find:
```go
	projectRepo := sqlite.NewProjectRepo(db)
```
Replace with:
```go
	projectRepo := inmem.NewProjectRepository(cfg.Projects)
```

### Step 2: Full compilation check

Run: `go build ./...`
Expected: PASS — the entire project compiles.

### Step 3: Run unit tests

Run: `go test ./internal/config/... ./internal/inmem/... ./internal/domain/... ./internal/repository/...`
Expected: PASS.

Run: `go test ./internal/service/...`
Note: Service tests may need minor updates if they create tasks with UUID project IDs. If tests fail, fix them by changing `ProjectID: &someUUID` to `ProjectID: "default"`.

Run: `go test ./internal/sqlite/...`
Note: SQLite tests that reference `ProjectRepo` or create projects via SQL will fail. Remove or update those tests.

### Step 4: Run vet and lint

Run: `make vet`
Run: `make lint`
Expected: PASS.

---

## Commits

Break into logical commits:

```bash
# Domain + repository types
git add internal/domain/ internal/repository/
git commit -m "feat(domain): change ProjectID to string, simplify Project to config-backed struct"

# In-memory implementation
git add internal/inmem/
git commit -m "feat(inmem): implement in-memory ProjectRepository backed by config"

# SQLite changes (consolidated migration, delete project repo)
git rm internal/sqlite/project.go
git rm migrations/002_project_settings.up.sql migrations/002_project_settings.down.sql
git rm migrations/003_project_version.up.sql migrations/003_project_version.down.sql
git add internal/sqlite/ migrations/001_initial.up.sql
git commit -m "feat(sqlite): consolidate migrations, drop projects table, string project IDs"

# Service changes
git add internal/service/
git commit -m "feat(service): update services for string project IDs and read-only projects"

# Filter + CLI + MCP + wiring
git add internal/filter/ internal/tui/ internal/mcp/ cmd/tusk/
git commit -m "feat: update filter, CLI, MCP, and DI wiring for config-driven projects"
```
