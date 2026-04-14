# Phase 1 — Workflows Schema & SQLite Repo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `workflows` table to the workspace SQLite schema, extend `domain.Workflow` with persistent-entity fields, and ship a full-CRUD `sqlite.WorkflowRepo` with optimistic locking — without changing any service-layer wiring.

**Architecture:** The in-memory `inmem.WorkflowRepository` remains the live backing store for `WorkflowService`. This phase lands the schema, the extended domain type, and a parallel SQLite implementation that is not yet wired into services. Migration `003_workflows.up.sql` creates the table and seeds the builtin `kanban` workflow with UUID `00000000-0000-0000-0000-000000000000`.

**Tech Stack:** Go, SQLite (via `modernc.org/sqlite`), `github.com/google/uuid`, embedded SQL migrations (`migrations/*.sql`).

## Prerequisites

None beyond `main` at the drafting commit (`f50bb24`). This is the first phase of the initiative.

---

## Task 1: Extend `domain.Workflow` with Persistent-Entity Fields

Add the identity, version, and timestamp fields every persisted entity in Tusk carries. The existing config-driven consumer (`inmem.WorkflowRepository`) gets its builder updated to populate the new fields so compilation and behavior hold.

**Files:**
- Modify: `domain/workflow.go` — extend `Workflow` struct
- Modify: `inmem/workflow.go:37-64` — builder function `buildWorkflowMap`
- Modify: `inmem/workflow.go:87-100` — `copyWorkflow` defensive copy
- Modify: `inmem/workflow_test.go` — update assertions that compare whole `Workflow` values
- Modify: `service/workflow_test.go` — same update if any test compares `Workflow` structurally

- [ ] **Step 1: Extend the domain type**

Edit `domain/workflow.go`. Locate the existing `Workflow` struct (currently `Name`, `Statuses`, `Transitions`). Replace with:

```go
// Workflow is a named set of statuses and allowed transitions.
// Workflows are persisted in the workspace database and carry the same
// version + audit fields as every other mutable entity.
type Workflow struct {
	ID          uuid.UUID
	Name        string
	Statuses    map[string]StatusConfig
	Transitions []WorkflowTransition
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
```

Add imports at the top of the file:

```go
import (
	"sort"
	"time"

	"github.com/google/uuid"
)
```

- [ ] **Step 2: Update `inmem.WorkflowRepository` builder**

Edit `inmem/workflow.go`. Replace the body of `buildWorkflowMap` so each synthesized `*domain.Workflow` is populated with `ID`, `Version`, and timestamps:

```go
func buildWorkflowMap(cfgWorkflows map[string]config.WorkflowConfig) map[string]*domain.Workflow {
	now := time.Now().UTC().Truncate(time.Millisecond)
	workflows := make(map[string]*domain.Workflow, len(cfgWorkflows))
	for name, cfg := range cfgWorkflows {
		wf := &domain.Workflow{
			ID:          uuid.NewSHA1(uuid.Nil, []byte("workflow:"+name)),
			Name:        name,
			Statuses:    make(map[string]domain.StatusConfig, len(cfg.Statuses)),
			Transitions: make([]domain.WorkflowTransition, len(cfg.Transitions)),
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		for statusName, sc := range cfg.Statuses {
			roles := make([]domain.StatusRole, len(sc.Roles))
			for i, r := range sc.Roles {
				roles[i] = domain.StatusRole(r)
			}
			wf.Statuses[statusName] = domain.StatusConfig{Roles: roles}
		}
		for i, t := range cfg.Transitions {
			wf.Transitions[i] = domain.WorkflowTransition{
				FromStatus: t.From,
				ToStatus:   t.To,
			}
		}
		workflows[name] = wf
	}
	return workflows
}
```

Add the two new imports at the top of `inmem/workflow.go`:

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

Update `copyWorkflow` to also copy the new scalar fields — since they are value types (`uuid.UUID`, `int`, `time.Time`), the existing `cp := &domain.Workflow{...}` literal needs them added:

