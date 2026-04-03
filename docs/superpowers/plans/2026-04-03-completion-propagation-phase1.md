# Completion Propagation — Phase 1: Data Model & Settings Infrastructure

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a JSON `settings` column to the `projects` table, update the domain type to include `ProjectSettings`, and update the SQLite project repository to read/write settings.

**Architecture:** New migration adds `settings TEXT NOT NULL DEFAULT '{}'` to the projects table. `domain.Project` gains a `Settings ProjectSettings` field with nested config structs. The SQLite `ProjectRepo` is updated to serialize/deserialize this JSON column on all reads and writes.

**Tech Stack:** Go, SQLite, `encoding/json`

**Spec:** `docs/superpowers/specs/2026-04-03-completion-propagation-design.md`

---

### Task 1: Domain Types for Project Settings

**Files:**
- Create: `internal/domain/project_settings.go`
- Test: `internal/domain/project_settings_test.go`

- [ ] **Step 1: Write the test for ProjectSettings JSON round-trip**

Create `internal/domain/project_settings_test.go`:

```go
package domain

import (
	"encoding/json"
	"testing"
)

func TestProjectSettings_JSONRoundTrip_Empty(t *testing.T) {
	// Default settings should serialize to `{}` and back
	var s ProjectSettings
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "{}" {
		t.Fatalf("expected '{}', got %q", string(data))
	}

	var decoded ProjectSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.AutoCompleteParent != nil {
		t.Fatal("expected AutoCompleteParent to be nil")
	}
	if decoded.AutoRevertParent != nil {
		t.Fatal("expected AutoRevertParent to be nil")
	}
}

func TestProjectSettings_JSONRoundTrip_WithConfig(t *testing.T) {
	s := ProjectSettings{
		AutoCompleteParent: &AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &AutoRevertConfig{
			TriggerStatus: "completed",
			TargetStatus:  "active",
		},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ProjectSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to be non-nil")
	}
	if decoded.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", decoded.AutoCompleteParent.TriggerStatus)
	}
	if decoded.AutoCompleteParent.TargetStatus != "completed" {
		t.Fatalf("expected target_status 'completed', got %q", decoded.AutoCompleteParent.TargetStatus)
	}
	if decoded.AutoRevertParent == nil {
		t.Fatal("expected AutoRevertParent to be non-nil")
	}
	if decoded.AutoRevertParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", decoded.AutoRevertParent.TriggerStatus)
	}
	if decoded.AutoRevertParent.TargetStatus != "active" {
		t.Fatalf("expected target_status 'active', got %q", decoded.AutoRevertParent.TargetStatus)
	}
}

func TestProjectSettings_JSONRoundTrip_PartialConfig(t *testing.T) {
	// Only auto-complete set, auto-revert nil
	s := ProjectSettings{
		AutoCompleteParent: &AutoCompleteConfig{
			TriggerStatus: "done",
			TargetStatus:  "done",
		},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ProjectSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to be non-nil")
	}
	if decoded.AutoRevertParent != nil {
		t.Fatal("expected AutoRevertParent to be nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/domain -run TestProjectSettings`
Expected: FAIL — `ProjectSettings`, `AutoCompleteConfig`, `AutoRevertConfig` types not defined.

- [ ] **Step 3: Write the domain types**

Create `internal/domain/project_settings.go`:

```go
package domain

// AutoCompleteConfig controls automatic parent completion when all children
// reach TriggerStatus. The parent is transitioned to TargetStatus.
type AutoCompleteConfig struct {
	TriggerStatus string `json:"trigger_status"`
	TargetStatus  string `json:"target_status"`
}

// AutoRevertConfig controls automatic parent revert when a child moves away
// from TriggerStatus. The parent is transitioned to TargetStatus.
type AutoRevertConfig struct {
	TriggerStatus string `json:"trigger_status"`
	TargetStatus  string `json:"target_status"`
}

// ProjectSettings holds per-project configuration stored as JSON in the
// projects table. Nil fields mean the feature is disabled (the default).
type ProjectSettings struct {
	AutoCompleteParent *AutoCompleteConfig `json:"auto_complete_parent,omitempty"`
	AutoRevertParent   *AutoRevertConfig   `json:"auto_revert_parent,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/domain -run TestProjectSettings`
