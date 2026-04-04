# Config-based Projects — Phase 2: Domain & Repository Layer

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite the domain Project type, simplify the ProjectRepository interface to read-only, implement the in-memory repository, and rewrite ProjectService. After this phase the project layer is fully config-driven but consumers (TaskService, CLI, MCP) still use the old types — they'll be updated in later phases.

**Architecture:** Domain types change from UUID-based DB entities to simple config-backed structs. The repository interface drops all write methods. The in-memory implementation replaces SQLite. ProjectService becomes a thin read-only wrapper.

**Tech Stack:** Go

**Prerequisite:** Phase 1 (config types, validation, auto-creation) must be complete.

**Design spec:** `docs/superpowers/specs/2026-04-04-config-based-projects-design.md`

**Important:** After this phase the code will NOT compile — TaskService, CLI, and MCP still reference the old `domain.Project` shape and old `ProjectRepository` interface. Phases 3-5 fix those consumers. If you want an incremental approach where the code compiles after each phase, you may introduce temporary adapter code in this phase and remove it later; however the recommended approach is to complete phases 2-5 as a batch.

---

### Task 1: Rewrite domain.Project struct

**Files:**
- Modify: `internal/domain/project.go`
- Keep unchanged: `internal/domain/project_settings.go`

Replace the DB-entity `Project` struct with a config-backed one. Remove the `uuid`, `time` imports since they're no longer needed.

- [ ] **Step 1: Rewrite internal/domain/project.go**

Replace the entire file contents with:

```go
package domain

// Project is a config-driven container for tasks. Projects are defined in
// config.toml and loaded into memory at startup. They are immutable at runtime.
type Project struct {
	ID       string          // Human-readable identifier, e.g. "default", "backend". The config key.
	Workflow string          // Name of the workflow for tasks in this project, e.g. "kanban".
	Settings ProjectSettings // Automation config (auto-complete/revert parent propagation).
}
```

- [ ] **Step 2: Verify the domain package compiles**

Run: `go build ./internal/domain/...`
Expected: PASS (project_settings.go and other files don't depend on the removed fields).

Note: Other packages that import `domain.Project` will now fail to compile. That's expected — we fix them in subsequent tasks and phases.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/project.go
git commit -m "feat(domain): rewrite Project as config-backed struct"
```

---

### Task 2: Simplify ProjectRepository interface

**Files:**
- Modify: `internal/repository/project.go`

Drop all write methods (`Create`, `Update`, `Delete`) and `GetByName`. The ID is now the human-readable name, so `GetByID(string)` replaces both `GetByID(uuid.UUID)` and `GetByName(string)`.

- [ ] **Step 1: Rewrite internal/repository/project.go**

Replace the entire file contents with:

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
	// Returns domain.ErrNotFound if the project doesn't exist in config.
	GetByID(ctx context.Context, id string) (*domain.Project, error)

	// List returns all projects defined in config, sorted by ID.
	List(ctx context.Context) ([]*domain.Project, error)
}
```

- [ ] **Step 2: Verify the repository package compiles**

Run: `go build ./internal/repository/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/repository/project.go
git commit -m "feat(repository): simplify ProjectRepository to read-only string-keyed interface"
```

---

### Task 3: Implement in-memory ProjectRepository

**Files:**
- Create: `internal/inmem/project.go`
- Create: `internal/inmem/project_test.go`

Build the in-memory implementation backed by config data. The constructor takes `map[string]config.ProjectConfig` (from `cfg.Projects`) and builds `domain.Project` instances.

- [ ] **Step 1: Write failing test**

Create `internal/inmem/project_test.go`:

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

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/inmem -run TestProjectRepository`
Expected: Compilation error — `inmem` package doesn't exist yet.

- [ ] **Step 3: Implement in-memory ProjectRepository**

Create `internal/inmem/project.go`:

```go
package inmem

import (
	"context"
	"sort"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
)

// ProjectRepository is a read-only, in-memory implementation of
// repository.ProjectRepository backed by config data.
type ProjectRepository struct {
	projects map[string]*domain.Project
}

// NewProjectRepository builds an in-memory project repository from config.
// The constructor converts config types to domain types. The resulting
// repository is immutable — no locking is needed.
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

// GetByID returns a project by its human-readable ID.
// Returns domain.ErrNotFound if the ID doesn't match any config entry.
func (r *ProjectRepository) GetByID(_ context.Context, id string) (*domain.Project, error) {
	p, ok := r.projects[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return p, nil
}

// List returns all projects sorted by ID for deterministic output.
func (r *ProjectRepository) List(_ context.Context) ([]*domain.Project, error) {
	result := make([]*domain.Project, 0, len(r.projects))
	for _, p := range r.projects {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/inmem -run TestProjectRepository`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/inmem/project.go internal/inmem/project_test.go
git commit -m "feat(inmem): implement in-memory ProjectRepository backed by config"
```

---

### Task 4: Rewrite ProjectService to read-only

**Files:**
- Modify: `internal/service/project.go`

Remove `Create`, `Modify`, `GetByName`, `ModifyOptions`, `applySettingsChanges`, and all supporting types. Keep `List` and replace `GetByID(uuid.UUID)` with `GetByID(string)`.

- [ ] **Step 1: Rewrite internal/service/project.go**

Replace the entire file contents with:

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

- [ ] **Step 2: Verify the service package compiles (it may not due to TaskService dependencies)**

Run: `go build ./internal/service/...`

Note: This may fail because `TaskService` still references `ProjectRepository.GetByID(uuid.UUID)`. That's expected — Phase 3 fixes TaskService. If you want to verify just this file compiles, you can check for syntax errors only.

- [ ] **Step 3: Commit**

```bash
git add internal/service/project.go
git commit -m "feat(service): rewrite ProjectService to read-only"
```
