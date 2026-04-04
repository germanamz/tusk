# Project Management Phase 1: Migration + Domain + SQLite

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `version` column to the `projects` table, update the domain type, and implement optimistic locking in the SQLite repository.

**Architecture:** New SQL migration adds the column with a default value of 1. The `domain.Project` struct gains a `Version int` field. `ProjectRepo` changes: `Create` inserts version, `Update` uses `WHERE version = ?` and returns `ErrConflict` on stale writes, all `scan*` functions read the new column.

**Tech Stack:** Go, SQLite, database/sql

---

### Task 1: Add SQL Migration Files

**Files:**
- Create: `migrations/003_project_version.up.sql`
- Create: `migrations/003_project_version.down.sql`

- [ ] **Step 1: Create the up migration**

Create `migrations/003_project_version.up.sql`:

```sql
ALTER TABLE projects ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
```

- [ ] **Step 2: Create the down migration**

Create `migrations/003_project_version.down.sql`:

```sql
ALTER TABLE projects DROP COLUMN version;
```

- [ ] **Step 3: Verify migrations are embedded**

The migration files are automatically embedded via `migrations/embed.go` which uses `//go:embed *.sql`. Verify by running:

```bash
go build ./migrations/...
```

Expected: No errors. The `embed.go` file uses `//go:embed *.sql` which picks up all `.sql` files in the directory.

- [ ] **Step 4: Commit**

```bash
git add migrations/003_project_version.up.sql migrations/003_project_version.down.sql
git commit -m "feat: add version column to projects table (migration 003)"
```

---

### Task 2: Update Domain Type

**Files:**
- Modify: `internal/domain/project.go`

- [ ] **Step 1: Add Version field to Project struct**

In `internal/domain/project.go`, add a `Version` field to the `Project` struct. The struct currently looks like:

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

Add `Version int` after `Settings`:

```go
type Project struct {
	ID              uuid.UUID
	Name            string
	Description     string
	DefaultWorkflow string
	Settings        ProjectSettings
	Version         int
	CreatedAt       time.Time
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/domain/...
```

Expected: Success. No other code references `Version` yet so nothing breaks.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/project.go
git commit -m "feat: add Version field to domain.Project"
```

---

### Task 3: Update SQLite ProjectRepo for Version Support

**Files:**
- Modify: `internal/sqlite/project.go`

This task modifies four parts of the file: `Create`, `Update`, `scanProject`, and the SQL queries in `GetByID`, `GetByName`, and `List`.

- [ ] **Step 1: Write the failing test for optimistic locking**

Add two tests to `internal/sqlite/project_test.go`:

```go
// TestProjectUpdateVersion verifies that Update increments the version field
// and that the new version is persisted.
func TestProjectUpdateVersion(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID:              uuid.New(),
		Name:            "versioned",
		Description:     "Test versioning",
		DefaultWorkflow: "default",
		Version:         1,
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read back to confirm version is 1
	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Version != 1 {
		t.Fatalf("expected version 1, got %d", got.Version)
	}

	// Update should increment version to 2
	got.Description = "Updated"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got2, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if got2.Version != 2 {
		t.Fatalf("expected version 2, got %d", got2.Version)
	}
	if got2.Description != "Updated" {
		t.Fatalf("expected description 'Updated', got %q", got2.Description)
	}
}

