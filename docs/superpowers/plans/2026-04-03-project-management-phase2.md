# Project Management Phase 2: ProjectService

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `ProjectService` with CRUD operations, dot-path settings merge logic, and `ModifyOptions`.

**Architecture:** `ProjectService` wraps `repository.ProjectRepository` with business logic: UUID generation on create, settings merge via a known dot-path map on modify, and optimistic locking awareness. The dot-path resolver is not a generic JSON walker — it maps a fixed set of paths to `ProjectSettings` struct fields.

**Tech Stack:** Go, testing with real SQLite (no mocks)

**Prerequisite:** Phase 1 must be complete (migration, domain `Version` field, optimistic locking in `ProjectRepo`).

---

### Task 1: Create ProjectService with Create and Read Methods

**Files:**
- Modify: `internal/service/project.go` (currently empty — only has `package service`)
- Create: `internal/service/project_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/service/project_test.go`:

```go
package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

func testProjectService(t *testing.T) *ProjectService {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	repo := sqlite.NewProjectRepo(store.DB())
	return NewProjectService(repo)
}

func TestProjectService_Create(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	p, err := svc.Create(ctx, "backend", "Backend services")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.Name != "backend" {
		t.Fatalf("expected name 'backend', got %q", p.Name)
	}
	if p.Description != "Backend services" {
		t.Fatalf("expected description 'Backend services', got %q", p.Description)
	}
	if p.DefaultWorkflow != "default" {
		t.Fatalf("expected workflow 'default', got %q", p.DefaultWorkflow)
	}
	if p.Version != 1 {
		t.Fatalf("expected version 1, got %d", p.Version)
	}
	if p.ID.String() == "" {
		t.Fatal("expected UUID to be set")
	}
}

func TestProjectService_CreateDuplicate(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "dup", "First"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, "dup", "Second")
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
}

func TestProjectService_CreateEmptyName(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, "", "No name")
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestProjectService_List(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	// _default is seeded by migration
	if _, err := svc.Create(ctx, "proj1", ""); err != nil {
		t.Fatal(err)
	}

	projects, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// _default + proj1 = 2
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

func TestProjectService_GetByName(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "myproj", "My project"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetByName(ctx, "myproj")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Name != "myproj" {
		t.Fatalf("expected 'myproj', got %q", got.Name)
	}
}

func TestProjectService_GetByNameNotFound(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	_, err := svc.GetByName(ctx, "nonexistent")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test -v ./internal/service -run "TestProjectService_Create|TestProjectService_List|TestProjectService_GetByName"
```

Expected: FAIL — `NewProjectService` does not exist.

- [ ] **Step 3: Implement the service**

Replace the contents of `internal/service/project.go` with:

```go
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// ProjectService encapsulates project business logic including CRUD
// operations and settings management.
type ProjectService struct {
	projectRepo repository.ProjectRepository
}

// NewProjectService creates a new ProjectService.
func NewProjectService(projectRepo repository.ProjectRepository) *ProjectService {
	return &ProjectService{projectRepo: projectRepo}
}

// Create creates a new project with the given name and description.
// It generates a UUID, sets the default workflow to "default", and
// initializes version to 1 with empty settings.
func (s *ProjectService) Create(ctx context.Context, name, description string) (*domain.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("project name must not be empty")
	}

	p := &domain.Project{
		ID:              uuid.New(),
		Name:            name,
		Description:     description,
		DefaultWorkflow: "default",
		Settings:        domain.ProjectSettings{},
		Version:         1,
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.projectRepo.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("creating project %q: %w", name, err)
	}
	return p, nil
}

// List returns all projects.
func (s *ProjectService) List(ctx context.Context) ([]*domain.Project, error) {
	return s.projectRepo.List(ctx)
}

// GetByName retrieves a project by its unique name.
// Returns domain.ErrNotFound if not found.
func (s *ProjectService) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	return s.projectRepo.GetByName(ctx, name)
}

// GetByID retrieves a project by its UUID.
// Returns domain.ErrNotFound if not found.
func (s *ProjectService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	return s.projectRepo.GetByID(ctx, id)
}
```

- [ ] **Step 4: Run the tests**

```bash
go test -v ./internal/service -run "TestProjectService_Create|TestProjectService_List|TestProjectService_GetByName"
```

Expected: All 5 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/service/project.go internal/service/project_test.go
git commit -m "feat: add ProjectService with Create, List, GetByName, GetByID"
```

---

### Task 2: Add Modify Method with Description Updates

**Files:**
- Modify: `internal/service/project.go`
- Modify: `internal/service/project_test.go`

- [ ] **Step 1: Write failing tests for Modify**

Append to `internal/service/project_test.go`:

```go
func TestProjectService_ModifyDescription(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", "Old description"); err != nil {
		t.Fatal(err)
	}

	desc := "New description"
	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Description: &desc,
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if updated.Description != "New description" {
		t.Fatalf("expected 'New description', got %q", updated.Description)
	}
	// Version should have incremented
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
}