Expected: PASS — all three test cases pass.

- [ ] **Step 5: Add Settings field to Project struct**

Modify `internal/domain/project.go`. Add the `Settings` field to the `Project` struct:

```go
type Project struct {
	ID              uuid.UUID
	Name            string
	Description     string
	DefaultWorkflow string
	Settings        ProjectSettings
	CreatedAt       time.Time
}
```

- [ ] **Step 6: Run all domain tests to confirm nothing broke**

Run: `go test -v ./internal/domain/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/domain/project_settings.go internal/domain/project_settings_test.go internal/domain/project.go
git commit -m "feat(domain): add ProjectSettings types for completion propagation"
```

---

### Task 2: Database Migration

**Files:**
- Create: `migrations/002_project_settings.up.sql`
- Create: `migrations/002_project_settings.down.sql`

- [ ] **Step 1: Write the up migration**

Create `migrations/002_project_settings.up.sql`:

```sql
ALTER TABLE projects ADD COLUMN settings TEXT NOT NULL DEFAULT '{}';
```

- [ ] **Step 2: Write the down migration**

Create `migrations/002_project_settings.down.sql`:

```sql
-- SQLite does not support DROP COLUMN prior to 3.35.0.
-- Recreate the table without the settings column.
CREATE TABLE projects_backup (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    default_workflow TEXT NOT NULL DEFAULT 'default',
    created_at TEXT NOT NULL
);

INSERT INTO projects_backup (id, name, description, default_workflow, created_at)
SELECT id, name, description, default_workflow, created_at FROM projects;

DROP TABLE projects;

ALTER TABLE projects_backup RENAME TO projects;
```

- [ ] **Step 3: Run the full test suite to verify the migration applies cleanly**

Run: `make test`
Expected: All existing tests still pass. The migration runs automatically because `sqlite.New()` applies pending migrations on startup, and all tests create fresh databases.

- [ ] **Step 4: Commit**

```bash
git add migrations/002_project_settings.up.sql migrations/002_project_settings.down.sql
git commit -m "feat(db): add settings column to projects table"
```

---

### Task 3: Update ProjectRepo to Read/Write Settings

**Files:**
- Modify: `internal/sqlite/project.go`

The `ProjectRepo` currently reads/writes 5 columns: `id, name, description, default_workflow, created_at`. We need to add `settings` as a 6th column and deserialize it into `domain.ProjectSettings` on read, serialize on write.

- [ ] **Step 1: Write a test for creating and reading a project with settings**

Add to `internal/service/task_test.go` (since this file already has `testTaskEnv` which wires up everything with an in-memory DB). Add this test function at the end of the file:

```go
func TestProjectSettings_Persistence(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// The _default project should have empty settings
	defaultProject, err := env.store.DB()
	if err != nil {
		t.Fatalf("getting DB: %v", err)
	}
	projectRepo := sqlite.NewProjectRepo(defaultProject)

	proj, err := projectRepo.GetByID(ctx, DefaultProjectID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if proj.Settings.AutoCompleteParent != nil {
		t.Fatal("expected default project AutoCompleteParent to be nil")
	}

	// Update settings
	proj.Settings = domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	}
	if err := projectRepo.Update(ctx, proj); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Re-read and verify
	proj2, err := projectRepo.GetByID(ctx, DefaultProjectID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if proj2.Settings.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to be non-nil after update")
	}
	if proj2.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", proj2.Settings.AutoCompleteParent.TriggerStatus)
	}
}
```