// TestProjectUpdateConflict verifies that updating with a stale version
// returns domain.ErrConflict.
func TestProjectUpdateConflict(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID:              uuid.New(),
		Name:            "conflict-test",
		Description:     "Original",
		DefaultWorkflow: "default",
		Version:         1,
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Simulate a stale read: read the project, then update it once
	// so the DB version is now 2, but our copy still says 1.
	stale, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	// First update succeeds (version 1 -> 2)
	fresh, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	fresh.Description = "Fresh update"
	if err := repo.Update(ctx, fresh); err != nil {
		t.Fatalf("Update (fresh): %v", err)
	}

	// Stale update should fail with ErrConflict (version is still 1, DB is 2)
	stale.Description = "Stale update"
	err = repo.Update(ctx, stale)
	if err != domain.ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Verify the fresh update persisted, not the stale one
	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID after conflict: %v", err)
	}
	if got.Description != "Fresh update" {
		t.Fatalf("expected 'Fresh update', got %q", got.Description)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test -v ./internal/sqlite -run "TestProjectUpdateVersion|TestProjectUpdateConflict"
```

Expected: FAIL — the `scanProject` function scans 6 columns but the DB now has 7 (including `version`), so all project queries will fail.

- [ ] **Step 3: Update SELECT queries to include version column**

In `internal/sqlite/project.go`, update the SELECT column lists in three places.

**GetByID** (around line 60-62) — change:

```go
return r.scanOne(r.db.QueryRowContext(ctx,
	`SELECT id, name, description, default_workflow, settings, created_at
	 FROM projects WHERE id = ?`, id.String()))
```

to:

```go
return r.scanOne(r.db.QueryRowContext(ctx,
	`SELECT id, name, description, default_workflow, settings, version, created_at
	 FROM projects WHERE id = ?`, id.String()))
```

**GetByName** (around line 67-70) — change:

```go
return r.scanOne(r.db.QueryRowContext(ctx,
	`SELECT id, name, description, default_workflow, settings, created_at
	 FROM projects WHERE name = ?`, name))
```

to:

```go
return r.scanOne(r.db.QueryRowContext(ctx,
	`SELECT id, name, description, default_workflow, settings, version, created_at
	 FROM projects WHERE name = ?`, name))
```

**List** (around line 78-79) — change:

```go
rows, err := r.db.QueryContext(ctx,
	`SELECT id, name, description, default_workflow, settings, created_at FROM projects`)
```

to:

```go
rows, err := r.db.QueryContext(ctx,
	`SELECT id, name, description, default_workflow, settings, version, created_at FROM projects`)
```

- [ ] **Step 4: Update scanProject to read version column**

In `internal/sqlite/project.go`, the `scanProject` function (around line 173-196) currently scans 6 columns. Update it to scan 7 columns including `version`.

Change:

```go
func scanProject(s projectScanner) (*domain.Project, error) {
	var (
		p            domain.Project
		id           string
		settingsJSON string
		createdAt    string
	)
	err := s.Scan(&id, &p.Name, &p.Description, &p.DefaultWorkflow, &settingsJSON, &createdAt)
```

to:

```go
func scanProject(s projectScanner) (*domain.Project, error) {
	var (
		p            domain.Project
		id           string
		settingsJSON string
		createdAt    string
	)
	err := s.Scan(&id, &p.Name, &p.Description, &p.DefaultWorkflow, &settingsJSON, &p.Version, &createdAt)
```

The only change is adding `&p.Version` between `&settingsJSON` and `&createdAt`, matching the column order in the SELECT queries.

- [ ] **Step 5: Update Create to insert version**

In `internal/sqlite/project.go`, the `Create` method (around line 42-55) currently inserts 6 columns. Add `version`.

Change:

```go
_, err = r.db.ExecContext(ctx,
	`INSERT INTO projects (id, name, description, default_workflow, settings, created_at)
	 VALUES (?, ?, ?, ?, ?, ?)`,
	project.ID.String(), project.Name, project.Description,
	project.DefaultWorkflow, string(settingsJSON),
	project.CreatedAt.UTC().Format(timeFormat),
)
```

to:

```go
_, err = r.db.ExecContext(ctx,
	`INSERT INTO projects (id, name, description, default_workflow, settings, version, created_at)
	 VALUES (?, ?, ?, ?, ?, ?, ?)`,
	project.ID.String(), project.Name, project.Description,
	project.DefaultWorkflow, string(settingsJSON), project.Version,
	project.CreatedAt.UTC().Format(timeFormat),
)
```

- [ ] **Step 6: Update Update method for optimistic locking**

In `internal/sqlite/project.go`, replace the `Update` method (around line 102-114). The current implementation does not check `RowsAffected`.

Change:

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

to:

```go
func (r *ProjectRepo) Update(ctx context.Context, project *domain.Project) error {
	settingsJSON, err := json.Marshal(project.Settings)
	if err != nil {
		return fmt.Errorf("marshaling project settings: %w", err)
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE projects SET name = ?, description = ?, default_workflow = ?, settings = ?, version = version + 1
		 WHERE id = ? AND version = ?`,
		project.Name, project.Description, project.DefaultWorkflow,
		string(settingsJSON), project.ID.String(), project.Version,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrConflict
	}
	return nil
}
```

- [ ] **Step 7: Update the Update method's doc comment**

Replace the old doc comment (around line 96-101):

```go
// Update modifies an existing project's name, description, and default_workflow.
// The project is identified by its ID. CreatedAt is NOT updated (it is immutable).
//
// Note: this method does NOT check RowsAffected. If the ID does not exist,
// the UPDATE silently affects 0 rows. This is intentional — Update is typically
// called right after GetByID, so the project is known to exist.
```

with:

```go
// Update modifies an existing project's mutable fields (name, description,
// default_workflow, settings). It uses optimistic locking: the UPDATE includes
// WHERE version = ? and increments the version on success. If the version
// does not match (concurrent modification), it returns domain.ErrConflict.
```

- [ ] **Step 8: Run the new tests**

```bash
go test -v ./internal/sqlite -run "TestProjectUpdateVersion|TestProjectUpdateConflict"
```

Expected: PASS for both tests.

- [ ] **Step 9: Run all existing project tests to check for regressions**

```bash
go test -v ./internal/sqlite -run "TestProject"
```

Expected: All tests pass. The existing tests (`TestProjectCreate`, `TestProjectUpdate`, `TestProjectList`, etc.) should still work because:
- `Create` now inserts `Version` (existing tests set `Version: 0`, which is fine — the DB default is 1, but our insert uses the struct value)
- Wait — existing tests don't set `Version` on the struct, so it will be 0. The DB column has `DEFAULT 1`, but our `Create` explicitly inserts the value. We need to check.

**Important:** If `TestProjectCreate` fails because `Version` is 0 instead of 1, the existing tests need to set `Version: 1` on the Project struct they create. The migration sets `DEFAULT 1` for existing rows, but our `Create` method now explicitly passes the struct's `Version` field. Update any failing test's project creation to include `Version: 1`.

- [ ] **Step 10: Run the full test suite**

```bash
make test
```

Expected: All tests pass.

- [ ] **Step 11: Commit**

```bash
git add internal/domain/project.go internal/sqlite/project.go internal/sqlite/project_test.go
git commit -m "feat: add optimistic locking to ProjectRepo

Add version column to project queries, increment on update,
and return ErrConflict on stale writes."
```

---

### Task 4: Fix Existing Tests for Version Field

**Files:**
- Modify: `internal/sqlite/project_test.go`
- Modify: `internal/sqlite/project_settings_test.go`

This task may be unnecessary if all tests already pass after Task 3. Run the tests first (Step 1) and skip to commit if everything is green.

- [ ] **Step 1: Run all project-related tests**

```bash
go test -v ./internal/sqlite -run "TestProject"
```

If all tests pass, skip to Step 4 (commit). If any tests fail because `Version` is `0` instead of `1`, continue with Step 2.

- [ ] **Step 2: Update project_test.go to set Version: 1**

In `internal/sqlite/project_test.go`, find every `&domain.Project{...}` literal and add `Version: 1`. There are 5 project creation sites:

1. `TestProjectCreate` (around line 39-45):
```go
p := &domain.Project{
	ID:              uuid.New(),
	Name:            "backend",
	Description:     "Backend services",
	DefaultWorkflow: "default",
	Version:         1,
	CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
}
```

2. `TestProjectList` (around line 112-116):
```go
p := &domain.Project{
	ID: uuid.New(), Name: "frontend", Description: "Frontend app",
	DefaultWorkflow: "default",
	Version:         1,
	CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
}
```

3. `TestProjectUpdate` (around line 137-141):
```go
p := &domain.Project{
	ID: uuid.New(), Name: "mobile", Description: "Mobile app",
	DefaultWorkflow: "default",
	Version:         1,
	CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
}
```

4. `TestProjectCreateDuplicate` (around line 179-183 and 187-191):
```go
p1 := &domain.Project{
	ID: uuid.New(), Name: "dupname", Description: "First",
	DefaultWorkflow: "default",
	Version:         1,
	CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
}
// ...
p2 := &domain.Project{
	ID: uuid.New(), Name: "dupname", Description: "Second",
	DefaultWorkflow: "default",
	Version:         1,
	CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
}
```

5. `TestProjectDelete` (around line 204-208):
```go
p := &domain.Project{
	ID: uuid.New(), Name: "temp", Description: "Temporary",
	DefaultWorkflow: "default",
	Version:         1,
	CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
}
```

- [ ] **Step 3: Update project_settings_test.go**

In `internal/sqlite/project_settings_test.go`, the `TestProjectRepo_SettingsCreate` test creates a project (around line 126-137). Add `Version: 1`:

```go
proj := &domain.Project{
	ID:              uuid.New(),
	Name:            "test-project",
	DefaultWorkflow: "default",
	Version:         1,
	Settings: domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "done",
			TargetStatus:  "done",
		},
	},
	CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
}
```

Also, in `TestProjectUpdate` — after calling `repo.Update(ctx, p)`, the in-memory `p.Version` is still 1 but the DB version is now 2. If any subsequent call uses `p` for another `Update`, it will fail with `ErrConflict`. Check if this is the case. If `TestProjectRepo_SettingsRoundTrip` does multiple updates on the same project, each update needs a fresh `GetByID` first (it already does this — `proj2, err := repo.GetByID(...)` before the second update — so it should work).

- [ ] **Step 4: Run all tests**

```bash
make test
```

Expected: All tests pass.

- [ ] **Step 5: Commit (only if changes were needed)**

```bash
git add internal/sqlite/project_test.go internal/sqlite/project_settings_test.go
git commit -m "test: update project tests for version field"
```