```go
func copyWorkflow(wf *domain.Workflow) *domain.Workflow {
	cp := &domain.Workflow{
		ID:          wf.ID,
		Name:        wf.Name,
		Statuses:    make(map[string]domain.StatusConfig, len(wf.Statuses)),
		Transitions: make([]domain.WorkflowTransition, len(wf.Transitions)),
		Version:     wf.Version,
		CreatedAt:   wf.CreatedAt,
		UpdatedAt:   wf.UpdatedAt,
	}
	for name, sc := range wf.Statuses {
		roles := make([]domain.StatusRole, len(sc.Roles))
		copy(roles, sc.Roles)
		cp.Statuses[name] = domain.StatusConfig{Roles: roles}
	}
	copy(cp.Transitions, wf.Transitions)
	return cp
}
```

- [ ] **Step 3: Compile the world**

Run: `make build`
Expected: success, no errors.

If any call site constructs `domain.Workflow{...}` as a struct literal with positional arguments it will now fail — search and switch to named fields:

Run: `grep -rn "domain.Workflow{" --include='*.go' .`
Expected: every hit uses named fields (`Name: "..."`, etc.). Fix any positional literals before continuing.

- [ ] **Step 4: Run existing tests**

Run: `go test ./domain/... ./inmem/... ./service/...`
Expected: PASS. Any test that pattern-matches on `Workflow` struct literals with positional fields must be updated to named fields. No semantic assertions should need updating because every new field is zero-value or builder-synthesized; existing assertions on `Name`, `Statuses`, `Transitions` still hold.

- [ ] **Step 5: Commit**

```bash
git add domain/workflow.go inmem/workflow.go
git commit -m "feat(domain): add id/version/timestamps to Workflow entity"
```

---

## Task 2: Migration `003_workflows` — Table + Kanban Seed

Create the SQL schema for workflows and seed the builtin `kanban` row. The migration runner (`sqlite.Store.migrate`) picks up new files by name order and runs each exactly once — no code changes to the runner are required.

**Files:**
- Create: `migrations/003_workflows.up.sql`
- Create: `migrations/003_workflows.down.sql`

- [ ] **Step 1: Write the up-migration**

Create `migrations/003_workflows.up.sql` with:

```sql
CREATE TABLE workflows (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    statuses TEXT NOT NULL,
    transitions TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_workflows_name ON workflows(name);

INSERT INTO workflows (id, name, statuses, transitions) VALUES (
    '00000000-0000-0000-0000-000000000000',
    'kanban',
    '{"pending":["initial"],"active":["start","highlight"],"completed":["terminal","done","dim"],"deleted":["terminal","delete","dim"]}',
    '[["pending","active"],["active","pending"],["active","completed"],["completed","pending"],["pending","deleted"],["active","deleted"]]'
);
```

- [ ] **Step 2: Write the down-migration**

Create `migrations/003_workflows.down.sql` with:

```sql
DROP INDEX IF EXISTS idx_workflows_name;
DROP TABLE IF EXISTS workflows;
```

- [ ] **Step 3: Verify the migration applies cleanly**

Run: `go test ./sqlite/... -run TestStore -v`
Expected: PASS. The existing store smoke tests open a fresh `:memory:` (or tempdir) DB and run all embedded migrations; if the SQL is malformed the test fails here.

If no `TestStore`-named test exists, fall back to:

```bash
go test ./sqlite/... -run TestPlayerRepo -v
```

`newTestPlayerRepo` opens `sqlite.New(tempdir, migrations.FS)` which runs every migration. A passing player-repo test confirms the new migration parses and applies.

- [ ] **Step 4: Sanity-check the seed row is present**

Add a new test file `sqlite/workflow_seed_test.go`:

```go
package sqlite_test

import (
	"testing"

	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
)

func TestMigration003_SeedsKanbanWorkflow(t *testing.T) {
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	defer store.Close()

	var name string
	err = store.DB().QueryRow(
		`SELECT name FROM workflows WHERE id = '00000000-0000-0000-0000-000000000000'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("querying seed row: %v", err)
	}
	if name != "kanban" {
		t.Errorf("got name %q, want %q", name, "kanban")
	}
}
```

Run: `go test ./sqlite/... -run TestMigration003_SeedsKanbanWorkflow -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add migrations/003_workflows.up.sql migrations/003_workflows.down.sql sqlite/workflow_seed_test.go
git commit -m "feat(sqlite): add workflows table and seed kanban workflow"
```

---

## Task 3: `sqlite.WorkflowRepo` Read Operations (`Create`, `GetByID`, `GetByName`, `List`)

Add a new SQLite-backed repository with read + insert methods. It is not yet wired into the service DI graph — this phase only ships the storage substrate. Follow the conventions established by `sqlite.PlayerRepo` and `sqlite.TaskRepo`.

**Files:**
- Create: `sqlite/workflow.go`
- Create: `sqlite/workflow_test.go`

- [ ] **Step 1: Write the failing test for `Create` + `GetByID`**

Create `sqlite/workflow_test.go` with:

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

func newTestWorkflowRepo(t *testing.T) *sqlite.WorkflowRepo {
	t.Helper()
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return sqlite.NewWorkflowRepo(store.DB())
}

func sampleWorkflow(name string) *domain.Workflow {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return &domain.Workflow{
		ID:   uuid.New(),
		Name: name,
		Statuses: map[string]domain.StatusConfig{
			"pending": {Roles: []domain.StatusRole{domain.RoleInitial}},
			"active":  {Roles: []domain.StatusRole{domain.RoleStart}},
			"done":    {Roles: []domain.StatusRole{domain.RoleTerminal, domain.RoleDone}},
		},
		Transitions: []domain.WorkflowTransition{
			{FromStatus: "pending", ToStatus: "active"},
			{FromStatus: "active", ToStatus: "done"},
		},
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestWorkflowRepo_CreateAndGetByID(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, wf.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "sprint" {
		t.Errorf("got name %q, want %q", got.Name, "sprint")
	}
	if len(got.Statuses) != 3 {
		t.Errorf("got %d statuses, want 3", len(got.Statuses))
	}
	if len(got.Transitions) != 2 {
		t.Errorf("got %d transitions, want 2", len(got.Transitions))
	}
	if got.Version != 1 {
		t.Errorf("got version %d, want 1", got.Version)
	}
}

func TestWorkflowRepo_GetByID_NotFound(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByID: got %v, want ErrNotFound", err)
	}
}

func TestWorkflowRepo_GetByName_Seed(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	got, err := repo.GetByName(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.ID != uuid.Nil {
		t.Errorf("got ID %v, want uuid.Nil", got.ID)
	}
	if _, ok := got.Statuses["pending"]; !ok {
		t.Errorf("expected pending status in seed workflow")
	}
}

func TestWorkflowRepo_List_ContainsSeed(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	wfs, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(wfs) < 1 {
		t.Fatalf("List: want >=1 workflow, got %d", len(wfs))
	}
	found := false
	for _, w := range wfs {
		if w.Name == "kanban" {
			found = true
		}
	}
	if !found {
		t.Errorf("kanban seed not in list")
	}
}

func TestWorkflowRepo_CreateDuplicate(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	// Same name, different UUID — UNIQUE(name) should trip.
	wf2 := sampleWorkflow("sprint")
	err := repo.Create(ctx, wf2)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second Create: got %v, want ErrConflict", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./sqlite/... -run TestWorkflowRepo -v`
Expected: FAIL with "undefined: sqlite.WorkflowRepo" (or "undefined: sqlite.NewWorkflowRepo").

- [ ] **Step 3: Implement `sqlite.WorkflowRepo` read path**

Create `sqlite/workflow.go` with:

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

