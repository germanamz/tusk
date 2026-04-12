# Phase 3 — Store Registry Plumbing

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Initiative:** Project Management CLI (ROADMAP.md:556-564)
**Phase:** 3 of 4
**Prerequisites:** Phase 1 and Phase 2 complete — `config.ProjectConfig.DBPath` field exists and the `tusk project` CLI commands can write it.

---

## Inherits From

Phase 2 left the repository with:

- `tusk project create/modify/delete` CLI commands that read and write `db_path` on projects in the config file
- A single global `sqlite.Store` opened in `cmd/tusk/main.go:48` and passed to every service constructor
- All service signatures unchanged from the pre-initiative baseline: `NewTaskService(taskRepo, annotationRepo, relationRepo, tagRepo, projectRepo, workflowSvc, store, urgencyEngine, playerRepo)`, `NewRelationService(relationRepo, taskRepo, store)`, `NewTagService(tagRepo)`
- `domain/errors.go` exports `ErrNotFound`, `ErrConflict`, `ErrCyclicBlock`, `ErrInvalidTransition`, `ErrDuplicateRelation`, `ErrNoAvailableTasks` — no `ErrCrossStoreRelation` yet
- `filter` package and `syntax` package unchanged; `syntax.FilterSet` already provides `HasField(key string) bool` and `GetField(key string) *FieldFilter` for inspecting parsed filters

---

## Goal

Land **plumbing only** for per-project databases. Introduce the `sqlite.StoreRegistry` type, a `service.RepoBundle` struct, and a cross-store relation sentinel error. Wire the registry into `cmd/tusk/main.go` behind a **bridge resolver** that always returns the default store regardless of project ID — preserving single-store behavior exactly. Services are **not** refactored in this phase. Phase 4 flips services over to the real resolver.

This phase ships compilable, test-passing code that is a perfect behavioral no-op. Its only user-visible effect is that `tusk` now opens its default store through the registry code path instead of the legacy direct `sqlite.New` call. The default path resolves through a new config-file-relative path helper, which is the one user-visible change — absolute paths and `~`-prefixed paths still work identically, but pre-existing configs never used relative `storage.path` values in practice.

---

## User-Visible Behaviors Preserved

Every command the user could run after Phase 2 must still work identically:

- `tusk add/list/info/modify/start/done/delete/claim/pop/available/next/timer` — unchanged
- `tusk workflow *`, `tusk project *`, `tusk tag *`, `tusk player *`, `tusk config *`, `tusk link/unlink`, `tusk annotate`, `tusk undo` — unchanged
- `tusk mcp serve` — unchanged
- Default database location `~/.local/share/tusk/tusk.db` — unchanged
- `--db` flag / `TUSK_DB` env var — unchanged
- Existing config files with no `db_path` on any project — unchanged behavior
- Existing config files with `db_path` set on some project — **still silently ignored in Phase 3** (Phase 4 honors it)

Acceptance: `make test test-race test-e2e vet lint` all green, and a manual smoke run (`./bin/tusk add`, `./bin/tusk list`) against the default DB produces the same output as before the phase.

---

## File Structure

**Create:**
- `sqlite/registry.go` — `StoreRegistry` type with `NewStoreRegistry`, `Get`, `Default`, `ProjectIDs`, `Close`, internal `openPath`, and `resolveDBPath` helper
- `sqlite/registry_test.go` — unit tests
- `service/repos.go` — `RepoBundle` struct (fields only), `BundleResolver` type, `ProjectLister` type

**Modify:**
- `domain/errors.go` — add `ErrCrossStoreRelation` sentinel
- `cmd/tusk/main.go` — replace direct `sqlite.New` with `sqlite.NewStoreRegistry`; pass the registry's default store to existing service constructors via a bridge adapter

---

## Tasks

### Task 1: `ErrCrossStoreRelation` sentinel

**Files:**
- Modify: `domain/errors.go`

Introduced now (not Phase 4) so that Phase 4 tests can reference it without adding a new error in the same commit as the refactor.

- [ ] **Step 1: Add the sentinel**

Open `domain/errors.go` and add to the existing `var (...)` block:

```go
// ErrCrossStoreRelation is returned when a relation is requested between
// tasks whose projects live in different SQLite stores. Per-project
// databases cannot hold referential links across files.
ErrCrossStoreRelation = errors.New("cross-store relation not allowed")
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add domain/errors.go
git commit -m "feat(domain): add ErrCrossStoreRelation sentinel"
```

---

### Task 2: `sqlite.StoreRegistry`

**Files:**
- Create: `sqlite/registry.go`
- Create: `sqlite/registry_test.go`