**Wait** — `testTaskEnv` is in `package service` and returns a `*testEnv` with a `store` field of type `*sqlite.Store`. `store.DB()` returns `*sql.DB`, not an error. Let me fix the test. Actually, looking at the existing test pattern more carefully, the test should be written differently. Let me create a dedicated test file instead.

Create `internal/sqlite/project_settings_test.go`:

```go
package sqlite

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

func TestProjectRepo_SettingsRoundTrip(t *testing.T) {
	store, err := New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := NewProjectRepo(store.DB())

	// The seeded _default project should have empty settings
	defaultID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	proj, err := repo.GetByID(ctx, defaultID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if proj.Settings.AutoCompleteParent != nil {
		t.Fatal("expected default project AutoCompleteParent to be nil")
	}
	if proj.Settings.AutoRevertParent != nil {
		t.Fatal("expected default project AutoRevertParent to be nil")
	}

	// Update with auto-complete settings
	proj.Settings = domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	}
	if err := repo.Update(ctx, proj); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Re-read and verify
	proj2, err := repo.GetByID(ctx, defaultID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if proj2.Settings.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to be non-nil")
	}
	if proj2.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("expected trigger_status 'completed', got %q", proj2.Settings.AutoCompleteParent.TriggerStatus)
	}
	if proj2.Settings.AutoCompleteParent.TargetStatus != "completed" {
		t.Fatalf("expected target_status 'completed', got %q", proj2.Settings.AutoCompleteParent.TargetStatus)
	}
	if proj2.Settings.AutoRevertParent != nil {
		t.Fatal("expected AutoRevertParent to still be nil")
	}

	// Update with both settings
	proj2.Settings.AutoRevertParent = &domain.AutoRevertConfig{
		TriggerStatus: "completed",
		TargetStatus:  "active",
	}
	if err := repo.Update(ctx, proj2); err != nil {
		t.Fatalf("Update with both: %v", err)
	}

	proj3, err := repo.GetByID(ctx, defaultID)
	if err != nil {
		t.Fatalf("GetByID after second update: %v", err)
	}
	if proj3.Settings.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent to persist")
	}
	if proj3.Settings.AutoRevertParent == nil {
		t.Fatal("expected AutoRevertParent to be non-nil")
	}
	if proj3.Settings.AutoRevertParent.TargetStatus != "active" {
		t.Fatalf("expected revert target_status 'active', got %q", proj3.Settings.AutoRevertParent.TargetStatus)
	}
}

func TestProjectRepo_SettingsList(t *testing.T) {
	store, err := New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := NewProjectRepo(store.DB())

	// List should include settings
	projects, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("expected at least one project")
	}
	// Default project should have empty settings
	for _, p := range projects {
		if p.Settings.AutoCompleteParent != nil {
			t.Fatalf("project %q: expected nil AutoCompleteParent", p.Name)
		}
	}
}

func TestProjectRepo_SettingsCreate(t *testing.T) {
	store, err := New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := NewProjectRepo(store.DB())

	// Create a project with settings
	proj := &domain.Project{
		ID:              uuid.New(),
		Name:            "test-project",
		DefaultWorkflow: "default",
		Settings: domain.ProjectSettings{
			AutoCompleteParent: &domain.AutoCompleteConfig{
				TriggerStatus: "done",
				TargetStatus:  "done",
			},
		},
		CreatedAt: domain.TimeNowUTC(),
	}
	if err := repo.Create(ctx, proj); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, proj.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Settings.AutoCompleteParent == nil {
		t.Fatal("expected AutoCompleteParent on created project")
	}
	if got.Settings.AutoCompleteParent.TriggerStatus != "done" {
		t.Fatalf("expected trigger_status 'done', got %q", got.Settings.AutoCompleteParent.TriggerStatus)
	}
}
```

**Note:** The test uses `domain.TimeNowUTC()`. This may not exist — check if there's a helper. If not, use `time.Now().UTC().Truncate(time.Millisecond)` instead. Replace `domain.TimeNowUTC()` with:

```go
CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
```