const workflowColumns = `id, name, statuses, transitions, version, created_at, updated_at`

// WorkflowRepo implements repository.WorkflowRepository using SQLite.
type WorkflowRepo struct {
	db DBTX
}

// NewWorkflowRepo creates a WorkflowRepo.
func NewWorkflowRepo(db DBTX) *WorkflowRepo {
	return &WorkflowRepo{db: db}
}

// Create inserts a new workflow. Returns domain.ErrConflict on unique-name collision.
func (r *WorkflowRepo) Create(ctx context.Context, wf *domain.Workflow) error {
	statusesJSON, err := encodeStatuses(wf.Statuses)
	if err != nil {
		return err
	}
	transitionsJSON, err := encodeTransitions(wf.Transitions)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO workflows (%s) VALUES (?, ?, ?, ?, ?, ?, ?)`, workflowColumns),
		wf.ID.String(), wf.Name, statusesJSON, transitionsJSON, wf.Version,
		wf.CreatedAt.UTC().Format(timeFormat),
		wf.UpdatedAt.UTC().Format(timeFormat),
	)
	if err != nil {
		if _, lookupErr := r.GetByName(ctx, wf.Name); lookupErr == nil {
			return domain.ErrConflict
		}
		return err
	}
	return nil
}

// GetByID retrieves a workflow by UUID. Returns domain.ErrNotFound if missing.
func (r *WorkflowRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Workflow, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM workflows WHERE id = ?`, workflowColumns),
		id.String())
	return scanWorkflow(row)
}

// GetByName retrieves a workflow by name. Returns domain.ErrNotFound if missing.
func (r *WorkflowRepo) GetByName(ctx context.Context, name string) (*domain.Workflow, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM workflows WHERE name = ?`, workflowColumns),
		name)
	return scanWorkflow(row)
}

// List returns all workflows ordered by name.
func (r *WorkflowRepo) List(ctx context.Context) ([]*domain.Workflow, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM workflows ORDER BY name`, workflowColumns))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*domain.Workflow, 0)
	for rows.Next() {
		wf, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, wf)
	}
	return result, rows.Err()
}

type workflowScanner interface {
	Scan(dest ...any) error
}

func scanWorkflow(s workflowScanner) (*domain.Workflow, error) {
	var (
		wf              domain.Workflow
		idStr           string
		statusesJSON    string
		transitionsJSON string
		createdAt       string
		updatedAt       string
	)
	err := s.Scan(&idStr, &wf.Name, &statusesJSON, &transitionsJSON, &wf.Version, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	wf.ID, err = uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parsing workflow id: %w", err)
	}
	wf.Statuses, err = decodeStatuses([]byte(statusesJSON))
	if err != nil {
		return nil, fmt.Errorf("decoding statuses: %w", err)
	}
	wf.Transitions, err = decodeTransitions([]byte(transitionsJSON))
	if err != nil {
		return nil, fmt.Errorf("decoding transitions: %w", err)
	}
	wf.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, err
	}
	wf.UpdatedAt, err = time.Parse(timeFormat, updatedAt)
	if err != nil {
		return nil, err
	}
	return &wf, nil
}