The registry lazily opens SQLite files keyed by absolute path. Projects with no `db_path` share the default store. `NewStoreRegistry` eagerly opens the default so a broken config fails at startup.

Paths are resolved relative to a base directory passed in by the caller. `cmd/tusk/main.go` passes the directory containing the effective config file, matching the spec in ROADMAP.md:558. Absolute paths bypass the base dir; `~`-prefixed paths expand against the user's home dir.

- [ ] **Step 1: Write the failing tests**

Create `sqlite/registry_test.go`:

```go
package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/migrations"
)

func TestStoreRegistry_DefaultFallback(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "default.db")
	projects := map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
	}

	reg, err := NewStoreRegistry(defaultPath, dir, projects, migrations.FS)
	if err != nil {
		t.Fatalf("NewStoreRegistry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })

	store, err := reg.Get("default")
	if err != nil {
		t.Fatalf("Get default: %v", err)
	}
	if store == nil {
		t.Fatal("nil store")
	}
	store2, _ := reg.Get("default")
	if store != store2 {
		t.Fatal("expected cached store")
	}
}

func TestStoreRegistry_PerProjectAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "default.db")
	backendPath := filepath.Join(dir, "backend.db")
	projects := map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {Workflow: "kanban", DBPath: backendPath},
	}
	reg, err := NewStoreRegistry(defaultPath, dir, projects, migrations.FS)
	if err != nil {
		t.Fatalf("NewStoreRegistry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })

	def, _ := reg.Get("default")
	back, _ := reg.Get("backend")
	if def == back {
		t.Fatal("default and backend should be distinct stores")
	}
	if _, err := os.Stat(backendPath); err != nil {
		t.Fatalf("backend db file not created: %v", err)
	}
}

func TestStoreRegistry_RelativePathResolvedAgainstBase(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "default.db")
	projects := map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {Workflow: "kanban", DBPath: "backend.db"}, // relative
	}
	reg, err := NewStoreRegistry(defaultPath, dir, projects, migrations.FS)
	if err != nil {
		t.Fatalf("NewStoreRegistry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })
	if _, err := reg.Get("backend"); err != nil {
		t.Fatalf("Get backend: %v", err)
	}
	expected := filepath.Join(dir, "backend.db")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected db at %s: %v", expected, err)
	}
}

func TestStoreRegistry_UnknownProject(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewStoreRegistry(filepath.Join(dir, "d.db"), dir,
		map[string]config.ProjectConfig{"default": {Workflow: "kanban"}}, migrations.FS)
	t.Cleanup(func() { reg.Close() })
	if _, err := reg.Get("ghost"); err == nil {
		t.Fatal("expected error for unknown project")
	}
}

func TestStoreRegistry_ProjectIDs(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewStoreRegistry(filepath.Join(dir, "d.db"), dir,
		map[string]config.ProjectConfig{"default": {Workflow: "kanban"}, "backend": {Workflow: "kanban"}}, migrations.FS)
	t.Cleanup(func() { reg.Close() })
	ids := reg.ProjectIDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 project ids, got %v", ids)
	}
}

func TestStoreRegistry_Close(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewStoreRegistry(filepath.Join(dir, "d.db"), dir,
		map[string]config.ProjectConfig{"default": {Workflow: "kanban"}}, migrations.FS)
	if _, err := reg.Get("default"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := reg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
go test ./sqlite -run TestStoreRegistry -v
```

Expected: FAIL — `NewStoreRegistry` undefined.

- [ ] **Step 3: Implement**

Create `sqlite/registry.go`:

```go
package sqlite

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/germanamz/tusk/config"
)

// StoreRegistry resolves project IDs to lazily-opened SQLite stores.
// Projects without an explicit db_path share the default store.
type StoreRegistry struct {
	defaultPath string
	baseDir     string
	projects    map[string]config.ProjectConfig
	migrations  fs.FS

	mu     sync.Mutex
	stores map[string]*Store // keyed by absolute path
}

// NewStoreRegistry creates a registry. The default store is opened eagerly;
// per-project stores are opened on first access. baseDir is the directory
// used to resolve relative db_path values (typically the directory holding
// the effective config file).
func NewStoreRegistry(defaultPath, baseDir string, projects map[string]config.ProjectConfig, migrations fs.FS) (*StoreRegistry, error) {
	abs, err := resolveDBPath(defaultPath, baseDir)
	if err != nil {
		return nil, err
	}
	reg := &StoreRegistry{
		defaultPath: abs,
		baseDir:     baseDir,
		projects:    projects,
		migrations:  migrations,
		stores:      make(map[string]*Store),
	}
	if _, err := reg.openPath(abs); err != nil {
		return nil, err
	}
	return reg, nil
}

// Get returns the store that handles the given project ID.
func (r *StoreRegistry) Get(projectID string) (*Store, error) {
	r.mu.Lock()
	proj, ok := r.projects[projectID]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown project %q", projectID)
	}
	path := r.defaultPath
	if proj.DBPath != "" {
		abs, err := resolveDBPath(proj.DBPath, r.baseDir)
		if err != nil {
			return nil, err
		}
		path = abs
	}
	return r.openPath(path)
}

// Default returns the default store.
func (r *StoreRegistry) Default() (*Store, error) {
	return r.openPath(r.defaultPath)
}

// ProjectIDs returns the set of project IDs known to the registry, sorted.
func (r *StoreRegistry) ProjectIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.projects))
	for id := range r.projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Close closes every opened store. Safe to call multiple times.
func (r *StoreRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	for p, s := range r.stores {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing %s: %w", p, err)
		}
		delete(r.stores, p)
	}
	return firstErr
}

func (r *StoreRegistry) openPath(path string) (*Store, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.stores[path]; ok {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating db dir: %w", err)
	}
	s, err := New(path, r.migrations)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	r.stores[path] = s
	return s, nil
}

// resolveDBPath expands ~ and returns an absolute path. Relative paths
// are resolved against baseDir. Absolute paths are returned as-is.
func resolveDBPath(path, baseDir string) (string, error) {
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

- [ ] **Step 4: Re-run tests**

```bash
go test ./sqlite -run TestStoreRegistry -v
```

Expected: PASS (all six cases).

- [ ] **Step 5: Commit**

```bash
git add sqlite/registry.go sqlite/registry_test.go
git commit -m "feat(sqlite): add StoreRegistry for per-project databases"
```

---

### Task 3: `service.RepoBundle` and resolver types

**Files:**
- Create: `service/repos.go`

Declare types only. Phase 4 gives them meaningful use. Phase 3 ships them so that main.go can reference the types in its bridge wiring without pulling them from Phase 4.

- [ ] **Step 1: Create `service/repos.go`**

```go
package service

import (
	"context"

	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/sqlite"
)

// RepoBundle groups the repositories and the underlying store (used as a
// transaction provider) for a single SQLite database. Every project
// resolves to exactly one bundle.
type RepoBundle struct {
	Store       *sqlite.Store
	Tasks       repository.TaskRepository
	Annotations repository.AnnotationRepository
	Relations   repository.RelationRepository
	Tags        repository.TagRepository
	Players     repository.PlayerRepository
}

// BundleResolver returns the RepoBundle that owns the given project.
// Implementations are wired in cmd/tusk/main.go using sqlite.StoreRegistry.
type BundleResolver func(ctx context.Context, projectID string) (*RepoBundle, error)

// ProjectLister returns every project ID currently known to the resolver.
// Used by fan-out reads in Phase 4.
type ProjectLister func(ctx context.Context) ([]string, error)
```

- [ ] **Step 2: Build**

```bash
go build ./service/...
```

Expected: PASS.

**Caveat:** verify that `repository.PlayerRepository`, `repository.AnnotationRepository`, `repository.RelationRepository`, `repository.TagRepository`, `repository.TaskRepository` all exist in `repository/`. If any interface is named differently, adjust the import here. Drop any field whose repository does not yet exist (e.g. if there is no `AnnotationRepository` interface yet, list the type behind however it is actually expressed today — grep `repository/` to confirm).

- [ ] **Step 3: Commit**

```bash
git add service/repos.go
git commit -m "feat(service): add RepoBundle and resolver type aliases"
```

---

### Task 4: Wire `StoreRegistry` into `cmd/tusk/main.go` as a bridge

**Files:**
- Modify: `cmd/tusk/main.go`

**Bridge code introduced in this step (tagged for removal in Phase 4 Task 5).** The registry is constructed and used for the default store, but services still receive direct repositories built from the default store. This keeps Phase 3 a pure no-op at runtime.

- [ ] **Step 1: Read the current wiring**

Open `cmd/tusk/main.go` and inspect lines 31-92 (the `run()` function). Note the current flow:

1. `cfg, err := config.Load()`
2. `dbPath, err := resolveDBPath(cfg.Storage.Path)` — local helper in main.go
3. `os.MkdirAll(filepath.Dir(dbPath), 0o755)`
4. `store, err := sqlite.New(dbPath, migrations.FS)`
5. Build `taskRepo`, `annotationRepo`, `tagRepo`, `relationRepo`, `playerRepo` from `store.DB()`
6. Build `projectRepo` from `cfg.Projects`, `workflowRepo` from `cfg.Workflows`
7. Build `workflowSvc`, `urgencyEngine`
8. Build `taskSvc`, `tagSvc`, `relationSvc`, `projectSvc`, `playerSvc`
9. Build `tui.New(...)`

- [ ] **Step 2: Replace steps 3-5 with registry-backed wiring**

Find the config file directory to use as base for relative `db_path` resolution. `config.ConfigFilePath()` returns the effective config file path; use `filepath.Dir` on it. Fall back to `"."` if the lookup fails.

Replace the existing `store, err := sqlite.New(dbPath, migrations.FS)` block with:

```go
configPath, _ := config.ConfigFilePath()
baseDir := "."
if configPath != "" {
	baseDir = filepath.Dir(configPath)
}