And add `"time"` to the imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/sqlite -run TestProjectRepo_Settings`
Expected: FAIL — `scanProject` doesn't scan the `settings` column, `Create` doesn't write it, etc.

- [ ] **Step 3: Update ProjectRepo to handle settings**

Modify `internal/sqlite/project.go`. The changes are:

**3a. Update `Create` to write settings:**

Replace the `Create` method body. The INSERT now includes a 6th column `settings`:

```go
func (r *ProjectRepo) Create(ctx context.Context, project *domain.Project) error {
	settingsJSON, err := json.Marshal(project.Settings)
	if err != nil {
		return fmt.Errorf("marshaling project settings: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, description, default_workflow, settings, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		project.ID.String(), project.Name, project.Description,
		project.DefaultWorkflow, string(settingsJSON),
		project.CreatedAt.UTC().Format(timeFormat),
	)
	return err
}
```

Add `"encoding/json"` and `"fmt"` to the imports at the top of the file (if `"fmt"` is not already there).

**3b. Update `GetByID` and `GetByName` SELECT queries to include `settings`:**

```go
func (r *ProjectRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	return r.scanOne(r.db.QueryRowContext(ctx,
		`SELECT id, name, description, default_workflow, settings, created_at
		 FROM projects WHERE id = ?`, id.String()))
}

func (r *ProjectRepo) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	return r.scanOne(r.db.QueryRowContext(ctx,
		`SELECT id, name, description, default_workflow, settings, created_at
		 FROM projects WHERE name = ?`, name))
}
```

**3c. Update `List` SELECT query:**

```go
func (r *ProjectRepo) List(ctx context.Context) ([]*domain.Project, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, description, default_workflow, settings, created_at FROM projects`)
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
```

**3d. Update `Update` to write settings:**

```go
func (r *ProjectRepo) Update(ctx context.Context, project *domain.Project) error {
	settingsJSON, err := json.Marshal(project.Settings)
	if err != nil {
		return fmt.Errorf("marshaling project settings: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE projects SET name = ?, description = ?, default_workflow = ?, settings = ?
		 WHERE id = ?`,
		project.Name, project.Description, project.DefaultWorkflow,
		string(settingsJSON), project.ID.String(),
	)
	return err
}
```

**3e. Update `scanProject` to read the settings column:**

The function currently scans 5 columns. Add `settings` as the 5th column (between `default_workflow` and `created_at`):

```go
func scanProject(s projectScanner) (*domain.Project, error) {
	var (
		p            domain.Project
		id           string
		settingsJSON string
		createdAt    string
	)
	err := s.Scan(&id, &p.Name, &p.Description, &p.DefaultWorkflow, &settingsJSON, &createdAt)
	if err != nil {
		return nil, err
	}
	p.ID, err = uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(settingsJSON), &p.Settings); err != nil {
		return nil, fmt.Errorf("parsing project settings: %w", err)
	}
	p.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/sqlite -run TestProjectRepo_Settings`
Expected: PASS — all three test cases pass.

- [ ] **Step 5: Run the full test suite to make sure nothing broke**

Run: `make test`
Expected: PASS — all existing tests still pass. The `settings` column defaults to `'{}'` which deserializes to an empty `ProjectSettings`.

- [ ] **Step 6: Commit**

```bash
git add internal/sqlite/project.go internal/sqlite/project_settings_test.go
git commit -m "feat(sqlite): read/write project settings JSON column"
```

---

### Task 4: Verify Full Suite & Integration

**Files:**
- No new files — verification only

- [ ] **Step 1: Run the full test suite with race detector**

Run: `make test-race`
Expected: PASS — no race conditions, all tests green.

- [ ] **Step 2: Run vet and lint**

Run: `make vet && make lint`
Expected: PASS — no issues.

- [ ] **Step 3: Run E2E tests specifically**

Run: `make test-e2e`
Expected: PASS — existing E2E tests still work because the migration applies automatically and the default settings value (`'{}'`) is backward-compatible.