func encodeStatuses(m map[string]domain.StatusConfig) (string, error) {
	out := make(map[string][]domain.StatusRole, len(m))
	for k, v := range m {
		roles := v.Roles
		if roles == nil {
			roles = []domain.StatusRole{}
		}
		out[k] = roles
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeStatuses(b []byte) (map[string]domain.StatusConfig, error) {
	var in map[string][]domain.StatusRole
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	out := make(map[string]domain.StatusConfig, len(in))
	for k, v := range in {
		out[k] = domain.StatusConfig{Roles: v}
	}
	return out, nil
}

func encodeTransitions(ts []domain.WorkflowTransition) (string, error) {
	out := make([][2]string, len(ts))
	for i, t := range ts {
		out[i] = [2]string{t.FromStatus, t.ToStatus}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeTransitions(b []byte) ([]domain.WorkflowTransition, error) {
	var in [][2]string
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	out := make([]domain.WorkflowTransition, len(in))
	for i, pair := range in {
		out[i] = domain.WorkflowTransition{FromStatus: pair[0], ToStatus: pair[1]}
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./sqlite/... -run TestWorkflowRepo -v`
Expected: PASS on `TestWorkflowRepo_CreateAndGetByID`, `TestWorkflowRepo_GetByID_NotFound`, `TestWorkflowRepo_GetByName_Seed`, `TestWorkflowRepo_List_ContainsSeed`, `TestWorkflowRepo_CreateDuplicate`.

- [ ] **Step 5: Commit**

```bash
git add sqlite/workflow.go sqlite/workflow_test.go
git commit -m "feat(sqlite): implement WorkflowRepo read operations"
```

---

## Task 4: `sqlite.WorkflowRepo.Update` with Optimistic Locking

Add the `Update` method following the `TaskRepo.Update` pattern: `version = version + 1 WHERE id = ? AND version = ?`, returning `ErrConflict` on mismatch and `ErrNotFound` when the row does not exist.

**Files:**
- Modify: `sqlite/workflow.go` — add `Update` method
- Modify: `sqlite/workflow_test.go` — add update tests

- [ ] **Step 1: Write the failing update tests**

Append to `sqlite/workflow_test.go`:

```go
func TestWorkflowRepo_Update_IncrementsVersion(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}

	wf.Statuses["review"] = domain.StatusConfig{Roles: []domain.StatusRole{domain.RoleHighlight}}
	if err := repo.Update(ctx, wf); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if wf.Version != 2 {
		t.Errorf("local version after Update: got %d, want 2", wf.Version)
	}

	got, err := repo.GetByID(ctx, wf.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("stored version: got %d, want 2", got.Version)
	}
	if _, ok := got.Statuses["review"]; !ok {
		t.Errorf("expected review status after update")
	}
}

func TestWorkflowRepo_Update_StaleVersion(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Make a stale copy before Update increments wf.Version.
	stale := *wf
	if err := repo.Update(ctx, wf); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	err := repo.Update(ctx, &stale)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale Update: got %v, want ErrConflict", err)
	}
}

func TestWorkflowRepo_Update_NotFound(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ghost := sampleWorkflow("ghost")
	err := repo.Update(context.Background(), ghost)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update missing row: got %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./sqlite/... -run 'TestWorkflowRepo_Update' -v`
Expected: FAIL with "undefined: (*sqlite.WorkflowRepo).Update".

- [ ] **Step 3: Implement `Update`**

Add to `sqlite/workflow.go`:

```go
// Update persists changes to a workflow with optimistic locking.
// Returns domain.ErrConflict if the stored version has advanced,
// and domain.ErrNotFound if the row does not exist.
func (r *WorkflowRepo) Update(ctx context.Context, wf *domain.Workflow) error {
	statusesJSON, err := encodeStatuses(wf.Statuses)
	if err != nil {
		return err
	}
	transitionsJSON, err := encodeTransitions(wf.Transitions)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	nowStr := now.Format(timeFormat)
	res, err := r.db.ExecContext(ctx, `
		UPDATE workflows SET
			name = ?, statuses = ?, transitions = ?,
			version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`,
		wf.Name, statusesJSON, transitionsJSON, nowStr,
		wf.ID.String(), wf.Version,
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
			`SELECT 1 FROM workflows WHERE id = ?`, wf.ID.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	wf.Version++
	wf.UpdatedAt = now
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./sqlite/... -run 'TestWorkflowRepo_Update' -v`
Expected: PASS on all three update tests.

- [ ] **Step 5: Commit**

```bash
git add sqlite/workflow.go sqlite/workflow_test.go
git commit -m "feat(sqlite): implement WorkflowRepo.Update with optimistic locking"
```

---

## Task 5: `sqlite.WorkflowRepo.Delete` with Optimistic Locking

Version-checked delete, matching the MCP/CLI contract that every mutable entity accepts a `version`.

**Files:**
- Modify: `sqlite/workflow.go` — add `Delete` method
- Modify: `sqlite/workflow_test.go` — add delete tests

- [ ] **Step 1: Write the failing delete tests**

Append to `sqlite/workflow_test.go`:

```go
func TestWorkflowRepo_Delete(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, wf.ID, wf.Version); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := repo.GetByID(ctx, wf.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("after Delete, GetByID: got %v, want ErrNotFound", err)
	}
}

func TestWorkflowRepo_Delete_StaleVersion(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	ctx := context.Background()

	wf := sampleWorkflow("sprint")
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Update(ctx, wf); err != nil {
		t.Fatalf("Update: %v", err)
	}
	// Version is now 2, caller still holds 1.
	err := repo.Delete(ctx, wf.ID, 1)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale Delete: got %v, want ErrConflict", err)
	}
}

func TestWorkflowRepo_Delete_NotFound(t *testing.T) {
	repo := newTestWorkflowRepo(t)
	err := repo.Delete(context.Background(), uuid.New(), 1)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete missing: got %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./sqlite/... -run 'TestWorkflowRepo_Delete' -v`
Expected: FAIL with "undefined: (*sqlite.WorkflowRepo).Delete".

- [ ] **Step 3: Implement `Delete`**

Add to `sqlite/workflow.go`:

```go
// Delete removes a workflow with optimistic locking on version.
// Returns domain.ErrConflict on version mismatch, domain.ErrNotFound if missing.
func (r *WorkflowRepo) Delete(ctx context.Context, id uuid.UUID, version int) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM workflows WHERE id = ? AND version = ?`,
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
			`SELECT 1 FROM workflows WHERE id = ?`, id.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return domain.ErrConflict
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./sqlite/... -run 'TestWorkflowRepo_Delete' -v`
Expected: PASS on all three delete tests.

- [ ] **Step 5: Run the full test suite to confirm nothing else regressed**

Run: `make test`
Expected: PASS.

Run: `make test-race`
Expected: PASS.

Run: `make vet`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add sqlite/workflow.go sqlite/workflow_test.go
git commit -m "feat(sqlite): implement WorkflowRepo.Delete with optimistic locking"
```

---

## Acceptance Criteria — User-Visible Behavior Still Works

At the end of this phase, every one of these must still hold:

- `make build`, `make test`, `make test-race`, `make vet`: clean.
- `tusk workflow list` shows the same output it did before this phase (still powered by `inmem.WorkflowRepository` reading from config).
- `tusk workflow info kanban` shows the kanban workflow identically.
- `tusk workflow create`, `modify`, `delete` still work through the existing config-file pathway.
- `tusk task create`, `start`, `done`, `delete` still transition statuses through the config-driven workflow unchanged.
- All E2E tests in `tests/e2e/` pass without modification.

## Changes Introduced

**New files:**
- `migrations/003_workflows.up.sql`
- `migrations/003_workflows.down.sql`
- `sqlite/workflow.go`
- `sqlite/workflow_test.go`
- `sqlite/workflow_seed_test.go`

**Modified interfaces / types:**
- `domain.Workflow` gains `ID uuid.UUID`, `Version int`, `CreatedAt time.Time`, `UpdatedAt time.Time`.
- `inmem.WorkflowRepository` builder now synthesizes these fields deterministically from the workflow name.

**Schema migrations:**
- `003_workflows` — creates `workflows` table and seeds `kanban` row with UUID `00000000-0000-0000-0000-000000000000`.

**Dependencies:**
- No new Go module dependencies.

**Bridge code:**
- None. `sqlite.WorkflowRepo` is usable immediately but is not yet wired into `WorkflowService`. That wiring lives in the next initiative ("Service Layer Migration") and is explicitly out of scope for this plan.