registry, err := sqlite.NewStoreRegistry(dbPath, baseDir, cfg.Projects, migrations.FS)
if err != nil {
	return fmt.Errorf("opening database: %w", err)
}
defer registry.Close()

// BRIDGE (remove in Phase 4 Task 5): services still consume direct repos
// built from the default store, preserving single-store behavior until
// the service layer is refactored to use BundleResolver.
defaultStore, err := registry.Default()
if err != nil {
	return fmt.Errorf("default store: %w", err)
}
store := defaultStore
```

Leave the rest of the function (lines 54 onward) **unchanged**. `store.DB()`, `sqlite.NewTaskRepo(db)`, `service.NewTaskService(...)`, and every other call continues to use the default store exactly as before.

- [ ] **Step 3: Build and run the full test suite**

```bash
go build ./cmd/tusk
make test test-race test-e2e vet lint
```

Expected: every target passes with zero behavior change. If any e2e scenario fails, the bridge is not preserving single-store semantics — most likely cause is the registry opening the default store at a different absolute path because of path normalization. Add a `t.Logf` to confirm paths match.

- [ ] **Step 4: Manual smoke test**

```bash
./bin/tusk add "smoke test task"
./bin/tusk list
```

Expected: task appears, no panic, default DB at `~/.local/share/tusk/tusk.db` (or wherever the user has it configured).

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/main.go
git commit -m "refactor(cmd): route default store through StoreRegistry bridge"
```

---

## Changes Introduced

**New files:**
- `sqlite/registry.go`
- `sqlite/registry_test.go`
- `service/repos.go`

**Modified files:**
- `domain/errors.go` — added `ErrCrossStoreRelation` sentinel
- `cmd/tusk/main.go` — replaced direct `sqlite.New` with `sqlite.NewStoreRegistry` + `registry.Default()`; rest of wiring unchanged (see bridge note)

**New exported API:**
- `sqlite.StoreRegistry`
- `sqlite.NewStoreRegistry(defaultPath, baseDir string, projects map[string]config.ProjectConfig, migrations fs.FS) (*StoreRegistry, error)`
- `(*StoreRegistry).Get(projectID string) (*Store, error)`
- `(*StoreRegistry).Default() (*Store, error)`
- `(*StoreRegistry).ProjectIDs() []string`
- `(*StoreRegistry).Close() error`
- `service.RepoBundle` struct
- `service.BundleResolver` func type
- `service.ProjectLister` func type
- `domain.ErrCrossStoreRelation` sentinel

**Bridge code (tagged for removal in Phase 4 Task 5):**
- `cmd/tusk/main.go` — the `defaultStore := registry.Default()` fallback and its `store := defaultStore` assignment, plus the unchanged direct-repo wiring that consumes it. Phase 4 replaces this block with a resolver closure and updated service constructors.

**Migrations / env vars / dependencies:** None added. Embedded `migrations.FS` is re-used through the registry.

**Behavioral guarantees for downstream phases:**
- `StoreRegistry.Get(projectID)` returns an error for unknown project IDs — Phase 4's `BundleResolver` closure must preserve this so that `TaskService.Create` with an invalid project still fails with the same error shape users see today.
- `StoreRegistry.Default()` returns the eagerly-opened default store; multiple calls return the same pointer. Phase 4's resolver closure must treat pointer identity as authoritative (the `RelationService` cross-store guard compares bundles by pointer).
- `resolveDBPath` now treats relative `db_path` values as relative to the config-file directory, not CWD. This is a new behavior but has no effect on existing configs that never used relative `db_path` (the field was introduced in Phase 1 and has no prior semantics).
- `ProjectIDs()` returns a sorted slice — deterministic ordering matters for tests that assert fan-out query order.