func TestProjectService_ModifyNotFound(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	desc := "whatever"
	_, err := svc.Modify(ctx, "nonexistent", ModifyOptions{
		Description: &desc,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestProjectService_ModifyNoOptions(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", "Desc"); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Modify(ctx, "proj", ModifyOptions{})
	if err == nil {
		t.Fatal("expected error when no modifications provided")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
go test -v ./internal/service -run "TestProjectService_Modify"
```

Expected: FAIL — `Modify` and `ModifyOptions` do not exist.

- [ ] **Step 3: Implement ModifyOptions and Modify**

Add to `internal/service/project.go`, after the `GetByID` method:

```go
// ModifyOptions specifies what to change on a project.
// All fields are optional — nil means "don't change".
type ModifyOptions struct {
	Description *string           // New description (never nullable, just changeable)
	Sets        map[string]string // Dot-path key → value for settings
	Unsets      []string          // Dot-path keys to nil out in settings
}

// isEmpty returns true if no modifications are specified.
func (o ModifyOptions) isEmpty() bool {
	return o.Description == nil && len(o.Sets) == 0 && len(o.Unsets) == 0
}

// Modify updates a project's fields and/or settings. It fetches the project
// by name, applies the changes, and persists via optimistic-locked update.
// Returns the updated project as read back from the database.
func (s *ProjectService) Modify(ctx context.Context, name string, opts ModifyOptions) (*domain.Project, error) {
	if opts.isEmpty() {
		return nil, fmt.Errorf("no modifications specified")
	}

	project, err := s.projectRepo.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("looking up project %q: %w", name, err)
	}

	if opts.Description != nil {
		project.Description = *opts.Description
	}

	if err := applySettingsChanges(&project.Settings, opts.Sets, opts.Unsets); err != nil {
		return nil, err
	}

	if err := s.projectRepo.Update(ctx, project); err != nil {
		return nil, fmt.Errorf("updating project %q: %w", name, err)
	}

	// Re-read to get the incremented version
	return s.projectRepo.GetByName(ctx, name)
}
```

Also add the `applySettingsChanges` stub (we'll implement it fully in Task 3, but it needs to compile):

```go
// applySettingsChanges applies dot-path --set and --unset operations to settings.
// Returns an error for unknown dot-paths.
func applySettingsChanges(settings *domain.ProjectSettings, sets map[string]string, unsets []string) error {
	// Settings merge logic will be implemented in the next task.
	// For now, this is a no-op if no sets/unsets are provided.
	if len(sets) > 0 || len(unsets) > 0 {
		return fmt.Errorf("settings changes not yet implemented")
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

```bash
go test -v ./internal/service -run "TestProjectService_Modify"
```

Expected: All 3 Modify tests pass. `ModifyDescription` works because it only uses `Description`, not settings. `ModifyNotFound` gets `ErrNotFound` wrapped. `ModifyNoOptions` gets the "no modifications" error.

- [ ] **Step 5: Commit**

```bash
git add internal/service/project.go internal/service/project_test.go
git commit -m "feat: add ProjectService.Modify with description updates"
```

---

### Task 3: Implement Dot-Path Settings Merge Logic

**Files:**
- Modify: `internal/service/project.go`
- Modify: `internal/service/project_test.go`

- [ ] **Step 1: Write failing tests for settings changes**

Append to `internal/service/project_test.go`:

```go
func TestProjectService_ModifySetAutoComplete(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"auto_complete_parent.trigger_status": "completed",
			"auto_complete_parent.target_status":  "completed",
		},
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if updated.Settings.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to be set")
	}
	if updated.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", updated.Settings.AutoCompleteParent.TriggerStatus)
	}
	if updated.Settings.AutoCompleteParent.TargetStatus != "completed" {
		t.Fatalf("expected target_status 'completed', got %q", updated.Settings.AutoCompleteParent.TargetStatus)
	}
}

func TestProjectService_ModifySetAutoRevert(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"auto_revert_parent.trigger_status": "completed",
			"auto_revert_parent.target_status":  "pending",
		},
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if updated.Settings.AutoRevertParent == nil {
		t.Fatal("expected AutoRevertParent to be set")
	}
	if updated.Settings.AutoRevertParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", updated.Settings.AutoRevertParent.TriggerStatus)
	}
	if updated.Settings.AutoRevertParent.TargetStatus != "pending" {
		t.Fatalf("expected target_status 'pending', got %q", updated.Settings.AutoRevertParent.TargetStatus)
	}
}

func TestProjectService_ModifySetAutoInitNilConfig(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	// Setting just one field should auto-initialize the parent struct
	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"auto_complete_parent.trigger_status": "completed",
		},
	})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if updated.Settings.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to be auto-initialized")
	}
	if updated.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", updated.Settings.AutoCompleteParent.TriggerStatus)
	}
	// target_status should be zero value (empty string) since we didn't set it
	if updated.Settings.AutoCompleteParent.TargetStatus != "" {
		t.Fatalf("expected empty target_status, got %q", updated.Settings.AutoCompleteParent.TargetStatus)
	}
}

func TestProjectService_ModifyUnsetAutoComplete(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	// First, set auto-complete
	if _, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"auto_complete_parent.trigger_status": "completed",
			"auto_complete_parent.target_status":  "completed",
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Then unset it
	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Unsets: []string{"auto_complete_parent"},
	})
	if err != nil {
		t.Fatalf("Modify unset: %v", err)
	}
	if updated.Settings.AutoCompleteParent != nil {
		t.Fatal("expected AutoCompleteParent to be nil after unset")
	}
}

func TestProjectService_ModifyUnsetAutoRevert(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	// Set auto-revert
	if _, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"auto_revert_parent.trigger_status": "completed",
			"auto_revert_parent.target_status":  "pending",
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Unset it
	updated, err := svc.Modify(ctx, "proj", ModifyOptions{
		Unsets: []string{"auto_revert_parent"},
	})
	if err != nil {
		t.Fatalf("Modify unset: %v", err)
	}
	if updated.Settings.AutoRevertParent != nil {
		t.Fatal("expected AutoRevertParent to be nil after unset")
	}
}

func TestProjectService_ModifyUnknownDotPath(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Modify(ctx, "proj", ModifyOptions{
		Sets: map[string]string{
			"unknown.path": "value",
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown dot-path")
	}
}

func TestProjectService_ModifyUnknownUnsetPath(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "proj", ""); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Modify(ctx, "proj", ModifyOptions{
		Unsets: []string{"unknown_key"},
	})
	if err == nil {
		t.Fatal("expected error for unknown unset path")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test -v ./internal/service -run "TestProjectService_ModifySet|TestProjectService_ModifyUnset|TestProjectService_ModifyUnknown"
```

Expected: FAIL — the `applySettingsChanges` stub returns "not yet implemented".

- [ ] **Step 3: Implement applySettingsChanges**

In `internal/service/project.go`, replace the `applySettingsChanges` stub with the full implementation:

```go
// validSetPaths lists all dot-paths that can be used with --set.
// This is NOT a generic JSON walker — it maps a known set of paths
// to ProjectSettings struct fields.
var validSetPaths = map[string]bool{
	"auto_complete_parent.trigger_status": true,
	"auto_complete_parent.target_status":  true,
	"auto_revert_parent.trigger_status":   true,
	"auto_revert_parent.target_status":    true,
}

// validUnsetPaths lists all top-level keys that can be used with --unset.
var validUnsetPaths = map[string]bool{
	"auto_complete_parent": true,
	"auto_revert_parent":   true,
}

// applySettingsChanges applies dot-path --set and --unset operations to settings.
// Returns an error for unknown dot-paths.
func applySettingsChanges(settings *domain.ProjectSettings, sets map[string]string, unsets []string) error {
	for _, key := range unsets {
		if !validUnsetPaths[key] {
			return fmt.Errorf("unknown settings key %q (valid: auto_complete_parent, auto_revert_parent)", key)
		}
		switch key {
		case "auto_complete_parent":
			settings.AutoCompleteParent = nil
		case "auto_revert_parent":
			settings.AutoRevertParent = nil
		}
	}

	for path, value := range sets {
		if !validSetPaths[path] {
			return fmt.Errorf("unknown settings path %q", path)
		}
		switch path {
		case "auto_complete_parent.trigger_status":
			if settings.AutoCompleteParent == nil {
				settings.AutoCompleteParent = &domain.AutoCompleteConfig{}
			}
			settings.AutoCompleteParent.TriggerStatus = value
		case "auto_complete_parent.target_status":
			if settings.AutoCompleteParent == nil {
				settings.AutoCompleteParent = &domain.AutoCompleteConfig{}
			}
			settings.AutoCompleteParent.TargetStatus = value
		case "auto_revert_parent.trigger_status":
			if settings.AutoRevertParent == nil {
				settings.AutoRevertParent = &domain.AutoRevertConfig{}
			}
			settings.AutoRevertParent.TriggerStatus = value
		case "auto_revert_parent.target_status":
			if settings.AutoRevertParent == nil {
				settings.AutoRevertParent = &domain.AutoRevertConfig{}
			}
			settings.AutoRevertParent.TargetStatus = value
		}
	}

	return nil
}
```

- [ ] **Step 4: Run the tests**

```bash
go test -v ./internal/service -run "TestProjectService_Modify"
```

Expected: All Modify tests pass.

- [ ] **Step 5: Run the full test suite**

```bash
make test
```

Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/service/project.go internal/service/project_test.go
git commit -m "feat: add dot-path settings merge to ProjectService.Modify

Support --set/--unset for auto_complete_parent and auto_revert_parent
settings with auto-initialization of nil config structs."
```
